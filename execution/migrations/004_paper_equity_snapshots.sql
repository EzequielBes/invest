CREATE TABLE IF NOT EXISTS paper_equity_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    cash            DOUBLE PRECISION NOT NULL,
    positions_value DOUBLE PRECISION NOT NULL,
    equity          DOUBLE PRECISION NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS paper_equity_snapshots_created_at ON paper_equity_snapshots (created_at);
