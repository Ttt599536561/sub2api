-- Historical used welfare codes and receipts retain their original meaning.
-- Only unconsumed codes adopt independent, group-scoped subscription supplements.
ALTER TABLE redeem_codes DROP CONSTRAINT redeem_codes_welfare_supplement_check;
ALTER TABLE redeem_codes ADD CONSTRAINT redeem_codes_welfare_supplement_check CHECK (
 (subscription_effect = '' AND welfare_campaign_id IS NULL AND welfare_campaign_version IS NULL AND quota_amount_usd IS NULL)
 OR (type='subscription' AND group_id IS NOT NULL AND welfare_campaign_id IS NOT NULL AND welfare_campaign_id>0
     AND welfare_campaign_version IS NOT NULL AND welfare_campaign_version>0
     AND ((subscription_effect='add_quota' AND quota_amount_usd IS NOT NULL AND quota_amount_usd>0 AND quota_amount_usd<10000000000 AND quota_amount_usd=round(quota_amount_usd,2) AND validity_days=0)
       OR (subscription_effect='extend_duration' AND quota_amount_usd IS NULL AND validity_days BETWEEN 1 AND 36500)))
 OR (type='subscription' AND value=0 AND group_id IS NOT NULL
     AND ((welfare_campaign_id IS NULL AND welfare_campaign_version IS NULL)
       OR (welfare_campaign_id IS NOT NULL AND welfare_campaign_id>0 AND welfare_campaign_version IS NOT NULL AND welfare_campaign_version>0))
     AND ((subscription_effect='add_balance' AND quota_amount_usd IS NOT NULL AND quota_amount_usd>0 AND quota_amount_usd<10000000000 AND quota_amount_usd=round(quota_amount_usd,2) AND validity_days=0)
       OR (subscription_effect='extend_subscription' AND quota_amount_usd IS NULL AND validity_days BETWEEN 1 AND 36500)))
);

UPDATE redeem_codes SET subscription_effect=CASE subscription_effect
 WHEN 'add_quota' THEN 'add_balance' ELSE 'extend_subscription' END
 WHERE status='unused' AND subscription_effect IN ('add_quota','extend_duration');

CREATE TABLE user_subscription_supplements (
 id BIGSERIAL PRIMARY KEY,
 redeem_code_id BIGINT NOT NULL UNIQUE REFERENCES redeem_codes(id) ON DELETE RESTRICT,
 subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
 user_id BIGINT NOT NULL,
 group_id BIGINT NOT NULL,
 entitlement_kind TEXT NOT NULL CHECK(entitlement_kind IN ('standard','welfare')),
 term_starts_at TIMESTAMPTZ NOT NULL,
 subscription_effect TEXT NOT NULL CHECK(subscription_effect IN ('add_balance','extend_subscription')),
 amount_usd NUMERIC(20,10) NOT NULL,
 validity_days INTEGER NOT NULL,
 before_granted_usd NUMERIC(20,10) NOT NULL,
 after_granted_usd NUMERIC(20,10) NOT NULL,
 used_usd NUMERIC(20,10) NOT NULL,
 before_expires_at TIMESTAMPTZ NOT NULL,
 after_expires_at TIMESTAMPTZ NOT NULL,
 before_version BIGINT NOT NULL,
 after_version BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(before_granted_usd>=0 AND after_granted_usd>=0 AND used_usd>=0),
 CHECK(after_granted_usd=before_granted_usd+amount_usd),
 CHECK(after_version=before_version+1),
 CHECK((subscription_effect='add_balance' AND amount_usd>0 AND amount_usd<10000000000 AND amount_usd=round(amount_usd,2) AND validity_days=0 AND before_expires_at=after_expires_at)
   OR (subscription_effect='extend_subscription' AND amount_usd=0 AND validity_days BETWEEN 1 AND 36500 AND after_expires_at>before_expires_at))
);
CREATE INDEX user_subscription_supplements_subscription_idx ON user_subscription_supplements(subscription_id,id DESC);
CREATE INDEX user_subscription_supplements_group_idx ON user_subscription_supplements(group_id);
