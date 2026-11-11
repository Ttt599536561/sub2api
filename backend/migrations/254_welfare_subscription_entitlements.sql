-- Keep paid/ordinary subscriptions and welfare subscriptions independent.
-- Existing rows keep their current term, usage, and standard behavior.
ALTER TABLE user_subscriptions
    ADD COLUMN entitlement_kind VARCHAR(20) NOT NULL DEFAULT 'standard',
    ADD COLUMN welfare_daily_limit_usd NUMERIC(20,10),
    ADD COLUMN welfare_weekly_limit_usd NUMERIC(20,10),
    ADD COLUMN welfare_monthly_limit_usd NUMERIC(20,10),
    ADD COLUMN welfare_grant_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN welfare_cycle_closed BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT user_subscriptions_entitlement_kind_check
        CHECK (entitlement_kind IN ('standard', 'welfare')),
    ADD CONSTRAINT user_subscriptions_standard_cycle_check
        CHECK (entitlement_kind <> 'standard' OR welfare_cycle_closed = false),
    ADD CONSTRAINT user_subscriptions_welfare_limits_check CHECK (
        entitlement_kind <> 'welfare' OR (
            welfare_grant_count > 0
            AND welfare_monthly_limit_usd IS NOT NULL
            AND welfare_monthly_limit_usd > 0
            AND welfare_monthly_limit_usd < 'Infinity'::numeric
            AND (welfare_daily_limit_usd IS NULL OR (
                welfare_daily_limit_usd >= 0 AND welfare_daily_limit_usd < 'Infinity'::numeric
            ))
            AND (welfare_weekly_limit_usd IS NULL OR (
                welfare_weekly_limit_usd >= 0 AND welfare_weekly_limit_usd < 'Infinity'::numeric
            ))
        )
    );

DROP INDEX user_subscriptions_user_group_unique_active;
CREATE UNIQUE INDEX user_subscriptions_user_group_kind_unique_active
    ON user_subscriptions(user_id, group_id, entitlement_kind)
    WHERE deleted_at IS NULL AND welfare_cycle_closed = false;

-- One durable receipt per code links every quota grant to its own term. This
-- migration creates storage only; historical reconciliation is a separate step.
CREATE TABLE user_welfare_subscription_grants (
    redeem_code_id BIGINT PRIMARY KEY REFERENCES redeem_codes(id) ON DELETE RESTRICT,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    daily_limit_usd NUMERIC(20,10),
    weekly_limit_usd NUMERIC(20,10),
    monthly_limit_usd NUMERIC(20,10),
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_welfare_subscription_grants_limits_check CHECK (
        (daily_limit_usd IS NULL OR (daily_limit_usd >= 0 AND daily_limit_usd < 'Infinity'::numeric))
        AND (weekly_limit_usd IS NULL OR (weekly_limit_usd >= 0 AND weekly_limit_usd < 'Infinity'::numeric))
        AND (monthly_limit_usd IS NULL OR (monthly_limit_usd > 0 AND monthly_limit_usd < 'Infinity'::numeric))
    )
);
CREATE INDEX user_welfare_subscription_grants_subscription_id_idx
    ON user_welfare_subscription_grants(subscription_id);
