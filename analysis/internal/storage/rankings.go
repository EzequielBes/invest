package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ranking is analysis's persisted deterministic ordering for a cycle.
type Ranking struct {
	RunID               string
	Asset               string
	Rank                int
	CompositeScore      float64
	OpportunityScoreRaw float64
	Thesis              string
	Confidence          float64
	Evidence            json.RawMessage
	ComputedAt          time.Time
}

// SaveRankings persists a complete recalculated ranking for one run.
func (s *Store) SaveRankings(ctx context.Context, rankings []Ranking) error {
	if len(rankings) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, ranking := range rankings {
		evidence := ranking.Evidence
		if len(evidence) == 0 {
			evidence = []byte("[]")
		}
		batch.Queue(`
			INSERT INTO analysis_rankings
				(run_id, asset, rank, composite_score, opportunity_score_raw, thesis, confidence, evidence, computed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (run_id, asset) DO UPDATE SET
				rank = EXCLUDED.rank,
				composite_score = EXCLUDED.composite_score,
				opportunity_score_raw = EXCLUDED.opportunity_score_raw,
				thesis = EXCLUDED.thesis,
				confidence = EXCLUDED.confidence,
				evidence = EXCLUDED.evidence,
				computed_at = EXCLUDED.computed_at
		`, ranking.RunID, ranking.Asset, ranking.Rank, ranking.CompositeScore, ranking.OpportunityScoreRaw,
			ranking.Thesis, ranking.Confidence, evidence, ranking.ComputedAt)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range rankings {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}
