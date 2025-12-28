-- v25 (compatible with v19+): Add persisted heartbeat sessions
-- originally v24, but we took that already
ALTER TABLE "user" ADD COLUMN heartbeat_session jsonb;

-- additional from emoji: Add guild to ignore emoji bridging
ALTER TABLE "guild" ADD COLUMN no_emoji boolean;
UPDATE "guild" SET no_emoji = 0;