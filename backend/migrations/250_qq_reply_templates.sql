-- Reply text revisions have their own lock and do not change QQ credentials,
-- connection identity, grants, order snapshots or financial records.
CREATE TABLE qq_reply_template_revisions (
    revision BIGINT PRIMARY KEY CHECK (revision > 0),
    overrides JSONB NOT NULL CHECK (jsonb_typeof(overrides) = 'object'),
    actor_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    restored_from_revision BIGINT REFERENCES qq_reply_template_revisions(revision)
);
CREATE TABLE qq_reply_template_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    active_revision BIGINT REFERENCES qq_reply_template_revisions(revision)
);
INSERT INTO qq_reply_template_state(id, active_revision) VALUES (1, NULL);

CREATE FUNCTION qq_reply_template_revision_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'QQ reply template revisions are immutable';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER qq_reply_template_revision_immutable
BEFORE UPDATE OR DELETE ON qq_reply_template_revisions
FOR EACH ROW EXECUTE FUNCTION qq_reply_template_revision_immutable();
