-- A paid renewal grants monthly allowance to the current standard term. The
-- legacy horizon preserves any intervening months already available before
-- the renewal and keeps the prepaid extension from granting a second quota.
ALTER TABLE user_subscriptions
 ADD COLUMN paid_monthly_limit_usd NUMERIC(30,10),
 ADD COLUMN paid_monthly_legacy_expires_at TIMESTAMPTZ,
 ADD COLUMN paid_monthly_legacy_limit_usd NUMERIC(30,10),
 ADD CONSTRAINT user_subscriptions_paid_monthly_quota_check CHECK (
  (paid_monthly_limit_usd IS NULL AND paid_monthly_legacy_expires_at IS NULL AND paid_monthly_legacy_limit_usd IS NULL)
  OR (
   entitlement_kind = 'standard'
   AND paid_monthly_limit_usd IS NOT NULL AND paid_monthly_limit_usd >= 0 AND paid_monthly_limit_usd < 100000000000000000000
   AND paid_monthly_legacy_expires_at IS NOT NULL
   AND paid_monthly_legacy_limit_usd IS NOT NULL AND paid_monthly_legacy_limit_usd > 0 AND paid_monthly_legacy_limit_usd < 100000000000000000000
  )
 );

CREATE TABLE paid_subscription_monthly_renewals (
 payment_order_id BIGINT PRIMARY KEY REFERENCES payment_orders(id) ON DELETE RESTRICT,
 subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
 user_id BIGINT NOT NULL,
 group_id BIGINT NOT NULL,
 term_starts_at TIMESTAMPTZ NOT NULL,
 amount_usd NUMERIC(30,10) NOT NULL CHECK (amount_usd > 0 AND amount_usd < 100000000000000000000),
 validity_days INTEGER NOT NULL CHECK (validity_days = 30),
 before_expires_at TIMESTAMPTZ NOT NULL,
 after_expires_at TIMESTAMPTZ NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 reversed_at TIMESTAMPTZ,
 CHECK (term_starts_at < before_expires_at AND before_expires_at < after_expires_at),
 CHECK (reversed_at IS NULL OR reversed_at >= recorded_at)
);
CREATE INDEX paid_subscription_monthly_renewals_term_idx
 ON paid_subscription_monthly_renewals(subscription_id,term_starts_at);

ALTER TABLE user_subscription_supplement_balances
 ALTER COLUMN closed_base_remaining_usd TYPE NUMERIC(30,10);

-- Settlement for already admitted requests uses the old term's effective
-- cap after renewal reuses the subscription ID. It must not see the new term.
CREATE OR REPLACE FUNCTION close_subscription_supplement_term() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.starts_at IS DISTINCT FROM OLD.starts_at THEN
  INSERT INTO user_subscription_supplement_balances(subscription_id,term_starts_at,closed_at,closed_base_remaining_usd)
  SELECT OLD.id,OLD.starts_at,clock_timestamp(),LEAST(
   CASE WHEN limits.daily_limit>0 THEN GREATEST(0,limits.daily_limit-OLD.daily_usage_usd) END,
   CASE WHEN limits.weekly_limit>0 THEN GREATEST(0,limits.weekly_limit-OLD.weekly_usage_usd) END,
   CASE WHEN OLD.entitlement_kind='welfare' OR OLD.paid_monthly_limit_usd IS NOT NULL OR limits.monthly_limit>0
    THEN GREATEST(0,COALESCE(limits.monthly_limit,0)-OLD.monthly_usage_usd) END)
  FROM groups g CROSS JOIN LATERAL (
   SELECT CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_daily_limit_usd ELSE g.daily_limit_usd END AS daily_limit,
    CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_weekly_limit_usd ELSE g.weekly_limit_usd END AS weekly_limit,
    CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_monthly_limit_usd ELSE COALESCE(OLD.paid_monthly_limit_usd,g.monthly_limit_usd) END AS monthly_limit
  ) limits WHERE g.id=OLD.group_id
  ON CONFLICT(subscription_id,term_starts_at) DO UPDATE SET closed_at=EXCLUDED.closed_at,
   closed_base_remaining_usd=EXCLUDED.closed_base_remaining_usd,updated_at=now()
  WHERE user_subscription_supplement_balances.closed_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$;
