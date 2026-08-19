-- strategist/migrations/002_execution_outcome.sql
ALTER TABLE strategist_decisions
    ADD COLUMN IF NOT EXISTS execution_status TEXT,
    ADD COLUMN IF NOT EXISTS execution_order_id TEXT,
    ADD COLUMN IF NOT EXISTS execution_filled_quantity DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS execution_filled_price DOUBLE PRECISION;
