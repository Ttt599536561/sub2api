-- Publish the v4 lottery rules without rewriting balances, draw entitlements,
-- check-in progress or historical results. Preserve enable/launch state and
-- audit timestamps; the migration runner applies this file atomically.
ALTER TABLE welfare_programs
    DROP CONSTRAINT IF EXISTS welfare_programs_rules_version_check;

ALTER TABLE welfare_programs
    ALTER COLUMN rules_version SET DEFAULT 'v4';

UPDATE welfare_programs SET rules_version = 'v4' WHERE rules_version = 'v3';

ALTER TABLE welfare_programs
    ADD CONSTRAINT welfare_programs_rules_version_check CHECK (rules_version = 'v4');
