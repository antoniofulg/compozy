package rundb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/compozy/compozy/internal/store"
	"github.com/compozy/compozy/pkg/compozy/events"
	"github.com/compozy/compozy/pkg/compozy/events/kinds"
)

// convergenceProjector projects one canonical convergence event into the run.db
// projection tables. It runs inside the same transaction as the canonical event
// row so the projection and its event commit or roll back together, which is what
// keeps run.db canonical and the projections reconstructible from it.
type convergenceProjector func(ctx context.Context, tx *sql.Tx, item events.Event) error

// convergenceProjectors maps each canonical convergence event kind to its
// projection or identity validator. The dispatcher treats an absent kind as a
// no-op so non-convergence events skip it.
var convergenceProjectors = map[events.EventKind]convergenceProjector{
	events.EventKindConvergencePreflightCompleted:   projectConvergencePreflight,
	events.EventKindConvergencePhaseStarted:         projectConvergencePhaseStarted,
	events.EventKindConvergenceRouteSelected:        projectConvergenceRouteSelected,
	events.EventKindConvergenceVerificationComplete: projectConvergenceVerification,
	events.EventKindConvergenceReviewCompleted:      projectConvergenceReviewCompleted,
	events.EventKindConvergenceFindingChanged:       projectConvergenceFindingChanged,
	events.EventKindConvergenceBatchCompleted:       projectConvergenceBatch,
	events.EventKindConvergenceProgressEvaluated:    projectConvergenceProgress,
	events.EventKindConvergenceApprovalRequested:    projectConvergenceApprovalRequested,
	events.EventKindConvergenceApprovalDecided:      projectConvergenceApprovalDecided,
	events.EventKindConvergenceSegmentParked:        projectConvergenceSegmentParked,
	events.EventKindConvergenceSegmentCompleted:     projectConvergenceSegmentCompleted,
}

func requireConvergenceIdentity(
	ctx context.Context,
	tx *sql.Tx,
	runID, convergenceID string,
) error {
	var present int
	err := tx.QueryRowContext(
		ctx,
		`SELECT 1 FROM convergence_segments WHERE run_id = ? AND convergence_id = ?`,
		runID,
		convergenceID,
	).Scan(&present)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"%w: convergence %q does not own run %q",
			ErrConvergenceReplay,
			convergenceID,
			runID,
		)
	}
	return fmt.Errorf("rundb: validate convergence identity %q: %w", convergenceID, err)
}

// applyConvergenceProjection projects one convergence event into run.db. It is
// invoked for every event in a batch and is a fast no-op for non-convergence
// kinds so ordinary runs pay no projection cost.
func applyConvergenceProjection(ctx context.Context, tx *sql.Tx, item events.Event) error {
	projector, ok := convergenceProjectors[item.Kind]
	if !ok {
		return nil
	}
	if item.Kind != events.EventKindConvergencePreflightCompleted {
		if err := requireSegmentProjection(ctx, tx, item.RunID); err != nil {
			return err
		}
	}
	return projector(ctx, tx, item)
}

func requireSegmentProjection(ctx context.Context, tx *sql.Tx, runID string) error {
	var present int
	err := tx.QueryRowContext(
		ctx,
		`SELECT 1 FROM convergence_segments WHERE run_id = ?`,
		runID,
	).Scan(&present)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"%w: convergence segment %q is not seeded",
			ErrConvergenceReplay,
			runID,
		)
	}
	return fmt.Errorf("rundb: validate convergence segment %q: %w", runID, err)
}

func requirePhaseProjection(ctx context.Context, tx *sql.Tx, runID, phaseID string) error {
	var present int
	err := tx.QueryRowContext(
		ctx,
		`SELECT 1 FROM convergence_phases WHERE phase_id = ? AND run_id = ?`,
		phaseID,
		runID,
	).Scan(&present)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"%w: convergence phase %q does not belong to run %q",
			ErrConvergenceReplay,
			phaseID,
			runID,
		)
	}
	return fmt.Errorf("rundb: validate convergence phase %q: %w", phaseID, err)
}

func decodeConvergencePayload[T any](item events.Event) (T, error) {
	var payload T
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return payload, fmt.Errorf("rundb: decode %s payload: %w", item.Kind, err)
	}
	return payload, nil
}

func projectConvergencePreflight(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergencePreflightCompletedPayload](item)
	if err != nil {
		return err
	}
	convergenceID := payload.ResourceID
	ts := store.FormatTimestamp(item.Timestamp.UTC())
	// The full frozen config and target binding are seeded separately by the
	// coordinator; this upsert only refreshes the light fields the preflight event
	// owns so a prior seed is preserved.
	if payload.CorrelationID != item.RunID {
		return fmt.Errorf(
			"%w: preflight segment %q does not match event run %q",
			ErrConvergenceReplay,
			payload.CorrelationID,
			item.RunID,
		)
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_runs
			(convergence_id, run_id, request_id, target_snapshot, config_fingerprint, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(convergence_id) DO UPDATE SET
			run_id=excluded.run_id,
			request_id=excluded.request_id,
			target_snapshot=excluded.target_snapshot,
			config_fingerprint=excluded.config_fingerprint,
			updated_at=excluded.updated_at
		 WHERE convergence_runs.run_id IN ('', excluded.run_id)
		   AND convergence_runs.request_id IN ('', excluded.request_id)
		   AND convergence_runs.target_snapshot IN ('', excluded.target_snapshot)
		   AND convergence_runs.config_fingerprint IN ('', excluded.config_fingerprint)`,
		convergenceID, item.RunID, payload.RequestID, payload.TargetSnapshot, payload.ConfigFingerprint, ts,
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence run %q: %w", convergenceID, err)
	}
	if err := requireConvergenceMutation(result, "project convergence run", convergenceID); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx,
		`INSERT INTO convergence_segments (run_id, convergence_id, state, updated_at)
		 VALUES (?, ?, 'prepared', ?)
		 ON CONFLICT(run_id) DO UPDATE SET
			convergence_id=excluded.convergence_id,
			updated_at=excluded.updated_at
		 WHERE convergence_segments.convergence_id = excluded.convergence_id`,
		item.RunID, convergenceID, ts,
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence segment %q: %w", item.RunID, err)
	}
	return requireConvergenceMutation(result, "project convergence segment", item.RunID)
}

func projectConvergencePhaseStarted(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergencePhaseStartedPayload](item)
	if err != nil {
		return err
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	var owningRun string
	err = tx.QueryRowContext(
		ctx,
		`SELECT run_id FROM convergence_phases WHERE phase_id = ?`,
		payload.ResourceID,
	).Scan(&owningRun)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("rundb: validate convergence phase identity %q: %w", payload.ResourceID, err)
	}
	if err == nil && owningRun != item.RunID {
		return fmt.Errorf(
			"%w: convergence phase %q belongs to run %q",
			ErrConvergenceReplay,
			payload.ResourceID,
			owningRun,
		)
	}
	ts := store.FormatTimestamp(item.Timestamp.UTC())
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_phases
			(phase_id, run_id, phase, round, batch_id, attempt, snapshot, state, sequence, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
		 ON CONFLICT(phase_id) DO NOTHING`,
		payload.ResourceID, item.RunID, payload.Phase, payload.Round, payload.BatchID,
		payload.Attempt, payload.Snapshot, item.Seq, ts,
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence phase %q: %w", payload.ResourceID, err)
	}
	if err := requireConvergenceMutation(result, "project convergence phase", payload.ResourceID); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx,
		`UPDATE convergence_segments SET state='active', updated_at=? WHERE run_id=?`,
		ts, item.RunID,
	)
	if err != nil {
		return fmt.Errorf("rundb: mark segment %q active: %w", item.RunID, err)
	}
	return requireConvergenceMutation(result, "mark convergence segment active", item.RunID)
}

func projectConvergenceRouteSelected(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceRouteSelectedPayload](item)
	if err != nil {
		return err
	}
	if err := requirePhaseProjection(ctx, tx, item.RunID, payload.ResourceID); err != nil {
		return err
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_routes
			(phase_id, role, primary_route, selected_route, configuration_source, fallback_reason, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(phase_id, role) DO NOTHING`,
		payload.ResourceID, payload.Role, payload.Primary, payload.Selected,
		payload.ConfigurationSource, payload.FallbackReason, store.FormatTimestamp(item.Timestamp.UTC()),
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence route %q/%q: %w", payload.ResourceID, payload.Role, err)
	}
	return requireConvergenceMutation(
		result,
		"project convergence route",
		payload.ResourceID+"/"+payload.Role,
	)
}

func projectConvergenceVerification(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceVerificationCompletedPayload](item)
	if err != nil {
		return err
	}
	if err := requirePhaseProjection(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	passed := 0
	if payload.Outcome == "passed" {
		passed = 1
	}
	var exitCode sql.NullInt64
	if payload.ExitCode != nil {
		exitCode = sql.NullInt64{Int64: int64(*payload.ExitCode), Valid: true}
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_verifications
			(verification_id, run_id, phase_id, command_fingerprint, snapshot, exit_code,
			 passed, attempt, evidence_path, sequence, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?,
			COALESCE((SELECT attempt FROM convergence_phases WHERE phase_id = ? AND run_id = ?), 0),
			?, ?, ?)
		 ON CONFLICT(verification_id) DO NOTHING`,
		payload.ResourceID, item.RunID, payload.CorrelationID, payload.CommandFingerprint,
		payload.Snapshot, exitCode, passed, payload.CorrelationID, item.RunID,
		payload.EvidencePath, item.Seq,
		store.FormatTimestamp(item.Timestamp.UTC()),
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence verification %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence verification", payload.ResourceID)
}

func projectConvergenceReviewCompleted(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceReviewCompletedPayload](item)
	if err != nil {
		return err
	}
	return requirePhaseProjection(ctx, tx, item.RunID, payload.CorrelationID)
}

func projectConvergenceFindingChanged(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceFindingChangedPayload](item)
	if err != nil {
		return err
	}
	ts := store.FormatTimestamp(item.Timestamp.UTC())
	switch payload.Outcome {
	case "created", "updated", "resolved", "waived":
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO convergence_findings
				(fingerprint, run_id, state, severity, snapshot_seq, attempts, first_seq, evidence_ref, updated_at)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)
			 ON CONFLICT(fingerprint) DO UPDATE SET
				state=excluded.state,
				severity=excluded.severity,
				snapshot_seq=excluded.snapshot_seq,
				evidence_ref=excluded.evidence_ref,
				updated_at=excluded.updated_at`,
			payload.ResourceID, item.RunID, payload.State, payload.Severity, item.Seq,
			item.Seq, payload.EvidenceRef, ts,
		); err != nil {
			return fmt.Errorf("rundb: project convergence finding %q: %w", payload.ResourceID, err)
		}
	}
	return projectFindingHistory(ctx, tx, item, payload, ts)
}

func projectFindingHistory(
	ctx context.Context,
	tx *sql.Tx,
	item events.Event,
	payload kinds.ConvergenceFindingChangedPayload,
	ts string,
) error {
	switch payload.Outcome {
	case "created", "updated", "resolved":
		observationID := fmt.Sprintf("%s:%d", payload.ResourceID, item.Seq)
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO convergence_observations
				(observation_id, run_id, fingerprint, snapshot, snapshot_seq, severity, outcome, review_id, sequence, recorded_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			observationID, item.RunID, payload.ResourceID, payload.Snapshot, item.Seq, payload.Severity,
			payload.Outcome, payload.CorrelationID, item.Seq, ts,
		); err != nil {
			return fmt.Errorf("rundb: project observation %q: %w", observationID, err)
		}
	case "invalid", "duplicate", "waived":
		decisionID := fmt.Sprintf("%s:%d", payload.ResourceID, item.Seq)
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO convergence_dispositions
				(decision_id, run_id, fingerprint, disposition, actor_kind, reason, snapshot,
				 snapshot_seq, related_fingerprint, sequence, recorded_at)
			 VALUES (?, ?, ?, ?, '', ?, ?, ?, '', ?, ?)`,
			decisionID, item.RunID, payload.ResourceID, payload.Outcome, payload.DispositionReason,
			payload.Snapshot, item.Seq, item.Seq, ts,
		); err != nil {
			return fmt.Errorf("rundb: project disposition %q: %w", decisionID, err)
		}
	}
	return nil
}

func projectConvergenceBatch(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceBatchCompletedPayload](item)
	if err != nil {
		return err
	}
	if err := requirePhaseProjection(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	fingerprints, err := json.Marshal(payload.FindingFingerprints)
	if err != nil {
		return fmt.Errorf("rundb: encode convergence batch fingerprints: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_batches
			(batch_id, run_id, phase_id, finding_fingerprints_json, before_snapshot,
			 after_snapshot, status, affected_ref, sequence, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(batch_id) DO NOTHING`,
		payload.ResourceID, item.RunID, payload.CorrelationID, string(fingerprints), payload.BeforeSnapshot,
		payload.AfterSnapshot, payload.Outcome, payload.AffectedPathsRef, item.Seq,
		store.FormatTimestamp(item.Timestamp.UTC()),
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence batch %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence batch", payload.ResourceID)
}

func projectConvergenceProgress(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceProgressEvaluatedPayload](item)
	if err != nil {
		return err
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_rounds
			(round_id, run_id, number, resolved, severity_decreased, verification_improved,
			 no_progress_count, oscillation_count, admitted_at, sequence, updated_at)
		 VALUES (?, ?,
			COALESCE((SELECT MAX(round) FROM convergence_phases WHERE run_id = ?), 0),
			?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(round_id) DO NOTHING`,
		payload.ResourceID, item.RunID, item.RunID,
		boolToInt(payload.Resolved), boolToInt(payload.SeverityDecreased),
		boolToInt(payload.VerificationImproved), payload.NoProgressCount, payload.OscillationCount,
		store.FormatTimestamp(item.Timestamp.UTC()), item.Seq, store.FormatTimestamp(item.Timestamp.UTC()),
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence round %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence round", payload.ResourceID)
}

func projectConvergenceApprovalRequested(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceApprovalRequestedPayload](item)
	if err != nil {
		return err
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_approvals
			(proposal_id, run_id, fingerprint, action, snapshot, decision, reason, evidence_ref, sequence, updated_at)
		 VALUES (?, ?, ?, ?, ?, '', '', ?, ?, ?)
		 ON CONFLICT(proposal_id) DO NOTHING`,
		payload.ResourceID, item.RunID, payload.ProposalFingerprint, payload.Action,
		payload.Snapshot, payload.EvidenceRef, item.Seq, store.FormatTimestamp(item.Timestamp.UTC()),
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence approval %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence approval", payload.ResourceID)
}

func projectConvergenceApprovalDecided(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceApprovalDecidedPayload](item)
	if err != nil {
		return err
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE convergence_approvals
		 SET decision=?, reason=?, updated_at=?
		 WHERE proposal_id=? AND run_id=? AND fingerprint=? AND snapshot=?`,
		payload.Decision, payload.Reason, store.FormatTimestamp(item.Timestamp.UTC()), payload.ResourceID,
		item.RunID, payload.ProposalFingerprint, payload.Snapshot,
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence approval decision %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence approval decision", payload.ResourceID)
}

func projectConvergenceSegmentParked(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceSegmentParkedPayload](item)
	if err != nil {
		return err
	}
	if payload.ResourceID != item.RunID {
		return fmt.Errorf(
			"%w: parked segment %q does not match event run %q",
			ErrConvergenceReplay,
			payload.ResourceID,
			item.RunID,
		)
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	// A parked segment is terminal and its resume cursor is claimable exactly once.
	// The cursor is derived from the terminal sequence so it is deterministic and
	// unique per terminal segment without depending on external state.
	cursor := ""
	if payload.ResumeAvailable {
		cursor = fmt.Sprintf("resume:%d", item.Seq)
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE convergence_segments
		 SET state='terminal', terminal_kind='parked', terminal_reason=?, terminal_seq=?,
			 resume_cursor=?, updated_at=?
		 WHERE run_id=?`,
		payload.Reason, item.Seq, cursor, store.FormatTimestamp(item.Timestamp.UTC()), payload.ResourceID,
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence parked segment %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence parked segment", payload.ResourceID)
}

func projectConvergenceSegmentCompleted(ctx context.Context, tx *sql.Tx, item events.Event) error {
	payload, err := decodeConvergencePayload[kinds.ConvergenceSegmentCompletedPayload](item)
	if err != nil {
		return err
	}
	if payload.ResourceID != item.RunID {
		return fmt.Errorf(
			"%w: completed segment %q does not match event run %q",
			ErrConvergenceReplay,
			payload.ResourceID,
			item.RunID,
		)
	}
	if err := requireConvergenceIdentity(ctx, tx, item.RunID, payload.CorrelationID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE convergence_segments
		 SET state='terminal', terminal_kind='clean', terminal_reason='', terminal_seq=?, updated_at=?
		 WHERE run_id=?`,
		item.Seq, store.FormatTimestamp(item.Timestamp.UTC()), payload.ResourceID,
	)
	if err != nil {
		return fmt.Errorf("rundb: project convergence completed segment %q: %w", payload.ResourceID, err)
	}
	return requireConvergenceMutation(result, "project convergence completed segment", payload.ResourceID)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
