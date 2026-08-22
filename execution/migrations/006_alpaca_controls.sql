ALTER TABLE paper_state
    ADD COLUMN IF NOT EXISTS alpaca_paper_enabled BOOLEAN NOT NULL DEFAULT false;
