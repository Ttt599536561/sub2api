ALTER TABLE redeem_codes
 ADD COLUMN IF NOT EXISTS subscription_effect TEXT NOT NULL DEFAULT '',
 ADD COLUMN IF NOT EXISTS welfare_campaign_id BIGINT,
 ADD COLUMN IF NOT EXISTS welfare_campaign_version BIGINT,
 ADD COLUMN IF NOT EXISTS welfare_campaign_name TEXT NOT NULL DEFAULT '',
 ADD COLUMN IF NOT EXISTS quota_amount_usd NUMERIC(20,10);

ALTER TABLE redeem_codes ADD CONSTRAINT redeem_codes_welfare_supplement_check CHECK (
 (subscription_effect = '' AND welfare_campaign_id IS NULL AND welfare_campaign_version IS NULL AND quota_amount_usd IS NULL)
 OR (type = 'subscription' AND group_id IS NOT NULL AND welfare_campaign_id > 0 AND welfare_campaign_version > 0
     AND welfare_campaign_id IS NOT NULL AND welfare_campaign_version IS NOT NULL
     AND ((subscription_effect = 'add_quota' AND quota_amount_usd IS NOT NULL AND quota_amount_usd > 0 AND quota_amount_usd < 10000000000 AND quota_amount_usd = round(quota_amount_usd,2) AND validity_days = 0)
       OR (subscription_effect = 'extend_duration' AND quota_amount_usd IS NULL AND validity_days BETWEEN 1 AND 36500)))
);
CREATE INDEX redeem_codes_welfare_campaign_idx ON redeem_codes(welfare_campaign_id) WHERE welfare_campaign_id IS NOT NULL;

-- Manual supplements are deliberately separate from QQ grant provenance. A
-- NULL optional quota in that grant ledger means unlimited during revocation.
CREATE TABLE user_welfare_subscription_supplements (
 id BIGSERIAL PRIMARY KEY,
 redeem_code_id BIGINT NOT NULL UNIQUE REFERENCES redeem_codes(id) ON DELETE RESTRICT,
 subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
 user_id BIGINT NOT NULL,
 group_id BIGINT NOT NULL,
 welfare_campaign_id BIGINT NOT NULL,
 welfare_campaign_version BIGINT NOT NULL,
 subscription_effect TEXT NOT NULL CHECK (subscription_effect IN ('add_quota','extend_duration')),
 quota_amount_usd NUMERIC(20,10),
 validity_days INTEGER NOT NULL,
 before_monthly_limit_usd NUMERIC(20,10) NOT NULL,
 after_monthly_limit_usd NUMERIC(20,10) NOT NULL,
 monthly_usage_usd NUMERIC(20,10) NOT NULL,
 before_expires_at TIMESTAMPTZ NOT NULL,
 after_expires_at TIMESTAMPTZ NOT NULL,
 before_version BIGINT NOT NULL,
 after_version BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK (before_monthly_limit_usd >= 0 AND before_monthly_limit_usd < 10000000000 AND after_monthly_limit_usd >= 0 AND after_monthly_limit_usd < 10000000000),
 CHECK (after_version = before_version + 1)
);
CREATE INDEX user_welfare_subscription_supplements_subscription_idx ON user_welfare_subscription_supplements(subscription_id,id DESC);
CREATE INDEX user_welfare_subscription_supplements_group_idx ON user_welfare_subscription_supplements(group_id);

-- Mutable per-cycle attribution, separate from the immutable receipts above.
-- All writers lock the subscription first. An administrator can permanently
-- trim this amount; summing old receipts would incorrectly resurrect that cut.
CREATE TABLE IF NOT EXISTS user_welfare_subscription_supplement_quota (
 subscription_id BIGINT PRIMARY KEY REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
 protected_quota_usd NUMERIC(20,10) NOT NULL DEFAULT 0,
 CHECK (protected_quota_usd >= 0 AND protected_quota_usd < 10000000000)
);
