CREATE TABLE IF NOT EXISTS backtest_runs (
    id                TEXT PRIMARY KEY,
    strategy_name     TEXT NOT NULL,
    period_start      TIMESTAMPTZ NOT NULL,
    period_end        TIMESTAMPTZ NOT NULL,
    timeframes        TEXT[] NOT NULL,
    driving_timeframe TEXT NOT NULL,
    initial_cash      DOUBLE PRECISION NOT NULL,
    fee_pct           DOUBLE PRECISION NOT NULL,
    status            TEXT NOT NULL DEFAULT 'running',
    error             TEXT,
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at          TIMESTAMPTZ,
    CONSTRAINT backtest_runs_valid_status CHECK (status IN ('running', 'completed', 'failed'))
);

CREATE TABLE IF NOT EXISTS backtest_trades (
    id            BIGSERIAL PRIMARY KEY,
    run_id        TEXT NOT NULL REFERENCES backtest_runs(id),
    ts            TIMESTAMPTZ NOT NULL,
    asset         TEXT NOT NULL,
    side          TEXT NOT NULL,
    quantity      DOUBLE PRECISION NOT NULL,
    price         DOUBLE PRECISION NOT NULL,
    fee           DOUBLE PRECISION NOT NULL,
    allowed       BOOLEAN NOT NULL,
    reject_reason TEXT
);
CREATE INDEX IF NOT EXISTS backtest_trades_run_id ON backtest_trades (run_id, ts);

CREATE TABLE IF NOT EXISTS backtest_equity_curve (
    id              BIGSERIAL PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES backtest_runs(id),
    ts              TIMESTAMPTZ NOT NULL,
    cash            DOUBLE PRECISION NOT NULL,
    positions_value DOUBLE PRECISION NOT NULL,
    total_equity    DOUBLE PRECISION NOT NULL
);
CREATE INDEX IF NOT EXISTS backtest_equity_curve_run_id ON backtest_equity_curve (run_id, ts);

CREATE TABLE IF NOT EXISTS backtest_results (
    run_id                    TEXT PRIMARY KEY REFERENCES backtest_runs(id),
    total_return_pct          DOUBLE PRECISION NOT NULL,
    max_drawdown_pct          DOUBLE PRECISION NOT NULL,
    sharpe_ratio              DOUBLE PRECISION NOT NULL,
    sortino_ratio             DOUBLE PRECISION NOT NULL,
    annualized_volatility_pct DOUBLE PRECISION NOT NULL,
    win_rate_pct              DOUBLE PRECISION NOT NULL,
    total_trades              INT NOT NULL,
    avg_trade_pct             DOUBLE PRECISION NOT NULL
);
