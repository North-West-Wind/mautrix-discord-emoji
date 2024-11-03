// mautrix-discord - A Matrix-Discord puppeting bridge.
// Copyright (C) 2022 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	log "maunium.net/go/maulogger/v2"
	"maunium.net/go/maulogger/v2/maulogadapt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/bwmarrin/discordgo"

	"go.mau.fi/mautrix-discord/config"
	"go.mau.fi/mautrix-discord/database"
)

type Guild struct {
	*database.Guild

	bridge *DiscordBridge
	log    log.Logger

	roomCreateLock      sync.Mutex
	emojis              map[string]*database.GuildEmoji
	discordEmojis       []*discordgo.Emoji
	allowExternalEmojis bool
}

func (br *DiscordBridge) loadGuild(dbGuild *database.Guild, id string, createIfNotExist bool) *Guild {
	if dbGuild == nil {
		if id == "" || !createIfNotExist {
			return nil
		}

		dbGuild = br.DB.Guild.New()
		dbGuild.ID = id
		dbGuild.Insert()
	}

	guild := br.NewGuild(dbGuild)

	br.guildsByID[guild.ID] = guild
	if guild.MXID != "" {
		br.guildsByMXID[guild.MXID] = guild
	}

	return guild
}

func (br *DiscordBridge) GetGuildByMXID(mxid id.RoomID) *Guild {
	br.guildsLock.Lock()
	defer br.guildsLock.Unlock()

	portal, ok := br.guildsByMXID[mxid]
	if !ok {
		return br.loadGuild(br.DB.Guild.GetByMXID(mxid), "", false)
	}

	return portal
}

func (br *DiscordBridge) GetGuildByID(id string, createIfNotExist bool) *Guild {
	br.guildsLock.Lock()
	defer br.guildsLock.Unlock()

	guild, ok := br.guildsByID[id]
	if !ok {
		return br.loadGuild(br.DB.Guild.GetByID(id), id, createIfNotExist)
	}

	return guild
}

func (br *DiscordBridge) GetAllGuilds() []*Guild {
	return br.dbGuildsToGuilds(br.DB.Guild.GetAll())
}

func (br *DiscordBridge) dbGuildsToGuilds(dbGuilds []*database.Guild) []*Guild {
	br.guildsLock.Lock()
	defer br.guildsLock.Unlock()

	output := make([]*Guild, len(dbGuilds))
	for index, dbGuild := range dbGuilds {
		if dbGuild == nil {
			continue
		}

		guild, ok := br.guildsByID[dbGuild.ID]
		if !ok {
			guild = br.loadGuild(dbGuild, "", false)
		}

		output[index] = guild
	}

	return output
}

func (br *DiscordBridge) NewGuild(dbGuild *database.Guild) *Guild {
	emojis := map[string]*database.GuildEmoji{}
	for _, emoji := range br.DB.GuildEmoji.GetAllByGuildID(dbGuild.ID) {
		emojis[emoji.EmojiName] = emoji
	}
	guild := &Guild{
		Guild:  dbGuild,
		bridge: br,
		log:    br.Log.Sub(fmt.Sprintf("Guild/%s", dbGuild.ID)),
		emojis: emojis,
	}

	return guild
}

func (guild *Guild) getBridgeInfo() (string, event.BridgeEventContent) {
	bridgeInfo := event.BridgeEventContent{
		BridgeBot: guild.bridge.Bot.UserID,
		Creator:   guild.bridge.Bot.UserID,
		Protocol: event.BridgeInfoSection{
			ID:          "discordgo",
			DisplayName: "Discord",
			AvatarURL:   guild.bridge.Config.AppService.Bot.ParsedAvatar.CUString(),
			ExternalURL: "https://discord.com/",
		},
		Channel: event.BridgeInfoSection{
			ID:          guild.ID,
			DisplayName: guild.Name,
			AvatarURL:   guild.AvatarURL.CUString(),
		},
	}
	bridgeInfoStateKey := fmt.Sprintf("fi.mau.discord://discord/%s", guild.ID)
	return bridgeInfoStateKey, bridgeInfo
}

func (guild *Guild) UpdateBridgeInfo() {
	if len(guild.MXID) == 0 {
		guild.log.Debugln("Not updating bridge info: no Matrix room created")
		return
	}
	guild.log.Debugln("Updating bridge info...")
	stateKey, content := guild.getBridgeInfo()
	_, err := guild.bridge.Bot.SendStateEvent(guild.MXID, event.StateBridge, stateKey, content)
	if err != nil {
		guild.log.Warnln("Failed to update m.bridge:", err)
	}
	// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
	_, err = guild.bridge.Bot.SendStateEvent(guild.MXID, event.StateHalfShotBridge, stateKey, content)
	if err != nil {
		guild.log.Warnln("Failed to update uk.half-shot.bridge:", err)
	}
}

func (guild *Guild) CreateMatrixRoom(user *User, meta *discordgo.Guild) error {
	guild.roomCreateLock.Lock()
	defer guild.roomCreateLock.Unlock()
	if guild.MXID != "" {
		return nil
	}
	guild.log.Infoln("Creating Matrix room for guild")
	guild.UpdateInfo(user, meta)

	bridgeInfoStateKey, bridgeInfo := guild.getBridgeInfo()

	initialState := []*event.Event{{
		Type:     event.StateBridge,
		Content:  event.Content{Parsed: bridgeInfo},
		StateKey: &bridgeInfoStateKey,
	}, {
		// TODO remove this once https://github.com/matrix-org/matrix-doc/pull/2346 is in spec
		Type:     event.StateHalfShotBridge,
		Content:  event.Content{Parsed: bridgeInfo},
		StateKey: &bridgeInfoStateKey,
	}}

	if !guild.AvatarURL.IsEmpty() {
		initialState = append(initialState, &event.Event{
			Type: event.StateRoomAvatar,
			Content: event.Content{Parsed: &event.RoomAvatarEventContent{
				URL: guild.AvatarURL,
			}},
		})
	}

	creationContent := map[string]interface{}{
		"type": event.RoomTypeSpace,
	}
	if !guild.bridge.Config.Bridge.FederateRooms {
		creationContent["m.federate"] = false
	}

	resp, err := guild.bridge.Bot.CreateRoom(&mautrix.ReqCreateRoom{
		Visibility:      "private",
		Name:            guild.Name,
		Preset:          "private_chat",
		InitialState:    initialState,
		CreationContent: creationContent,
	})
	if err != nil {
		guild.log.Warnln("Failed to create room:", err)
		return err
	}

	guild.MXID = resp.RoomID
	guild.NameSet = true
	guild.AvatarSet = !guild.AvatarURL.IsEmpty()
	guild.Update()
	guild.bridge.guildsLock.Lock()
	guild.bridge.guildsByMXID[guild.MXID] = guild
	guild.bridge.guildsLock.Unlock()
	guild.log.Infoln("Matrix room created:", guild.MXID)

	user.ensureInvited(nil, guild.MXID, false, true)

	return nil
}

func (guild *Guild) UpdateInfo(source *User, meta *discordgo.Guild) *discordgo.Guild {
	if meta.Unavailable {
		guild.log.Debugfln("Ignoring unavailable guild update")
		return meta
	}
	changed := false
	changed = guild.UpdateName(meta) || changed
	changed = guild.UpdateAvatar(meta.Icon) || changed
	if changed {
		guild.UpdateBridgeInfo()
		guild.Update()
	}
	// handle emoji fetching
	guild.UpdateEmojis(meta)
	guild.allowExternalEmojis = meta.Permissions&discordgo.PermissionUseExternalEmojis != 0
	source.ensureInvited(nil, guild.MXID, false, false)
	return meta
}

func (guild *Guild) UpdateName(meta *discordgo.Guild) bool {
	name := guild.bridge.Config.Bridge.FormatGuildName(config.GuildNameParams{
		Name: meta.Name,
	})
	if guild.PlainName == meta.Name && guild.Name == name && (guild.NameSet || guild.MXID == "") {
		return false
	}
	guild.log.Debugfln("Updating name %q -> %q", guild.Name, name)
	guild.Name = name
	guild.PlainName = meta.Name
	guild.NameSet = false
	if guild.MXID != "" {
		_, err := guild.bridge.Bot.SetRoomName(guild.MXID, guild.Name)
		if err != nil {
			guild.log.Warnln("Failed to update room name: %s", err)
		} else {
			guild.NameSet = true
		}
	}
	return true
}

func (guild *Guild) UpdateAvatar(iconID string) bool {
	if guild.Avatar == iconID && (iconID == "") == guild.AvatarURL.IsEmpty() && (guild.AvatarSet || guild.MXID == "") {
		return false
	}
	guild.log.Debugfln("Updating avatar %q -> %q", guild.Avatar, iconID)
	guild.AvatarSet = false
	guild.Avatar = iconID
	guild.AvatarURL = id.ContentURI{}
	if guild.Avatar != "" {
		// TODO direct media support
		copied, err := guild.bridge.copyAttachmentToMatrix(guild.bridge.Bot, discordgo.EndpointGuildIcon(guild.ID, iconID), false, AttachmentMeta{
			AttachmentID: fmt.Sprintf("guild_avatar/%s/%s", guild.ID, iconID),
		})
		if err != nil {
			guild.log.Warnfln("Failed to reupload guild avatar %s: %v", iconID, err)
			return true
		}
		guild.AvatarURL = copied.MXC
	}
	if guild.MXID != "" {
		_, err := guild.bridge.Bot.SetRoomAvatar(guild.MXID, guild.AvatarURL)
		if err != nil {
			guild.log.Warnln("Failed to update room avatar:", err)
		} else {
			guild.AvatarSet = true
		}
	}
	return true
}

var ImagePack = event.Type{Type: "im.ponies.room_emotes", Class: event.StateEventType}

type ImagePackPack struct {
	DisplayName string   `json:"display_name,omitempty"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	Usage       []string `json:"usage,omitempty"`
	Attribution string   `json:"attribution,omitempty"`
}

type ImagePackImage struct {
	URL   string   `json:"url"`
	Usage []string `json:"usage,omitempty"`
}

type ImagePackEventContent struct {
	Pack   ImagePackPack             `json:"pack"`
	Images map[string]ImagePackImage `json:"images"`
}

func (guild *Guild) UpdateEmojis(meta *discordgo.Guild) {
	guild.discordEmojis = meta.Emojis
	emojis := map[string]*database.GuildEmoji{}
	for _, emoji := range meta.Emojis {
		if emoji.Animated {
			// skip animated for now
			continue
		}
		converted := guild.bridge.DB.GuildEmoji.New()
		converted.FromDiscord(meta.ID, emoji)
		emojis[converted.EmojiName] = converted
	}
	changed := len(emojis) != len(guild.emojis)
	for name, emoji := range emojis {
		existing := guild.emojis[name]
		if existing != nil {
			emoji.MXC = existing.MXC
		} else {
			changed = true
			split := strings.Split(name, ":")
			copied, err := guild.bridge.copyAttachmentToMatrix(guild.bridge.Bot, discordgo.EndpointEmoji(split[len(split)-1]), false, AttachmentMeta{
				AttachmentID: fmt.Sprintf("guild_emoji/%s/%s", guild.ID, name),
			})
			if err != nil {
				guild.log.Warnfln("Failed to upload guild emoji %s: %v", name, err)
				// we still allow processing of other emojis
			} else {
				emoji.MXC = strings.TrimPrefix(copied.MXC.String(), "mxc://")
			}
		}
	}
	// self-handling changes because it doesn't have anything to do with the guild
	if changed {
		for name, emoji := range guild.emojis {
			// remove those that doesn't exist anymore
			if emojis[name] == nil {
				emoji.Delete()
				guild.emojis[name] = nil
			}
		}
		for name, emoji := range emojis {
			// add all the new ones that didn't exist in the database
			if guild.emojis[name] == nil {
				emoji.Insert()
				guild.emojis[name] = emoji
			}
		}
		// update image pack
		if guild.MXID != "" {
			content := ImagePackEventContent{
				Pack: ImagePackPack{
					DisplayName: guild.PlainName,
					AvatarURL:   guild.AvatarURL.String(),
					Usage:       []string{"emoticon", "sticker"},
				},
				Images: map[string]ImagePackImage{},
			}
			for name, emoji := range emojis {
				if emoji.MXC == "" {
					continue
				}
				split := strings.Split(name, ":")
				emojiName := strings.Join(split[:len(split)-1], ":")
				content.Images[emojiName] = ImagePackImage{
					URL: "mxc://" + emoji.MXC,
				}
			}
			_, err := guild.bridge.Bot.SendStateEvent(guild.MXID, ImagePack, guild.ID, content)
			if err != nil {
				guild.log.Warnln("Failed to update im.ponies.room_emotes:", err)
			}
		}
	}
}

func (guild *Guild) cleanup() {
	if guild.MXID == "" {
		return
	}
	intent := guild.bridge.Bot
	if guild.bridge.SpecVersions.Supports(mautrix.BeeperFeatureRoomYeeting) {
		err := intent.BeeperDeleteRoom(guild.MXID)
		if err != nil && !errors.Is(err, mautrix.MNotFound) {
			guild.log.Errorfln("Failed to delete %s using hungryserv yeet endpoint: %v", guild.MXID, err)
		}
		return
	}
	guild.bridge.cleanupRoom(intent, guild.MXID, false, *maulogadapt.MauAsZero(guild.log))
}

func (guild *Guild) RemoveMXID() {
	guild.bridge.guildsLock.Lock()
	defer guild.bridge.guildsLock.Unlock()
	if guild.MXID == "" {
		return
	}
	delete(guild.bridge.guildsByMXID, guild.MXID)
	guild.MXID = ""
	guild.AvatarSet = false
	guild.NameSet = false
	guild.BridgingMode = database.GuildBridgeNothing
	guild.Update()
}

func (guild *Guild) Delete() {
	guild.Guild.Delete()
	guild.bridge.guildsLock.Lock()
	delete(guild.bridge.guildsByID, guild.ID)
	if guild.MXID != "" {
		delete(guild.bridge.guildsByMXID, guild.MXID)
	}
	guild.bridge.guildsLock.Unlock()

}
