-- risk-engine/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS risk_limits (
    id                     INT PRIMARY KEY DEFAULT 1,
    max_pct_per_asset      DOUBLE PRECISION NOT NULL,
    max_pct_crypto_total   DOUBLE PRECISION NOT NULL,
    max_value_per_trade    DOUBLE PRECISION NOT NULL,
    max_daily_loss         DOUBLE PRECISION NOT NULL,
    max_weekly_loss        DOUBLE PRECISION NOT NULL,
    max_drawdown           DOUBLE PRECISION NOT NULL,
    max_consecutive_losses INT NOT NULL,
    max_volatility         DOUBLE PRECISION NOT NULL,
    min_liquidity          DOUBLE PRECISION NOT NULL,
    max_data_age_minutes   INT NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT risk_limits_single_row CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS risk_state (
    id         INT PRIMARY KEY DEFAULT 1,
    status     TEXT NOT NULL DEFAULT 'normal',
    reason     TEXT NOT NULL DEFAULT 'initial state',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT risk_state_single_row CHECK (id = 1),
    CONSTRAINT risk_state_valid_status CHECK (status IN ('normal', 'paused', 'kill_switch'))
);

CREATE TABLE IF NOT EXISTS risk_decisions (
    id            BIGSERIAL PRIMARY KEY,
    ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
    asset         TEXT NOT NULL,
    side          TEXT NOT NULL,
    quantity      DOUBLE PRECISION NOT NULL,
    value         DOUBLE PRECISION NOT NULL,
    allowed       BOOLEAN NOT NULL,
    reasons       JSONB NOT NULL,
    rules_checked JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS risk_decisions_ts ON risk_decisions (ts DESC);

INSERT INTO risk_limits (
    id, max_pct_per_asset, max_pct_crypto_total, max_value_per_trade,
    max_daily_loss, max_weekly_loss, max_drawdown, max_consecutive_losses,
    max_volatility, min_liquidity, max_data_age_minutes
) VALUES (
    1, 0.20, 1.00, 1000,
    0.05, 0.10, 0.20, 5,
    0.15, 100000, 30
) ON CONFLICT (id) DO NOTHING;

INSERT INTO risk_state (id, status, reason) VALUES (1, 'normal', 'initial state')
ON CONFLICT (id) DO NOTHING;
