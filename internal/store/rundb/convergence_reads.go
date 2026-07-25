package rundb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/compozy/compozy/internal/store"
)

// ConvergenceRoutes reads selected routes in phase order.
func (r *RunDB) ConvergenceRoutes(ctx context.Context, runID string) ([]ConvergenceRouteRow, error) {
	if err := r.requireContext(ctx, "read convergence routes"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT route.phase_id, route.role, route.primary_route, route.selected_route,
			route.configuration_source, route.fallback_reason
		 FROM convergence_routes AS route
		 JOIN convergence_phases AS phase ON phase.phase_id = route.phase_id
		 WHERE phase.run_id = ?
		 ORDER BY phase.sequence ASC, route.role ASC`,
		strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence routes for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceRouteRow, 0)
	for rows.Next() {
		var row ConvergenceRouteRow
		if err := rows.Scan(&row.PhaseID, &row.Role, &row.Primary, &row.Selected,
			&row.ConfigurationSource, &row.FallbackReason); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence route: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence routes: %w", err)
	}
	return result, nil
}

// ConvergenceRounds reads projected round evaluations in canonical order.
func (r *RunDB) ConvergenceRounds(ctx context.Context, runID string) ([]ConvergenceRoundRow, error) {
	if err := r.requireContext(ctx, "read convergence rounds"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT round_id, number, admitted_at, resolved, severity_decreased, verification_improved,
			no_progress_count, oscillation_count, terminal_reason
		 FROM convergence_rounds WHERE run_id = ? ORDER BY sequence ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence rounds for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceRoundRow, 0)
	for rows.Next() {
		var (
			row            ConvergenceRoundRow
			admittedAt     string
			resolved       int
			severityDown   int
			verifyImproved int
		)
		if err := rows.Scan(&row.RoundID, &row.Number, &admittedAt,
			&resolved, &severityDown, &verifyImproved,
			&row.NoProgressCount, &row.OscillationCount, &row.TerminalReason); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence round: %w", err)
		}
		row.Resolved = resolved == 1
		row.SeverityDecreased = severityDown == 1
		row.VerificationImproved = verifyImproved == 1
		if admittedAt != "" {
			row.AdmittedAt, err = store.ParseTimestamp(admittedAt)
			if err != nil {
				return nil, fmt.Errorf("rundb: parse convergence round admitted_at: %w", err)
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence rounds: %w", err)
	}
	return result, nil
}

// ConvergenceBatches reads durable correction checkpoints in canonical order.
func (r *RunDB) ConvergenceBatches(ctx context.Context, runID string) ([]ConvergenceBatchRow, error) {
	if err := r.requireContext(ctx, "read convergence batches"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT batch_id, phase_id, finding_fingerprints_json, before_snapshot,
			after_snapshot, status, affected_ref
		 FROM convergence_batches WHERE run_id = ? ORDER BY sequence ASC`,
		strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence batches for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceBatchRow, 0)
	for rows.Next() {
		var (
			row          ConvergenceBatchRow
			fingerprints string
		)
		if err := rows.Scan(&row.BatchID, &row.PhaseID, &fingerprints, &row.BeforeSnapshot,
			&row.AfterSnapshot, &row.Status, &row.AffectedRef); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence batch: %w", err)
		}
		if err := json.Unmarshal([]byte(fingerprints), &row.FindingFingerprints); err != nil {
			return nil, fmt.Errorf("rundb: decode convergence batch %q fingerprints: %w", row.BatchID, err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence batches: %w", err)
	}
	return result, nil
}

// ConvergenceObservations reads immutable finding observations in canonical order.
func (r *RunDB) ConvergenceObservations(
	ctx context.Context,
	runID string,
) ([]ConvergenceObservationRow, error) {
	if err := r.requireContext(ctx, "read convergence observations"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT observation_id, fingerprint, snapshot, snapshot_seq, severity, outcome, review_id
		 FROM convergence_observations WHERE run_id = ? ORDER BY sequence ASC`,
		strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence observations for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceObservationRow, 0)
	for rows.Next() {
		var row ConvergenceObservationRow
		if err := rows.Scan(&row.ObservationID, &row.Fingerprint, &row.Snapshot, &row.SnapshotSeq,
			&row.Severity, &row.Outcome, &row.ReviewID); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence observation: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence observations: %w", err)
	}
	return result, nil
}

// ConvergenceDispositions reads immutable finding decisions in canonical order.
func (r *RunDB) ConvergenceDispositions(
	ctx context.Context,
	runID string,
) ([]ConvergenceDispositionRow, error) {
	if err := r.requireContext(ctx, "read convergence dispositions"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT decision_id, fingerprint, disposition, actor_kind, reason, snapshot,
			snapshot_seq, related_fingerprint
		 FROM convergence_dispositions WHERE run_id = ? ORDER BY sequence ASC`,
		strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence dispositions for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceDispositionRow, 0)
	for rows.Next() {
		var row ConvergenceDispositionRow
		if err := rows.Scan(&row.DecisionID, &row.Fingerprint, &row.Disposition,
			&row.ActorKind, &row.Reason, &row.Snapshot, &row.SnapshotSeq,
			&row.RelatedFingerprint); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence disposition: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence dispositions: %w", err)
	}
	return result, nil
}

// ConvergenceFindings reads projected current finding state in first-seen order.
func (r *RunDB) ConvergenceFindings(ctx context.Context, runID string) ([]ConvergenceFindingRow, error) {
	if err := r.requireContext(ctx, "read convergence findings"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT fingerprint, state, severity, snapshot_seq, attempts, first_seq, evidence_ref
		 FROM convergence_findings WHERE run_id = ? ORDER BY first_seq ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence findings for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceFindingRow, 0)
	for rows.Next() {
		var row ConvergenceFindingRow
		if err := rows.Scan(&row.Fingerprint, &row.State, &row.Severity, &row.SnapshotSeq,
			&row.Attempts, &row.FirstSeq, &row.EvidenceRef); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence finding: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence findings: %w", err)
	}
	return result, nil
}

// ConvergenceVerifications reads projected verification results in canonical order.
func (r *RunDB) ConvergenceVerifications(
	ctx context.Context,
	runID string,
) ([]ConvergenceVerificationRow, error) {
	if err := r.requireContext(ctx, "read convergence verifications"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT verification_id, phase_id, command_fingerprint, snapshot, exit_code, passed, attempt, evidence_path
		 FROM convergence_verifications WHERE run_id = ? ORDER BY sequence ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence verifications for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceVerificationRow, 0)
	for rows.Next() {
		var (
			row      ConvergenceVerificationRow
			exitCode sql.NullInt64
			passed   int
		)
		if err := rows.Scan(&row.VerificationID, &row.PhaseID, &row.CommandFingerprint, &row.Snapshot,
			&exitCode, &passed, &row.Attempt, &row.EvidencePath); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence verification: %w", err)
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			row.ExitCode = &code
		}
		row.Passed = passed == 1
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence verifications: %w", err)
	}
	return result, nil
}

// ConvergenceApprovals reads projected approval proposals in canonical order.
func (r *RunDB) ConvergenceApprovals(ctx context.Context, runID string) ([]ConvergenceApprovalRow, error) {
	if err := r.requireContext(ctx, "read convergence approvals"); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT proposal_id, fingerprint, action, snapshot, decision, reason, evidence_ref
		 FROM convergence_approvals WHERE run_id = ? ORDER BY sequence ASC`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("rundb: query convergence approvals for %q: %w", runID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceApprovalRow, 0)
	for rows.Next() {
		var row ConvergenceApprovalRow
		if err := rows.Scan(&row.ProposalID, &row.Fingerprint, &row.Action, &row.Snapshot,
			&row.Decision, &row.Reason, &row.EvidenceRef); err != nil {
			return nil, fmt.Errorf("rundb: scan convergence approval: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rundb: iterate convergence approvals: %w", err)
	}
	return result, nil
}
