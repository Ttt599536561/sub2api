CREATE TABLE IF NOT EXISTS affiliate_promotion_hosts (
    hostname VARCHAR(253) PRIMARY KEY,
    inviter_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (hostname <> '' AND hostname = lower(hostname))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_affiliate_promotion_hosts_active_user
    ON affiliate_promotion_hosts(inviter_id) WHERE enabled = true AND inviter_id IS NOT NULL;

-- A hard user deletion executes ON DELETE SET NULL. Keep the hostname as a
-- disabled tombstone and revoke snapshots, including when the FK acts directly.
CREATE OR REPLACE FUNCTION maintain_affiliate_promotion_host_revision()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.inviter_id IS DISTINCT FROM NEW.inviter_id OR OLD.enabled IS DISTINCT FROM NEW.enabled THEN
        NEW.revision := GREATEST(NEW.revision, OLD.revision + 1);
        NEW.updated_at := NOW();
    END IF;
    IF NEW.inviter_id IS NULL THEN
        NEW.enabled := false;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_affiliate_promotion_host_revision ON affiliate_promotion_hosts;
CREATE TRIGGER trg_affiliate_promotion_host_revision
    BEFORE UPDATE ON affiliate_promotion_hosts
    FOR EACH ROW EXECUTE FUNCTION maintain_affiliate_promotion_host_revision();

-- Normal account deletion is a soft delete in this application, so the FK
-- alone cannot revoke its host. Keep the user-before-host lock order used by
-- signup validation, and do not revive mappings when an account is restored.
CREATE OR REPLACE FUNCTION revoke_deleted_user_affiliate_promotion_hosts()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE affiliate_promotion_hosts
       SET enabled=false, revision=revision+1, updated_at=NOW()
     WHERE inviter_id=NEW.id AND enabled=true;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_revoke_deleted_user_affiliate_promotion_hosts ON users;
CREATE TRIGGER trg_revoke_deleted_user_affiliate_promotion_hosts
    AFTER UPDATE OF deleted_at ON users
    FOR EACH ROW
    WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION revoke_deleted_user_affiliate_promotion_hosts();

COMMENT ON TABLE affiliate_promotion_hosts IS 'Promotion host mappings and permanent revoked-host tombstones';
COMMENT ON COLUMN affiliate_promotion_hosts.revision IS 'Monotonic version invalidating captured referral snapshots after changes';
