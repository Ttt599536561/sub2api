ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS allow_subscription_day_reset BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS auto_daily_reset_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS daily_reset_version BIGINT NOT NULL DEFAULT 0 CHECK (daily_reset_version >= 0),
    ADD COLUMN IF NOT EXISTS preserve_calendar_daily_reset BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS subscription_daily_reset_events (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    source VARCHAR(16) NOT NULL CHECK (source IN ('manual', 'automatic')),
    operation_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(128) NOT NULL,
    before_version BIGINT NOT NULL CHECK (before_version >= 0),
    after_version BIGINT NOT NULL CHECK (after_version = before_version + 1),
    timezone VARCHAR(64) NOT NULL,
    count_date DATE NOT NULL,
    day_sequence INTEGER NOT NULL CHECK (day_sequence BETWEEN 1 AND 100),
    before_daily_usage_usd NUMERIC(20,10) NOT NULL CHECK (before_daily_usage_usd > 0 AND before_daily_usage_usd < 'Infinity'::numeric),
    after_daily_usage_usd NUMERIC(20,10) NOT NULL DEFAULT 0 CHECK (after_daily_usage_usd = 0),
    daily_limit_usd NUMERIC(20,8) NOT NULL CHECK (daily_limit_usd > 0 AND daily_limit_usd < 'Infinity'::numeric),
    before_expires_at TIMESTAMPTZ NOT NULL,
    after_expires_at TIMESTAMPTZ NOT NULL,
    deducted_seconds INTEGER NOT NULL DEFAULT 86400 CHECK (deducted_seconds = 86400),
    decided_at TIMESTAMPTZ NOT NULL,
    CHECK (EXTRACT(EPOCH FROM (before_expires_at - after_expires_at)) = 86400),
    CHECK (after_expires_at > decided_at),
    UNIQUE (subscription_id, operation_id),
    UNIQUE (subscription_id, before_version),
    UNIQUE (subscription_id, count_date, day_sequence)
);

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_auto_daily_reset
    ON user_subscriptions (id)
    WHERE auto_daily_reset_enabled AND deleted_at IS NULL AND status = 'active';
