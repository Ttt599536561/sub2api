-- Administrative date reports select reward facts by their Shanghai business date.
-- Existing user/created_at indexes cannot restrict a global business_date range.
-- Covering the aggregate columns keeps append-only reward scans eligible for index-only access.
CREATE INDEX CONCURRENTLY IF NOT EXISTS welfare_ledger_reward_business_date_idx
    ON welfare_ledger (business_date, user_id)
    INCLUDE (type, amount_cents)
    WHERE type IN ('daily', 'streak', 'draw');
