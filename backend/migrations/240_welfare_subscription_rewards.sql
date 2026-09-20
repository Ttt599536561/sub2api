-- Package purchases grant whole draws per order's CNY payment. They never
-- contribute to eligible_spend, which remains actual API balance consumption.
ALTER TABLE welfare_wallets ADD COLUMN subscription_draws BIGINT NOT NULL DEFAULT 0
    CHECK (subscription_draws >= 0);

CREATE TABLE welfare_subscription_rewards (
    order_id BIGINT PRIMARY KEY REFERENCES payment_orders(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    order_amount NUMERIC(20,2) NOT NULL CHECK (order_amount > 0),
    paid_amount_cny NUMERIC(20,2) NOT NULL CHECK (paid_amount_cny >= 50),
    paid_at TIMESTAMPTZ NOT NULL,
    granted_draws BIGINT NOT NULL CHECK (granted_draws > 0),
    reversed_draws BIGINT NOT NULL DEFAULT 0 CHECK (reversed_draws BETWEEN 0 AND granted_draws),
    refunded_amount_cny NUMERIC(20,2) NOT NULL DEFAULT 0
        CHECK (refunded_amount_cny BETWEEN 0 AND paid_amount_cny),
    refund_amount NUMERIC(20,2),
    refund_status TEXT,
    refund_reason TEXT,
    refund_processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (granted_draws = floor(paid_amount_cny / 50)),
    CHECK (granted_draws - reversed_draws = floor((paid_amount_cny - refunded_amount_cny) / 50)),
    CHECK ((refund_processed_at IS NULL AND refund_amount IS NULL AND refund_status IS NULL
            AND refund_reason IS NULL AND reversed_draws = 0 AND refunded_amount_cny = 0)
        OR (refund_processed_at IS NOT NULL AND refund_amount IS NOT NULL AND refund_amount > 0
            AND refund_status IS NOT NULL AND refund_status IN ('REFUNDED', 'PARTIALLY_REFUNDED')
            AND refund_reason IS NOT NULL AND refund_reason ~ '[^[:space:]]'))
);
CREATE INDEX welfare_subscription_rewards_user_idx ON welfare_subscription_rewards(user_id, created_at DESC, order_id DESC);

-- Keep the original entitlement immutable while allowing its single confirmed
-- refund to be recorded atomically with the wallet adjustment.
CREATE FUNCTION welfare_subscription_reward_audit_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'welfare subscription rewards cannot be deleted';
    END IF;
    IF ROW(NEW.order_id, NEW.user_id, NEW.order_amount, NEW.paid_amount_cny, NEW.paid_at, NEW.granted_draws, NEW.created_at)
        IS DISTINCT FROM ROW(OLD.order_id, OLD.user_id, OLD.order_amount, OLD.paid_amount_cny, OLD.paid_at, OLD.granted_draws, OLD.created_at)
        OR OLD.refund_processed_at IS NOT NULL THEN
        RAISE EXCEPTION 'welfare subscription reward audit is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER welfare_subscription_rewards_audit_guard
    BEFORE UPDATE OR DELETE ON welfare_subscription_rewards
    FOR EACH ROW EXECUTE FUNCTION welfare_subscription_reward_audit_guard();
