-- analysis/migrations/002_rankings.sql
-- The qualitative assessment belongs in analysis_results; this table stores
-- the deterministic, Go-computed ordering consumed by paper trading.
CREATE TABLE IF NOT EXISTS analysis_rankings (
    run_id                    TEXT NOT NULL REFERENCES analysis_runs(id),
    asset                     TEXT NOT NULL,
    rank                      INTEGER NOT NULL CHECK (rank > 0),
    composite_score           DOUBLE PRECISION NOT NULL,
    opportunity_score_raw     DOUBLE PRECISION NOT NULL,
    thesis                    TEXT NOT NULL,
    confidence                DOUBLE PRECISION NOT NULL,
    evidence                  JSONB NOT NULL DEFAULT '[]',
    computed_at               TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (run_id, asset)
);
CREATE INDEX IF NOT EXISTS analysis_rankings_run_rank ON analysis_rankings (run_id, rank);
