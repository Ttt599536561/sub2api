-- One-time subscription credit follows the original term, not quota windows.
-- Standard renewal reuses a subscription ID and changes starts_at; retaining
-- historical rows prevents late settlement from spending a new term's credit.
CREATE TABLE user_subscription_supplement_balances (
 subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
 term_starts_at TIMESTAMPTZ NOT NULL,
 granted_usd NUMERIC(20,10) NOT NULL DEFAULT 0 CHECK (granted_usd >= 0),
 used_usd NUMERIC(20,10) NOT NULL DEFAULT 0 CHECK (used_usd >= 0),
 closed_at TIMESTAMPTZ,
 -- NULL with closed_at set means the frozen base capacity was unlimited.
 closed_base_remaining_usd NUMERIC(20,10) CHECK(closed_base_remaining_usd >= 0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(subscription_id,term_starts_at)
);

-- Capture the old base capacity before standard renewal replaces its windows.
-- Updating a subscription already owns its row lock; every supplement writer
-- follows the same subscription -> projection order.
CREATE FUNCTION close_subscription_supplement_term() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.starts_at IS DISTINCT FROM OLD.starts_at THEN
  INSERT INTO user_subscription_supplement_balances(subscription_id,term_starts_at,closed_at,closed_base_remaining_usd)
  SELECT OLD.id,OLD.starts_at,clock_timestamp(),LEAST(
   CASE WHEN (CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_daily_limit_usd ELSE g.daily_limit_usd END)>0
    THEN GREATEST(0,(CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_daily_limit_usd ELSE g.daily_limit_usd END)-OLD.daily_usage_usd) END,
   CASE WHEN (CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_weekly_limit_usd ELSE g.weekly_limit_usd END)>0
    THEN GREATEST(0,(CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_weekly_limit_usd ELSE g.weekly_limit_usd END)-OLD.weekly_usage_usd) END,
   CASE WHEN OLD.entitlement_kind='welfare' OR g.monthly_limit_usd>0
    THEN GREATEST(0,COALESCE(CASE WHEN OLD.entitlement_kind='welfare' THEN OLD.welfare_monthly_limit_usd ELSE g.monthly_limit_usd END,0)-OLD.monthly_usage_usd) END)
  FROM groups g WHERE g.id=OLD.group_id
  ON CONFLICT(subscription_id,term_starts_at) DO UPDATE SET closed_at=EXCLUDED.closed_at,
   closed_base_remaining_usd=EXCLUDED.closed_base_remaining_usd,updated_at=now()
  WHERE user_subscription_supplement_balances.closed_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER user_subscription_supplement_term_close BEFORE UPDATE OF starts_at ON user_subscriptions
 FOR EACH ROW EXECUTE FUNCTION close_subscription_supplement_term();

-- Immutable allocation evidence shares the usage billing dedup transaction.
-- used_usd may exceed granted_usd: already delivered usage is recorded even
-- when actual streaming cost exceeds its estimate; it never debits cash.
CREATE TABLE user_subscription_supplement_consumptions (
 id BIGSERIAL PRIMARY KEY,
 request_id TEXT NOT NULL,
 api_key_id BIGINT NOT NULL,
 subscription_id BIGINT NOT NULL,
 term_starts_at TIMESTAMPTZ NOT NULL,
 base_cost_usd NUMERIC(20,10) NOT NULL CHECK(base_cost_usd >= 0),
 supplement_cost_usd NUMERIC(20,10) NOT NULL CHECK(supplement_cost_usd >= 0),
 before_used_usd NUMERIC(20,10) NOT NULL CHECK(before_used_usd >= 0),
 after_used_usd NUMERIC(20,10) NOT NULL CHECK(after_used_usd >= before_used_usd),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(request_id,api_key_id),
 FOREIGN KEY(subscription_id,term_starts_at) REFERENCES user_subscription_supplement_balances(subscription_id,term_starts_at) ON DELETE RESTRICT,
 CHECK(after_used_usd = before_used_usd + supplement_cost_usd)
);
CREATE INDEX user_subscription_supplement_consumptions_subscription_idx
 ON user_subscription_supplement_consumptions(subscription_id,term_starts_at,id DESC);

-- Financial admission remains authoritative when Redis loses its cache keys.
-- Orphan holds expire; accepted asynchronous tasks explicitly extend their lease.
CREATE TABLE user_subscription_supplement_reservations (
 id TEXT PRIMARY KEY CHECK(length(id) BETWEEN 1 AND 128),
 subscription_id BIGINT NOT NULL,
 term_starts_at TIMESTAMPTZ NOT NULL,
 user_id BIGINT NOT NULL,
 group_id BIGINT NOT NULL,
 amount_usd NUMERIC(20,10) NOT NULL CHECK(amount_usd>0),
 expires_at TIMESTAMPTZ NOT NULL,
 released_at TIMESTAMPTZ,
 external_task_key TEXT UNIQUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(subscription_id,term_starts_at) REFERENCES user_subscription_supplement_balances(subscription_id,term_starts_at) ON DELETE RESTRICT
);
CREATE INDEX user_subscription_supplement_reservations_active_idx
 ON user_subscription_supplement_reservations(subscription_id,term_starts_at,expires_at) WHERE released_at IS NULL;
