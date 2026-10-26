-- Publish the v3 check-in rules without rewriting balances, counters or
-- historical accounting facts. Preserve the program's enable/launch state
-- and audit timestamps; the migration runner applies this file atomically.
ALTER TABLE welfare_programs
    DROP CONSTRAINT IF EXISTS welfare_programs_rules_version_check;

ALTER TABLE welfare_programs
    ALTER COLUMN rules_version SET DEFAULT 'v3';

UPDATE welfare_programs SET rules_version = 'v3' WHERE rules_version = 'v2';

ALTER TABLE welfare_programs
    ADD CONSTRAINT welfare_programs_rules_version_check CHECK (rules_version = 'v3');
