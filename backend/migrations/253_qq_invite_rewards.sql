-- Independent NapCat welfare ledger. No existing QQ image identity is reused.
CREATE TABLE IF NOT EXISTS qq_invite_config_versions (
 id BIGSERIAL PRIMARY KEY, payload JSONB NOT NULL, secrets BYTEA NOT NULL,
 actor_id BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS qq_invite_campaigns (
 id BIGSERIAL PRIMARY KEY, version BIGINT NOT NULL DEFAULT 1, payload JSONB NOT NULL,
 baseline_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS qq_invite_campaign_versions (
 campaign_id BIGINT NOT NULL REFERENCES qq_invite_campaigns(id), version BIGINT NOT NULL,
 payload JSONB NOT NULL, actor_id BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(campaign_id,version)
);
CREATE TABLE IF NOT EXISTS qq_invite_baseline (
 campaign_id BIGINT NOT NULL REFERENCES qq_invite_campaigns(id),group_id TEXT NOT NULL,qq TEXT NOT NULL,
 PRIMARY KEY(campaign_id,group_id,qq)
);
CREATE TABLE IF NOT EXISTS qq_invite_events (
 id BIGSERIAL PRIMARY KEY,event_key TEXT NOT NULL UNIQUE,payload JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS qq_invite_records (
 id BIGSERIAL PRIMARY KEY,campaign_id BIGINT NOT NULL REFERENCES qq_invite_campaigns(id),campaign_version BIGINT NOT NULL,
 snapshot JSONB NOT NULL,self_id TEXT NOT NULL,group_id TEXT NOT NULL,invitee_qq TEXT NOT NULL,inviter_qq TEXT NOT NULL DEFAULT '',
 qq_level INT,status TEXT NOT NULL DEFAULT 'received',reason TEXT NOT NULL DEFAULT '',event_time TIMESTAMPTZ NOT NULL,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),lease_token TEXT,lease_until TIMESTAMPTZ,last_claim_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(campaign_id,invitee_qq),FOREIGN KEY(campaign_id,campaign_version) REFERENCES qq_invite_campaign_versions(campaign_id,version)
);
CREATE INDEX IF NOT EXISTS qq_invite_records_queue ON qq_invite_records(status,next_attempt_at);
CREATE TABLE IF NOT EXISTS qq_invite_joins (
 campaign_id BIGINT NOT NULL REFERENCES qq_invite_campaigns(id),group_id TEXT NOT NULL,qq TEXT NOT NULL,
 PRIMARY KEY(campaign_id,group_id,qq)
);
CREATE TABLE IF NOT EXISTS qq_invite_code_pool (
 id BIGSERIAL PRIMARY KEY,campaign_id BIGINT NOT NULL REFERENCES qq_invite_campaigns(id),
 redeem_code_id BIGINT NOT NULL UNIQUE REFERENCES redeem_codes(id) ON DELETE RESTRICT,
 record_id BIGINT UNIQUE REFERENCES qq_invite_records(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS qq_invite_code_pool_available ON qq_invite_code_pool(campaign_id,id) WHERE record_id IS NULL;
CREATE TABLE IF NOT EXISTS qq_invite_rewards (
 id BIGSERIAL PRIMARY KEY,record_id BIGINT NOT NULL UNIQUE REFERENCES qq_invite_records(id),
 pool_id BIGINT NOT NULL UNIQUE REFERENCES qq_invite_code_pool(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS qq_invite_outbox (
 id BIGSERIAL PRIMARY KEY,record_id BIGINT NOT NULL REFERENCES qq_invite_records(id),kind TEXT NOT NULL,
 destination TEXT NOT NULL,mention TEXT NOT NULL DEFAULT '',template TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'pending',
 attempts INT NOT NULL DEFAULT 0,next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),lease_token TEXT,lease_until TIMESTAMPTZ,
 message_id TEXT NOT NULL DEFAULT '',last_error TEXT NOT NULL DEFAULT '',created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(record_id,kind,destination)
);
CREATE INDEX IF NOT EXISTS qq_invite_outbox_queue ON qq_invite_outbox(status,next_attempt_at);
CREATE TABLE IF NOT EXISTS qq_invite_audit (
 id BIGSERIAL PRIMARY KEY,actor_id BIGINT NOT NULL,action TEXT NOT NULL,entity_id BIGINT NOT NULL,
 detail JSONB NOT NULL DEFAULT '{}',created_at TIMESTAMPTZ NOT NULL DEFAULT now()
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
 IF assigned IS NULL AND (NEW.status IS DISTINCT FROM OLD.status OR NEW.used_by IS DISTINCT FROM OLD.used_by OR NEW.used_at IS DISTINCT FROM OLD.used_at) THEN
   RAISE EXCEPTION 'qq_invite_pool_unallocated';
 END IF;
 IF assigned IS NOT NULL AND (NEW.status IS DISTINCT FROM OLD.status OR NEW.used_by IS DISTINCT FROM OLD.used_by OR NEW.used_at IS DISTINCT FROM OLD.used_at)
    AND NOT (OLD.status='unused' AND NEW.status='used' AND NEW.used_by IS NOT NULL AND NEW.used_at IS NOT NULL) THEN
   RAISE EXCEPTION 'qq_invite_pool_redemption_transition';
 END IF;
 RETURN NEW;
END;
$$;
DO $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='qq_invite_redeem_code_guard' AND tgrelid='redeem_codes'::regclass AND NOT tgisinternal) THEN
  CREATE TRIGGER qq_invite_redeem_code_guard BEFORE UPDATE ON redeem_codes FOR EACH ROW EXECUTE FUNCTION qq_invite_protect_redeem_code();
 END IF;
END;
$$;
