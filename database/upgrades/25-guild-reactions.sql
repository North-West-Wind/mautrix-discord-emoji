-- v25 (compatible with v19+): Discord emoji store
CREATE TABLE guild_emoji (
    dc_guild_id   TEXT NOT NULL,
    dc_emoji_name TEXT NOT NULL,
		mxc           TEXT NOT NULL,

    PRIMARY KEY (dc_guild_id, dc_emoji_name)
);