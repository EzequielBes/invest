-- strategist/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS strategist_decisions (
    id                TEXT PRIMARY KEY,
    analysis_run_id   TEXT NOT NULL,
    asset             TEXT NOT NULL,
    side              TEXT NOT NULL,
    confidence        DOUBLE PRECISION NOT NULL,
    sizing_pct        DOUBLE PRECISION NOT NULL DEFAULT 0,
    rationale         TEXT NOT NULL DEFAULT '',
    proposed_quantity DOUBLE PRECISION NOT NULL DEFAULT 0,
    proposed_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_allowed      BOOLEAN,
    risk_reasons      JSONB NOT NULL DEFAULT '[]',
    created_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS strategist_decisions_run_id ON strategist_decisions (analysis_run_id);
