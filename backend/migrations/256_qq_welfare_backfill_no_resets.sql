-- Follow-up: enabling auto reset is not evidence of consuming a reset.
-- Preserve migration 255; include only pure rewards with zero reset events.
-- Convert only provably unmixed invitation entitlements. Purchased/default/admin
-- subscriptions remain standard, including ambiguous historical mixed records.
-- The assigned pool/reward foreign keys establish provenance; notes are only an
-- additional conservative guard against unrecorded administrator changes.
CREATE TEMP TABLE qq_welfare_backfill_no_resets ON COMMIT DROP AS
WITH rewarded AS (
 SELECT c.id,c.used_by,c.group_id,c.used_at,c.validity_days
 FROM redeem_codes c
 JOIN qq_invite_code_pool p ON p.redeem_code_id=c.id
 JOIN qq_invite_rewards r ON r.pool_id=p.id AND r.record_id=p.record_id
 WHERE c.status='used' AND c.type='subscription' AND c.validity_days=30
   AND c.used_by IS NOT NULL AND c.used_at IS NOT NULL
), candidates AS (
 SELECT s.id,s.user_id,s.group_id,s.starts_at,s.expires_at,s.notes,
        count(*)::bigint AS grant_count,min(r.used_at) AS first_grant,
        array_agg(r.id) AS code_ids,
        NULLIF(g.daily_limit_usd,0) AS daily_limit_usd,NULLIF(g.weekly_limit_usd,0) AS weekly_limit_usd,g.monthly_limit_usd
 FROM user_subscriptions s
 JOIN groups g ON g.id=s.group_id AND g.deleted_at IS NULL
 JOIN rewarded r ON r.used_by=s.user_id AND r.group_id=s.group_id
   AND r.used_at>=s.starts_at-interval '1 second'
 WHERE s.entitlement_kind='standard' AND s.deleted_at IS NULL
   AND s.status IN ('active','expired') AND s.assigned_by IS NULL
   AND NOT s.preserve_calendar_daily_reset AND NOT EXISTS (SELECT 1 FROM subscription_daily_reset_events e WHERE e.subscription_id=s.id)
   AND g.monthly_limit_usd>0 AND g.monthly_limit_usd<10000000000
   AND (g.daily_limit_usd IS NULL OR g.daily_limit_usd>=0)
   AND (g.weekly_limit_usd IS NULL OR g.weekly_limit_usd>=0)
 GROUP BY s.id,g.id
)
SELECT c.* FROM candidates c
WHERE abs(extract(epoch FROM (c.starts_at-c.first_grant)))<1
  AND c.monthly_limit_usd*c.grant_count<10000000000
  AND (c.daily_limit_usd IS NULL OR c.daily_limit_usd*c.grant_count<10000000000)
  AND (c.weekly_limit_usd IS NULL OR c.weekly_limit_usd*c.grant_count<10000000000)
  AND abs(extract(epoch FROM (c.expires_at-(c.starts_at+c.grant_count*interval '30 days'))))<1
  AND NOT EXISTS (
    SELECT 1 FROM regexp_split_to_table(COALESCE(c.notes,''),E'\n') AS n(line)
    WHERE n.line NOT IN (SELECT '通过兑换码 #'||r.id||' 兑换' FROM rewarded r
                        WHERE r.used_by=c.user_id AND r.group_id=c.group_id)
  )
  AND NOT EXISTS (
    SELECT 1 FROM redeem_codes r WHERE r.used_by=c.user_id AND r.group_id=c.group_id
      AND r.type='subscription' AND r.status='used'
      AND NOT EXISTS (SELECT 1 FROM rewarded w WHERE w.id=r.id)
  )
  AND NOT EXISTS (
    SELECT 1 FROM payment_orders o WHERE o.user_id=c.user_id AND o.subscription_group_id=c.group_id
      AND o.status NOT IN ('PENDING','CANCELLED','EXPIRED')
  )
  AND NOT EXISTS (
    SELECT 1 FROM user_subscriptions w WHERE w.user_id=c.user_id AND w.group_id=c.group_id
      AND w.entitlement_kind='welfare' AND NOT w.welfare_cycle_closed AND w.deleted_at IS NULL
  );

UPDATE user_subscriptions s SET
 entitlement_kind='welfare',
 welfare_grant_count=b.grant_count,
 welfare_daily_limit_usd=b.daily_limit_usd*b.grant_count,
 welfare_weekly_limit_usd=b.weekly_limit_usd*b.grant_count,
 welfare_monthly_limit_usd=b.monthly_limit_usd*b.grant_count,
 expires_at=b.starts_at+interval '30 days',
 status=CASE WHEN b.starts_at+interval '30 days'>now() THEN 'active' ELSE 'expired' END,
 auto_daily_reset_enabled=false,updated_at=now()
FROM qq_welfare_backfill_no_resets b WHERE s.id=b.id;

INSERT INTO user_welfare_subscription_grants
 (redeem_code_id,subscription_id,daily_limit_usd,weekly_limit_usd,monthly_limit_usd,granted_at)
SELECT r.id,b.id,b.daily_limit_usd,b.weekly_limit_usd,b.monthly_limit_usd,r.used_at
FROM qq_welfare_backfill_no_resets b JOIN redeem_codes r ON r.id=ANY(b.code_ids);

-- Usage counters, reset windows, subscription IDs, usage-log foreign keys and
-- all standard/purchased entitlements are intentionally preserved.
