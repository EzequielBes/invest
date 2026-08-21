CREATE TABLE IF NOT EXISTS strategist_intent_applications (
    intent_id         TEXT NOT NULL,
    target_id         TEXT NOT NULL,
    analysis_run_id   TEXT NOT NULL,
    asset             TEXT NOT NULL,
    side              TEXT NOT NULL,
    confidence        DOUBLE PRECISION NOT NULL,
    sizing_pct        DOUBLE PRECISION NOT NULL,
    rationale         TEXT NOT NULL DEFAULT '',
    proposed_quantity DOUBLE PRECISION NOT NULL DEFAULT 0,
    proposed_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_allowed      BOOLEAN,
    risk_reasons      JSONB NOT NULL DEFAULT '[]',
    execution_status  TEXT NOT NULL,
    execution_order_id TEXT,
    execution_filled_quantity DOUBLE PRECISION,
    execution_filled_price DOUBLE PRECISION,
    created_at        TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (intent_id, target_id)
);
CREATE INDEX IF NOT EXISTS strategist_intent_applications_run_id ON strategist_intent_applications (analysis_run_id);
