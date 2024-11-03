package database

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"go.mau.fi/util/dbutil"
	log "maunium.net/go/maulogger/v2"
)

type GuildEmojiQuery struct {
	db  *Database
	log log.Logger
}

const (
	guildReactionSelect = "SELECT dc_guild_id, dc_emoji_name, mxc FROM guild_emoji"
)

func (eq *GuildEmojiQuery) New() *GuildEmoji {
	return &GuildEmoji{
		db:  eq.db,
		log: eq.log,
	}
}

func (geq *GuildEmojiQuery) GetAllByGuildID(guildID string) []*GuildEmoji {
	query := guildReactionSelect + " WHERE dc_guild_id=$1"

	return geq.getAll(query, guildID)
}

func (geq *GuildEmojiQuery) getAll(query string, args ...interface{}) []*GuildEmoji {
	rows, err := geq.db.Query(query, args...)
	if err != nil || rows == nil {
		return nil
	}

	var guildEmojis []*GuildEmoji
	for rows.Next() {
		guildEmojis = append(guildEmojis, geq.New().Scan(rows))
	}

	return guildEmojis
}

func (geq *GuildEmojiQuery) GetByMXC(mxc string) *GuildEmoji {
	query := guildReactionSelect + " WHERE mxc=$1"

	return geq.get(query, mxc)
}

func (geq *GuildEmojiQuery) GetByAlt(alt string) *GuildEmoji {
	query := guildReactionSelect + " WHERE dc_emoji_name_='$1:%'"

	return geq.get(query, alt)
}

func (geq *GuildEmojiQuery) get(query string, args ...interface{}) *GuildEmoji {
	row := geq.db.QueryRow(query, args...)
	if row == nil {
		return nil
	}

	return geq.New().Scan(row)
}

type GuildEmoji struct {
	db  *Database
	log log.Logger

	GuildID   string
	EmojiName string
	MXC       string
}

func (e *GuildEmoji) Scan(row dbutil.Scannable) *GuildEmoji {
	err := row.Scan(&e.GuildID, &e.EmojiName, &e.MXC)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			e.log.Errorln("Database scan failed:", err)
			panic(err)
		}
		return nil
	}

	return e
}

func (r *GuildEmoji) Insert() {
	query := `
		INSERT INTO guild_emoji (dc_guild_id, dc_emoji_name, mxc)
		VALUES($1, $2, $3)
	`
	_, err := r.db.Exec(query, r.GuildID, r.EmojiName, r.MXC)
	if err != nil {
		r.log.Warnfln("Failed to insert reaction for %s@%s: %v", r.GuildID, r.EmojiName, err)
		panic(err)
	}
}

func (r *GuildEmoji) Delete() {
	query := "DELETE FROM guild_emoji WHERE dc_guild_id=$1 AND dc_emoji_name=$2"
	_, err := r.db.Exec(query, r.GuildID, r.EmojiName)
	if err != nil {
		r.log.Warnfln("Failed to delete reaction for %s@%s: %v", r.GuildID, r.EmojiName, err)
		panic(err)
	}
}

func (r *GuildEmoji) FromDiscord(guildID string, emoji *discordgo.Emoji) {
	r.GuildID = guildID
	r.EmojiName = fmt.Sprintf("%s:%s", emoji.Name, emoji.ID)
}
