package storage

import (
	"context"

	validation "validation/internal/validation"
)

// Findings returns persisted findings for reporting after an audit completes.
func (s *Store) Findings(ctx context.Context, runID string) ([]validation.Finding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT severity, rule, message, evidence
		FROM validation_findings
		WHERE validation_run_id = $1
		ORDER BY created_at, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var findings []validation.Finding
	for rows.Next() {
		var finding validation.Finding
		if err := rows.Scan(&finding.Severity, &finding.Rule, &finding.Message, &finding.Evidence); err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}
