-- A welfare total may be reduced to zero. Ordinary group limits and the
-- positive-quota requirements for newly granted invitation passes stay intact.
ALTER TABLE user_subscriptions
    DROP CONSTRAINT user_subscriptions_welfare_limits_check,
    ADD CONSTRAINT user_subscriptions_welfare_limits_check CHECK (
        entitlement_kind <> 'welfare' OR (
            welfare_grant_count > 0
            AND welfare_monthly_limit_usd IS NOT NULL
            AND welfare_monthly_limit_usd >= 0
            AND welfare_monthly_limit_usd < 'Infinity'::numeric
            AND (welfare_daily_limit_usd IS NULL OR (
                welfare_daily_limit_usd >= 0 AND welfare_daily_limit_usd < 'Infinity'::numeric
            ))
            AND (welfare_weekly_limit_usd IS NULL OR (
                welfare_weekly_limit_usd >= 0 AND welfare_weekly_limit_usd < 'Infinity'::numeric
            ))
        )
    );

-- The adjustment and this durable receipt are inserted in one transaction.
-- Actor IDs are snapshots rather than foreign keys: legacy administrative keys
-- can have actor_id=0, and later user removal must not erase the audit trail.
CREATE TABLE user_welfare_subscription_adjustments (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    actor_id BIGINT NOT NULL,
    actor_key_id TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    before_monthly_limit_usd NUMERIC(20,10) NOT NULL,
    after_monthly_limit_usd NUMERIC(20,10) NOT NULL,
    before_expires_at TIMESTAMPTZ NOT NULL,
    after_expires_at TIMESTAMPTZ NOT NULL,
    before_status TEXT NOT NULL,
    after_status TEXT NOT NULL,
    before_version BIGINT NOT NULL,
    after_version BIGINT NOT NULL,
    welfare_grant_count BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_welfare_subscription_adjustments_limits_check CHECK (
        before_monthly_limit_usd >= 0 AND before_monthly_limit_usd < 'Infinity'::numeric
        AND after_monthly_limit_usd >= 0 AND after_monthly_limit_usd < 'Infinity'::numeric
    ),
    CONSTRAINT user_welfare_subscription_adjustments_version_check CHECK (
        before_version >= 0 AND after_version > before_version
    ),
    CONSTRAINT user_welfare_subscription_adjustments_grant_count_check CHECK (welfare_grant_count > 0)
);
CREATE INDEX user_welfare_subscription_adjustments_subscription_id_idx
    ON user_welfare_subscription_adjustments(subscription_id, id DESC);
