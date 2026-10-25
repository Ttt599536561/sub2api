-- QQ is an independent opt-in authorization and accounting domain. Existing API charging is unchanged.
CREATE TABLE IF NOT EXISTS qq_image_configs (
 id BIGSERIAL PRIMARY KEY, config JSONB NOT NULL, bot_secret BYTEA NOT NULL DEFAULT ''::bytea,
 api_key BYTEA NOT NULL DEFAULT ''::bytea, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS qq_image_email_verifications (
 user_id BIGINT NOT NULL REFERENCES users(id), email TEXT NOT NULL, verified_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(user_id,email)
);
CREATE TABLE IF NOT EXISTS qq_image_recent_authorizations (
 user_id BIGINT NOT NULL REFERENCES users(id), session_hash TEXT NOT NULL, email TEXT NOT NULL,
 verified_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(user_id,session_hash)
);
CREATE TABLE IF NOT EXISTS qq_image_binding_requests (
 id TEXT PRIMARY KEY, identity_key TEXT NOT NULL, identity JSONB NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('await_website','await_qq','completed','cancelled')),
 revision BIGINT NOT NULL DEFAULT 1, candidate_user_id BIGINT REFERENCES users(id), session_hash TEXT,
 code_hash TEXT UNIQUE, code_expires_at TIMESTAMPTZ, expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX IF NOT EXISTS qq_image_one_pending_request ON qq_image_binding_requests(identity_key) WHERE status IN ('await_website','await_qq');
CREATE TABLE IF NOT EXISTS qq_image_grants (
 id TEXT PRIMARY KEY, identity_key TEXT NOT NULL, identity JSONB NOT NULL, user_id BIGINT NOT NULL REFERENCES users(id),
 revision BIGINT NOT NULL DEFAULT 1, status TEXT NOT NULL CHECK(status IN ('active','revoked')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), revoked_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS qq_image_one_active_grant ON qq_image_grants(identity_key) WHERE status='active';
CREATE TABLE IF NOT EXISTS qq_image_orders (
 id TEXT PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), grant_id TEXT NOT NULL REFERENCES qq_image_grants(id),
 grant_revision BIGINT NOT NULL, identity_key TEXT NOT NULL, event_key TEXT NOT NULL UNIQUE, request_hash TEXT NOT NULL,
 config_id BIGINT NOT NULL REFERENCES qq_image_configs(id), data JSONB NOT NULL, price NUMERIC(20,8) NOT NULL CHECK(price>0),
 business_day DATE NOT NULL, status TEXT NOT NULL CHECK(status IN ('queued','dispatching','generating','reconciling','succeeded','failed','cancelled','closed_unknown')),
 funds_status TEXT NOT NULL CHECK(funds_status IN ('held','captured','released','refunded')),
 delivery_status TEXT NOT NULL DEFAULT 'pending', website_status TEXT NOT NULL DEFAULT 'pending',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), deadline TIMESTAMPTZ NOT NULL, queue_deadline TIMESTAMPTZ NOT NULL,
 lease_token TEXT NOT NULL DEFAULT '', lease_until TIMESTAMPTZ, error TEXT NOT NULL DEFAULT '',
 artifact BYTEA, artifact_mime TEXT, artifact_sha256 TEXT, artifact_width INT, artifact_height INT, artifact_expires_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS qq_image_orders_recovery ON qq_image_orders(status,deadline);
CREATE INDEX IF NOT EXISTS qq_image_orders_user_day ON qq_image_orders(user_id,business_day);
CREATE TABLE IF NOT EXISTS qq_image_holds (
 order_id TEXT PRIMARY KEY REFERENCES qq_image_orders(id), user_id BIGINT NOT NULL REFERENCES users(id), amount NUMERIC(20,8) NOT NULL CHECK(amount>0),
 status TEXT NOT NULL CHECK(status IN ('held','captured','released','refunded'))
);
CREATE TABLE IF NOT EXISTS qq_image_ledger (
 id BIGSERIAL PRIMARY KEY, order_id TEXT NOT NULL REFERENCES qq_image_orders(id), user_id BIGINT NOT NULL REFERENCES users(id),
 operation TEXT NOT NULL CHECK(operation IN ('hold','capture','release','refund')), amount NUMERIC(20,8) NOT NULL CHECK(amount>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), UNIQUE(order_id,operation)
);
CREATE UNIQUE INDEX IF NOT EXISTS qq_image_capture_release_exclusive ON qq_image_ledger(order_id) WHERE operation IN ('capture','release');
CREATE TABLE IF NOT EXISTS qq_image_deliveries (
 id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES qq_image_orders(id), reply_message_id TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', lease_token TEXT NOT NULL DEFAULT '', lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS qq_image_audit (
 id BIGSERIAL PRIMARY KEY, actor_id BIGINT NOT NULL DEFAULT 0, action TEXT NOT NULL, object_id TEXT NOT NULL,
 reason TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS qq_image_balance_outbox (
 id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS qq_image_rate_limits (
 key TEXT PRIMARY KEY, window_start TIMESTAMPTZ NOT NULL, count INT NOT NULL CHECK(count>0)
);
CREATE TABLE IF NOT EXISTS qq_image_events (
 event_key TEXT PRIMARY KEY, request_hash TEXT NOT NULL, result TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS qq_image_media_grants (
 token_hash TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES qq_image_orders(id), grant_id TEXT NOT NULL REFERENCES qq_image_grants(id),
 grant_revision BIGINT NOT NULL, expires_at TIMESTAMPTZ NOT NULL, fetches INT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS qq_image_refund_intents (
 order_id TEXT PRIMARY KEY REFERENCES qq_image_orders(id), actor_id BIGINT NOT NULL,
 reason TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), completed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS qq_image_credential_revocations (
 config_id BIGINT NOT NULL REFERENCES qq_image_configs(id), kind TEXT NOT NULL CHECK(kind IN ('bot','api')),
 actor_id BIGINT NOT NULL, revoked_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(config_id,kind)
);
