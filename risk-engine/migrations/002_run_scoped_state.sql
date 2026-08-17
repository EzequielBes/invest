-- risk-engine/migrations/002_run_scoped_state.sql
-- Drops the singleton constraint on risk_state so a backtest run can get
-- its own row (run_id set), while the live row (run_id IS NULL, id=1)
-- keeps behaving exactly as before this migration.
ALTER TABLE risk_state DROP CONSTRAINT IF EXISTS risk_state_single_row;

CREATE SEQUENCE IF NOT EXISTS risk_state_id_seq START WITH 2;
ALTER TABLE risk_state ALTER COLUMN id SET DEFAULT nextval('risk_state_id_seq');
ALTER SEQUENCE risk_state_id_seq OWNED BY risk_state.id;

ALTER TABLE risk_state ADD COLUMN IF NOT EXISTS run_id TEXT NULL;
ALTER TABLE risk_decisions ADD COLUMN IF NOT EXISTS run_id TEXT NULL;

-- At most one live row (run_id IS NULL) and at most one row per run_id.
CREATE UNIQUE INDEX IF NOT EXISTS risk_state_live_row ON risk_state ((1)) WHERE run_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS risk_state_run_row ON risk_state (run_id) WHERE run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS risk_decisions_run_id ON risk_decisions (run_id) WHERE run_id IS NOT NULL;
