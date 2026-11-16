-- Stable group IDs are part of invitation and welfare history. Never remove
-- those groups or turn them into ordinary balance groups. Display names and
-- future plan limits remain editable; existing welfare snapshots stay intact.
CREATE OR REPLACE FUNCTION welfare_group_has_references(target_group BIGINT)
RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM qq_invite_campaigns WHERE payload->>'subscription_group_id'=target_group::text)
 OR EXISTS(SELECT 1 FROM qq_invite_campaign_versions WHERE payload->>'subscription_group_id'=target_group::text)
 OR EXISTS(SELECT 1 FROM redeem_codes WHERE group_id=target_group AND subscription_effect<>'')
 OR EXISTS(SELECT 1 FROM user_subscriptions WHERE group_id=target_group AND entitlement_kind='welfare')
 OR EXISTS(SELECT 1 FROM user_welfare_subscription_grants g JOIN user_subscriptions s ON s.id=g.subscription_id WHERE s.group_id=target_group)
 OR EXISTS(SELECT 1 FROM user_welfare_subscription_adjustments a JOIN user_subscriptions s ON s.id=a.subscription_id WHERE s.group_id=target_group)
 OR EXISTS(SELECT 1 FROM user_welfare_subscription_supplements WHERE group_id=target_group);
$$;

CREATE OR REPLACE FUNCTION welfare_assert_group_mutable(target_group BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE reference_details TEXT;
BEGIN
 -- A Repeatable Read snapshot can predate a newly committed binding even after
 -- waiting for its SHARE lock. Application writes explicitly use Read Committed;
 -- reject destructive direct SQL in stronger isolation instead of missing history.
 IF current_setting('transaction_isolation') NOT IN ('read committed','read uncommitted') THEN
  RAISE EXCEPTION 'welfare_group_write_requires_read_committed'
   USING ERRCODE='40001', CONSTRAINT='welfare_group_write_requires_read_committed',
    DETAIL=jsonb_build_object('group_id',target_group)::text;
 END IF;
 -- This VOLATILE lookup takes a fresh RC snapshot after the group lock wait.
 -- Do not lock campaigns, codes, users or subscriptions here: reference writers
 -- hold those rows before taking the group SHARE lock.
 IF welfare_group_has_references(target_group) THEN
  SELECT jsonb_build_object(
   'group_id',target_group,
   'campaign_count',(SELECT count(DISTINCT campaign_id) FROM (
     SELECT id AS campaign_id FROM qq_invite_campaigns WHERE payload->>'subscription_group_id'=target_group::text
     UNION ALL SELECT campaign_id FROM qq_invite_campaign_versions WHERE payload->>'subscription_group_id'=target_group::text
    ) campaigns),
   'supplement_code_count',(SELECT count(*) FROM redeem_codes WHERE group_id=target_group AND subscription_effect<>''),
   'unused_supplement_count',(SELECT count(*) FROM redeem_codes WHERE group_id=target_group AND subscription_effect<>'' AND status='unused'),
   'welfare_subscription_count',(SELECT count(*) FROM user_subscriptions WHERE group_id=target_group AND entitlement_kind='welfare'),
   'welfare_history_count',(
     (SELECT count(*) FROM user_welfare_subscription_grants g JOIN user_subscriptions s ON s.id=g.subscription_id WHERE s.group_id=target_group)
     +(SELECT count(*) FROM user_welfare_subscription_adjustments a JOIN user_subscriptions s ON s.id=a.subscription_id WHERE s.group_id=target_group)
     +(SELECT count(*) FROM user_welfare_subscription_supplements WHERE group_id=target_group))
  )::text INTO reference_details;
  RAISE EXCEPTION 'welfare_group_referenced'
   USING ERRCODE='23514', CONSTRAINT='welfare_group_referenced', DETAIL=reference_details;
 END IF;
END;
$$;

CREATE OR REPLACE FUNCTION welfare_guard_group_lifecycle()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  PERFORM welfare_assert_group_mutable(OLD.id);
  RETURN OLD;
 END IF;
 IF (NEW.deleted_at IS DISTINCT FROM OLD.deleted_at AND NEW.deleted_at IS NOT NULL)
    OR (NEW.subscription_type IS DISTINCT FROM OLD.subscription_type AND NEW.subscription_type<>'subscription') THEN
  PERFORM welfare_assert_group_mutable(OLD.id);
 END IF;
 RETURN NEW;
END;
$$;

CREATE TRIGGER welfare_group_lifecycle_guard
 BEFORE UPDATE OF deleted_at,subscription_type OR DELETE ON groups
 FOR EACH ROW EXECUTE FUNCTION welfare_guard_group_lifecycle();

CREATE OR REPLACE FUNCTION welfare_lock_subscription_group(target_group BIGINT)
RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 -- SHARE conflicts with both NO KEY UPDATE (type/soft delete) and UPDATE (hard
 -- delete), unlike a foreign key's KEY SHARE. Writers lock campaign before group.
 PERFORM id FROM groups WHERE id=target_group AND deleted_at IS NULL
  AND subscription_type='subscription' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'welfare_group_unavailable'
   USING ERRCODE='23514', CONSTRAINT='welfare_group_unavailable';
 END IF;
END;
$$;

CREATE OR REPLACE FUNCTION welfare_guard_campaign_group_reference()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.payload->>'subscription_group_id' IS DISTINCT FROM OLD.payload->>'subscription_group_id' THEN
  PERFORM welfare_lock_subscription_group((NEW.payload->>'subscription_group_id')::bigint);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER welfare_campaign_group_reference_guard
 BEFORE INSERT OR UPDATE OF payload ON qq_invite_campaigns
 FOR EACH ROW EXECUTE FUNCTION welfare_guard_campaign_group_reference();
CREATE TRIGGER welfare_campaign_version_group_reference_guard
 BEFORE INSERT OR UPDATE OF payload ON qq_invite_campaign_versions
 FOR EACH ROW EXECUTE FUNCTION welfare_guard_campaign_group_reference();

CREATE OR REPLACE FUNCTION welfare_guard_code_group_reference()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.subscription_effect<>'' THEN
  IF EXISTS(SELECT 1 FROM qq_invite_code_pool WHERE redeem_code_id=NEW.id) THEN
   RAISE EXCEPTION 'qq_invite_welfare_supplement_not_allowed'
    USING ERRCODE='23514', CONSTRAINT='qq_invite_welfare_supplement_not_allowed';
  END IF;
  IF TG_OP='INSERT' OR NEW.group_id IS DISTINCT FROM OLD.group_id OR OLD.subscription_effect='' THEN
   PERFORM welfare_lock_subscription_group(NEW.group_id);
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER welfare_code_group_reference_guard
 BEFORE INSERT OR UPDATE OF group_id,subscription_effect ON redeem_codes
 FOR EACH ROW EXECUTE FUNCTION welfare_guard_code_group_reference();

CREATE OR REPLACE FUNCTION welfare_guard_subscription_group_reference()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.entitlement_kind='welfare' AND (TG_OP='INSERT' OR NEW.group_id IS DISTINCT FROM OLD.group_id OR OLD.entitlement_kind<>'welfare') THEN
  PERFORM welfare_lock_subscription_group(NEW.group_id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER welfare_subscription_group_reference_guard
 BEFORE INSERT OR UPDATE OF group_id,entitlement_kind ON user_subscriptions
 FOR EACH ROW EXECUTE FUNCTION welfare_guard_subscription_group_reference();

CREATE OR REPLACE FUNCTION welfare_guard_supplement_group_reference()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM welfare_lock_subscription_group(NEW.group_id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER welfare_supplement_group_reference_guard
 BEFORE INSERT OR UPDATE OF group_id ON user_welfare_subscription_supplements
 FOR EACH ROW EXECUTE FUNCTION welfare_guard_supplement_group_reference();

CREATE OR REPLACE FUNCTION qq_invite_guard_pool_supplement()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE effect TEXT;
BEGIN
 SELECT subscription_effect INTO effect FROM redeem_codes WHERE id=NEW.redeem_code_id FOR SHARE;
 IF effect<>'' THEN
  RAISE EXCEPTION 'qq_invite_welfare_supplement_not_allowed'
   USING ERRCODE='23514', CONSTRAINT='qq_invite_welfare_supplement_not_allowed';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER qq_invite_pool_supplement_guard
 BEFORE INSERT OR UPDATE OF redeem_code_id ON qq_invite_code_pool
 FOR EACH ROW EXECUTE FUNCTION qq_invite_guard_pool_supplement();

CREATE INDEX IF NOT EXISTS qq_invite_campaign_subscription_group_idx ON qq_invite_campaigns ((payload->>'subscription_group_id'));
CREATE INDEX IF NOT EXISTS qq_invite_campaign_version_subscription_group_idx ON qq_invite_campaign_versions ((payload->>'subscription_group_id'));
CREATE INDEX IF NOT EXISTS redeem_codes_welfare_group_idx ON redeem_codes(group_id) WHERE subscription_effect<>'';
