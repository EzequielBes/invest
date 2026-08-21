ALTER TABLE strategist_intent_applications
    DROP CONSTRAINT strategist_intent_applications_pkey,
    ADD PRIMARY KEY (analysis_run_id, intent_id, target_id);

CREATE INDEX IF NOT EXISTS strategist_intent_applications_run_intent
    ON strategist_intent_applications (analysis_run_id, intent_id);
