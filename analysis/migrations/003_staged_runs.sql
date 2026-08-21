-- Staged MCP submissions retry safely by replacing their one result slot.
ALTER TABLE analysis_runs ALTER COLUMN status SET DEFAULT 'pending';
CREATE UNIQUE INDEX IF NOT EXISTS analysis_results_run_agent_asset
    ON analysis_results (run_id, agent_type, asset);
