-- Administrator-only, bounded group discovery. Codes and platform event identities are hashed.
-- This table deliberately has no member identity, message body, binding or wallet columns.
CREATE TABLE IF NOT EXISTS qq_image_discoveries (
 id TEXT PRIMARY KEY,
 config_id BIGINT NOT NULL REFERENCES qq_image_configs(id),
 app_id TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('waiting','found','confirmed','closed','expired')),
 code_hash TEXT NOT NULL UNIQUE,
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 group_openid TEXT,
 event_id TEXT,
 received_at TIMESTAMPTZ,
 CHECK ((group_openid IS NULL AND event_id IS NULL AND received_at IS NULL) OR
        (group_openid IS NOT NULL AND event_id IS NOT NULL AND received_at IS NOT NULL)),
 CHECK (status NOT IN ('found','confirmed') OR group_openid IS NOT NULL)
);
CREATE UNIQUE INDEX IF NOT EXISTS qq_image_discovery_one_active ON qq_image_discoveries ((true)) WHERE status IN ('waiting','found');
CREATE TABLE IF NOT EXISTS qq_image_discovery_events (
 event_hash TEXT PRIMARY KEY,
 message_hash TEXT NOT NULL UNIQUE,
 discovery_id TEXT NOT NULL REFERENCES qq_image_discoveries(id)
);
