CREATE TABLE IF NOT EXISTS strategist_intent_outcomes (
    analysis_run_id      TEXT NOT NULL,
    intent_id            TEXT NOT NULL,
    horizon_hours        INTEGER NOT NULL CHECK (horizon_hours IN (1, 4, 24)),
    direction_return_pct DOUBLE PRECISION NOT NULL,
    correct              BOOLEAN NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (analysis_run_id, intent_id, horizon_hours)
);

CREATE INDEX IF NOT EXISTS strategist_intent_outcomes_created_at
    ON strategist_intent_outcomes (created_at DESC);
