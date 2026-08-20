CREATE TABLE IF NOT EXISTS equity_snapshots (
    id               BIGSERIAL PRIMARY KEY,
    ts               TIMESTAMPTZ NOT NULL,
    cash             DOUBLE PRECISION NOT NULL,
    positions_value  DOUBLE PRECISION NOT NULL,
    total_equity     DOUBLE PRECISION NOT NULL
);

CREATE INDEX IF NOT EXISTS equity_snapshots_ts ON equity_snapshots (ts DESC);
