CREATE TABLE IF NOT EXISTS validation_hypotheses (
    id                    TEXT PRIMARY KEY,
    description           TEXT NOT NULL,
    universe              TEXT NOT NULL,
    horizon               TEXT NOT NULL,
    cost_policy           TEXT NOT NULL,
    primary_metrics       JSONB NOT NULL,
    availability_rule     TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS validation_runs (
    id                    TEXT PRIMARY KEY,
    hypothesis_id         TEXT NOT NULL REFERENCES validation_hypotheses(id) ON DELETE CASCADE,
    status                TEXT NOT NULL,
    config                JSONB NOT NULL,
    config_hash           TEXT NOT NULL,
    backtest_run_id       TEXT,
    git_commit            TEXT,
    error                 TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at          TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS validation_splits (
    id                    TEXT PRIMARY KEY,
    validation_run_id     TEXT NOT NULL REFERENCES validation_runs(id) ON DELETE CASCADE,
    kind                  TEXT NOT NULL,
    start_at              TIMESTAMPTZ NOT NULL,
    end_at                TIMESTAMPTZ NOT NULL,
    embargo_minutes       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS validation_metrics (
    id                    TEXT PRIMARY KEY,
    validation_run_id     TEXT NOT NULL REFERENCES validation_runs(id) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    value                 DOUBLE PRECISION NOT NULL,
    segment               TEXT NOT NULL,
    unit                  TEXT NOT NULL,
    evidence              JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS validation_findings (
    id                    TEXT PRIMARY KEY,
    validation_run_id     TEXT NOT NULL REFERENCES validation_runs(id) ON DELETE CASCADE,
    severity              TEXT NOT NULL,
    rule                  TEXT NOT NULL,
    message               TEXT NOT NULL,
    evidence              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS validation_attempts (
    id                    TEXT PRIMARY KEY,
    hypothesis_id         TEXT NOT NULL REFERENCES validation_hypotheses(id) ON DELETE CASCADE,
    validation_run_id     TEXT REFERENCES validation_runs(id) ON DELETE SET NULL,
    variant               TEXT NOT NULL,
    config                JSONB NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS validation_runs_hypothesis_id ON validation_runs (hypothesis_id);
CREATE INDEX IF NOT EXISTS validation_splits_run_id ON validation_splits (validation_run_id);
CREATE INDEX IF NOT EXISTS validation_findings_run_id ON validation_findings (validation_run_id);
