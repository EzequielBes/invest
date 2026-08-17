-- analysis/migrations/001_init.sql
CREATE TABLE IF NOT EXISTS analysis_runs (
    id          TEXT PRIMARY KEY,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    timeframe   TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    error       TEXT
);

CREATE TABLE IF NOT EXISTS analysis_results (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES analysis_runs(id),
    agent_type  TEXT NOT NULL,
    asset       TEXT NOT NULL DEFAULT '',
    indicators  JSONB NOT NULL,
    narrative   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS analysis_results_run_id ON analysis_results (run_id);
