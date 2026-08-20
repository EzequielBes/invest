CREATE TABLE IF NOT EXISTS paper_state (
    id         INT PRIMARY KEY DEFAULT 1,
    cash       DOUBLE PRECISION NOT NULL,
    positions  JSONB NOT NULL DEFAULT '{}',
    enabled    BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT paper_state_single_row CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS paper_fills (
    id         TEXT PRIMARY KEY,
    asset      TEXT NOT NULL,
    side       TEXT NOT NULL,
    quantity   DOUBLE PRECISION NOT NULL,
    price      DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS paper_fills_created_at ON paper_fills (created_at DESC);

-- Tracks which strategist_decisions rows came from a paper (simulated)
-- run, so web-api can exclude them from the real Decisions dashboard.
CREATE TABLE IF NOT EXISTS paper_decision_ids (
    id         TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL
);
