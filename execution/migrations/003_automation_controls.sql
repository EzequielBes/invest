ALTER TABLE paper_state
    ADD COLUMN IF NOT EXISTS testnet_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS active_agent TEXT NOT NULL DEFAULT 'claude_code';

ALTER TABLE paper_state
    ADD CONSTRAINT paper_state_active_agent_check CHECK (active_agent IN ('claude_code', 'codex'));
