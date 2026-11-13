-- Leave notices outlive campaigns and prevent delayed join callbacks from granting.
CREATE TABLE qq_invite_departures (
 self_id TEXT NOT NULL, group_id TEXT NOT NULL, invitee_qq TEXT NOT NULL,
 departed_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(self_id,group_id,invitee_qq)
);
CREATE INDEX qq_invite_records_departure_match
 ON qq_invite_records(self_id,group_id,invitee_qq,event_time);

-- An immutable per-record receipt. Missing historical grant provenance is made
-- explicit as manual_review; it never authorizes a deduction from another term.
CREATE TABLE qq_invite_reward_revocations (
 id BIGSERIAL PRIMARY KEY,
 record_id BIGINT NOT NULL UNIQUE REFERENCES qq_invite_records(id) ON DELETE RESTRICT,
 redeem_code_id BIGINT UNIQUE REFERENCES redeem_codes(id) ON DELETE RESTRICT,
 subscription_id BIGINT REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
 user_id BIGINT, group_id BIGINT,
 departed_at TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('applied','manual_review')),
 reason TEXT NOT NULL,
 daily_limit_usd NUMERIC(20,10), weekly_limit_usd NUMERIC(20,10), monthly_limit_usd NUMERIC(20,10),
 before_daily_limit_usd NUMERIC(20,10), after_daily_limit_usd NUMERIC(20,10),
 before_weekly_limit_usd NUMERIC(20,10), after_weekly_limit_usd NUMERIC(20,10),
 before_monthly_limit_usd NUMERIC(20,10), after_monthly_limit_usd NUMERIC(20,10),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX qq_invite_reward_revocations_subscription_idx
 ON qq_invite_reward_revocations(subscription_id,id DESC);
CREATE INDEX qq_invite_reward_revocations_review_idx
 ON qq_invite_reward_revocations(status,id);

-- Invalidation is committed with the quota change and retried after commit.
CREATE TABLE qq_invite_subscription_invalidations (
 revocation_id BIGINT PRIMARY KEY REFERENCES qq_invite_reward_revocations(id) ON DELETE RESTRICT,
 user_id BIGINT NOT NULL, group_id BIGINT NOT NULL,
 attempts INT NOT NULL DEFAULT 0, next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_token TEXT, lease_until TIMESTAMPTZ, last_error TEXT NOT NULL DEFAULT ''
);

CREATE OR REPLACE FUNCTION qq_invite_protect_redeem_code() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE assigned BIGINT;
BEGIN
 SELECT record_id INTO assigned FROM qq_invite_code_pool WHERE redeem_code_id=OLD.id;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF NEW.code IS DISTINCT FROM OLD.code OR NEW.type IS DISTINCT FROM OLD.type
    OR NEW.value IS DISTINCT FROM OLD.value OR NEW.group_id IS DISTINCT FROM OLD.group_id
    OR NEW.validity_days IS DISTINCT FROM OLD.validity_days OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
    OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
   RAISE EXCEPTION 'qq_invite_pool_metadata_locked';
 END IF;
 IF NEW.status IS NOT DISTINCT FROM OLD.status AND NEW.used_by IS NOT DISTINCT FROM OLD.used_by
    AND NEW.used_at IS NOT DISTINCT FROM OLD.used_at THEN RETURN NEW; END IF;
 IF assigned IS NULL THEN RAISE EXCEPTION 'qq_invite_pool_unallocated'; END IF;
 IF OLD.status='unused' AND NEW.status='cancelled' AND NEW.used_by IS NULL AND NEW.used_at IS NULL
    AND EXISTS(SELECT 1 FROM qq_invite_reward_revocations WHERE record_id=assigned AND redeem_code_id=OLD.id AND status='applied')
 THEN RETURN NEW; END IF;
 IF OLD.status='unused' AND NEW.status='used' AND NEW.used_by IS NOT NULL AND NEW.used_at IS NOT NULL
    AND NOT EXISTS(SELECT 1 FROM qq_invite_reward_revocations WHERE record_id=assigned)
 THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'qq_invite_pool_redemption_transition';
END;
$$;
