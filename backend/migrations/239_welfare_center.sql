-- Welfare is opt-in. Existing users acquire an independent zero-balance wallet
-- lazily; no historical spend is imported before the first enable timestamp.
CREATE TABLE IF NOT EXISTS welfare_programs (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    launch_at TIMESTAMPTZ,
    rules_version TEXT NOT NULL DEFAULT 'v2' CHECK (rules_version = 'v2'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO welfare_programs (id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS welfare_wallets (
    user_id BIGINT PRIMARY KEY REFERENCES users(id),
    balance_cents BIGINT NOT NULL DEFAULT 0 CHECK (balance_cents >= 0),
    total_earned_cents BIGINT NOT NULL DEFAULT 0 CHECK (total_earned_cents >= 0),
    total_redeemed_cents BIGINT NOT NULL DEFAULT 0 CHECK (total_redeemed_cents >= 0),
    total_checkin_days BIGINT NOT NULL DEFAULT 0 CHECK (total_checkin_days >= 0),
    cycle_id BIGINT NOT NULL DEFAULT 0 CHECK (cycle_id >= 0),
    cycle_day BIGINT NOT NULL DEFAULT 0 CHECK (cycle_day BETWEEN 0 AND 30),
    last_checkin_date DATE,
    daily_low_count SMALLINT NOT NULL DEFAULT 0 CHECK (daily_low_count BETWEEN 0 AND 3),
    eligible_spend NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (eligible_spend >= 0),
    draws_used BIGINT NOT NULL DEFAULT 0 CHECK (draws_used >= 0),
    wallet_version BIGINT NOT NULL DEFAULT 0 CHECK (wallet_version >= 0),
    welfare_balance_version BIGINT NOT NULL DEFAULT 0 CHECK (welfare_balance_version >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (balance_cents = total_earned_cents - total_redeemed_cents)
);

CREATE TABLE IF NOT EXISTS welfare_operations (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    type TEXT NOT NULL CHECK (type IN ('checkin','draw','redeem')),
    idempotency_key VARCHAR(128) NOT NULL,
    request_fingerprint TEXT NOT NULL,
    result JSONB NOT NULL,
    private_audit JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, type, idempotency_key)
);
CREATE INDEX IF NOT EXISTS welfare_operations_user_date_idx ON welfare_operations(user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS welfare_checkins (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    business_date DATE NOT NULL,
    operation_id UUID NOT NULL UNIQUE REFERENCES welfare_operations(id),
    daily_cents BIGINT NOT NULL CHECK (daily_cents > 0),
    streak_cents BIGINT NOT NULL DEFAULT 0 CHECK (streak_cents >= 0),
    cycle_id BIGINT NOT NULL,
    cycle_day BIGINT NOT NULL CHECK (cycle_day BETWEEN 1 AND 30),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, business_date)
);

CREATE TABLE IF NOT EXISTS welfare_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    operation_id UUID NOT NULL REFERENCES welfare_operations(id),
    type TEXT NOT NULL CHECK (type IN ('daily','streak','draw','redeem')),
    amount_cents BIGINT NOT NULL,
    balance_after_cents BIGINT NOT NULL CHECK (balance_after_cents >= 0),
    business_date DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (operation_id, type),
    CHECK ((type = 'redeem' AND amount_cents < 0) OR (type <> 'redeem' AND amount_cents > 0))
);
CREATE INDEX IF NOT EXISTS welfare_ledger_user_date_idx ON welfare_ledger(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS welfare_ledger_user_type_date_idx ON welfare_ledger(user_id, type, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS welfare_spend_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('debit','refund')),
    amount NUMERIC(20,8) NOT NULL,
    refund_source_id TEXT,
    actor_id BIGINT REFERENCES users(id),
    reason TEXT,
    ticket_delta BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_type, source_id, action),
    CHECK ((action = 'debit' AND amount > 0 AND refund_source_id IS NULL) OR
           (action = 'refund' AND amount < 0 AND refund_source_id IS NOT NULL)),
    CHECK (action <> 'refund' OR
           (actor_id IS NOT NULL AND actor_id > 0 AND reason IS NOT NULL AND reason ~ '[^[:space:]]'))
);
CREATE INDEX IF NOT EXISTS welfare_spend_events_user_date_idx ON welfare_spend_events(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS welfare_spend_events_refund_idx ON welfare_spend_events(source_type,refund_source_id) WHERE action='refund';

CREATE TABLE IF NOT EXISTS welfare_balance_outbox (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    version BIGINT NOT NULL,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    lease_token TEXT,
    completed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, version)
);
CREATE INDEX IF NOT EXISTS welfare_balance_outbox_pending_idx ON welfare_balance_outbox(available_at,id) WHERE completed_at IS NULL;

-- Accounting facts and results are append-only, including for SQL callers.
CREATE OR REPLACE FUNCTION welfare_reject_fact_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'welfare accounting facts are immutable';
END;
$$ LANGUAGE plpgsql;
DO $$
DECLARE tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY['welfare_ledger','welfare_operations','welfare_checkins','welfare_spend_events'] LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = tbl || '_immutable') THEN
            EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION welfare_reject_fact_mutation()', tbl || '_immutable', tbl);
        END IF;
    END LOOP;
END;
$$;
