package rundb

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/compozy/compozy/internal/store"
	"github.com/compozy/compozy/pkg/compozy/events"
)

// Convergence store errors. They are run-local so the per-run store owns no
// convergence domain types (which would create an import cycle through the model
// and journal packages). A higher layer maps them to convergence sentinels.
var (
	// ErrConvergenceRunNotFound reports that no convergence segment exists for a run.
	ErrConvergenceRunNotFound = errors.New("rundb: convergence run not found")
	// ErrConvergenceReplay reports a duplicate, out-of-order, or post-terminal append
	// that the deterministic canonical store rejects.
	ErrConvergenceReplay = errors.New("rundb: convergence replay conflict")
)

// ConvergenceSegmentRow is one convergence segment projection row. run.db is the
// canonical projection authority; every field is reconstructible from the ordered
// convergence events committed alongside it.
type ConvergenceSegmentRow struct {
	RunID          string
	ConvergenceID  string
	Ordinal        int
	PreviousRunID  string
	SourceRunID    string
	State          string
	ResumeCursor   string
	ResumeClaimed  bool
	TerminalKind   string
	TerminalReason string
	TerminalSeq    uint64
}

// ConvergenceRunRow is the frozen target binding and configuration projection.
type ConvergenceRunRow struct {
	ConvergenceID  string
	RunID          string
	RequestID      string
	WorkspaceID    string
	ExecutionScope string
	TaskGroupID    string
	Branch         string
	Worktree       string
	TargetSnapshot string
	ConfigJSON     string
}

// ConvergencePhaseRow is one projected phase state.
type ConvergencePhaseRow struct {
	PhaseID  string
	Phase    string
	Round    int
	BatchID  string
	Attempt  int
	Snapshot string
	State    string
}

// ConvergenceRoundRow is one projected round evaluation.
type ConvergenceRoundRow struct {
	RoundID              string
	Number               int
	AdmittedAt           time.Time
	Resolved             bool
	SeverityDecreased    bool
	VerificationImproved bool
	NoProgressCount      int
	OscillationCount     int
	TerminalReason       string
}

// ConvergenceFindingRow is one projected current finding state.
type ConvergenceFindingRow struct {
	Fingerprint string
	State       string
	Severity    string
	SnapshotSeq uint64
	Attempts    int
	FirstSeq    uint64
	EvidenceRef string
}

// ConvergenceVerificationRow is one projected verification result.
type ConvergenceVerificationRow struct {
	VerificationID     string
	PhaseID            string
	CommandFingerprint string
	Snapshot           string
	ExitCode           *int
	Passed             bool
	Attempt            int
	EvidencePath       string
}

// ConvergenceApprovalRow is one projected approval proposal and decision.
type ConvergenceApprovalRow struct {
	ProposalID  string
	Fingerprint string
	Action      string
	Snapshot    string
	Decision    string
	Reason      string
	EvidenceRef string
}

// ConvergenceRouteRow is one selected route projection.
type ConvergenceRouteRow struct {
	PhaseID             string
	Role                string
	Primary             string
	Selected            string
	ConfigurationSource string
	FallbackReason      string
}

// ConvergenceBatchRow is one durable correction batch projection.
type ConvergenceBatchRow struct {
	BatchID             string
	PhaseID             string
	FindingFingerprints []string
	BeforeSnapshot      string
	AfterSnapshot       string
	Status              string
	AffectedRef         string
}

// ConvergenceObservationRow is one immutable finding observation.
type ConvergenceObservationRow struct {
	ObservationID string
	Fingerprint   string
	Snapshot      string
	SnapshotSeq   uint64
	Severity      string
	Outcome       string
	ReviewID      string
}

// ConvergenceDispositionRow is one immutable finding disposition.
type ConvergenceDispositionRow struct {
	DecisionID         string
	Fingerprint        string
	Disposition        string
	ActorKind          string
	Reason             string
	Snapshot           string
	SnapshotSeq        uint64
	RelatedFingerprint string
}

// ConvergenceRunSeed seeds the frozen configuration and target binding for one
// convergence segment. No event payload carries the full frozen configuration, so
// the coordinator seeds it at preflight; the receipt and snapshot project it back.
type ConvergenceRunSeed struct {
	ConvergenceID  string
	RunID          string
	Ordinal        int
	PreviousRunID  string
	SourceRunID    string
	State          string
	WorkspaceID    string
	ExecutionScope string
	TaskGroupID    string
	Branch         string
	Worktree       string
	TargetSnapshot string
	ConfigJSON     string
}

// ApplyConvergenceEvent appends one canonical convergence event and its run.db
// projection inside a single transaction. It is idempotent under at-least-once
// replay: a sequence that already exists is a no-op. It rejects an out-of-order
// canonical append and any append after a segment terminal event so a terminal
// segment stays immutable.
func (r *RunDB) ApplyConvergenceEvent(ctx context.Context, event events.Event) error {
	if err := r.requireContext(ctx, "apply convergence event"); err != nil {
		return err
	}
	if event.Seq == 0 {
		return fmt.Errorf("%w: convergence event requires a positive sequence", ErrConvergenceReplay)
	}
	return r.StoreEventBatch(ctx, []events.Event{event})
}

func validateConvergenceAppend(
	ctx context.Context,
	tx *sql.Tx,
	event events.Event,
) (bool, error) {
	if !isGuardedConvergenceEvent(event.Kind) {
		return false, nil
	}
	if event.Seq == 0 {
		return false, fmt.Errorf(
			"%w: convergence event requires a positive sequence",
			ErrConvergenceReplay,
		)
	}
	matches, present, err := convergenceEventSeqMatches(ctx, tx, event)
	if err != nil {
		return false, err
	}
	if present {
		if matches {
			return true, nil
		}
		return false, fmt.Errorf(
			"%w: convergence event seq %d conflicts with the committed canonical event",
			ErrConvergenceReplay,
			event.Seq,
		)
	}
	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM events`).Scan(&maxSeq); err != nil {
		return false, fmt.Errorf("rundb: query max convergence sequence: %w", err)
	}
	maxSequence, err := sequenceValue(maxSeq.Int64, "convergence event sequence")
	if maxSeq.Valid && err != nil {
		return false, fmt.Errorf("%w: %w", ErrConvergenceReplay, err)
	}
	if maxSeq.Valid && event.Seq <= maxSequence {
		return false, fmt.Errorf(
			"%w: convergence event seq %d is not after %d",
			ErrConvergenceReplay,
			event.Seq,
			maxSeq.Int64,
		)
	}
	terminalSeq, err := convergenceTerminalSeqFrom(ctx, tx, event.RunID)
	if err != nil {
		return false, err
	}
	if terminalSeq > 0 && event.Seq > terminalSeq {
		return false, fmt.Errorf(
			"%w: convergence segment %q is terminal at seq %d",
			ErrConvergenceReplay,
			event.RunID,
			terminalSeq,
		)
	}
	return false, nil
}

func isGuardedConvergenceEvent(kind events.EventKind) bool {
	return strings.HasPrefix(string(kind), "convergence.") ||
		kind == events.EventKindTaskConvergenceRequested ||
		kind == events.EventKindTaskConvergenceContinued
}

func convergenceEventSeqMatches(
	ctx context.Context,
	q rowQuerier,
	event events.Event,
) (bool, bool, error) {
	var (
		kind      string
		payload   string
		timestamp string
	)
	err := q.QueryRowContext(
		ctx,
		`SELECT event_kind, payload_json, timestamp FROM events WHERE sequence = ?`,
		event.Seq,
	).Scan(&kind, &payload, &timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("rundb: read convergence event sequence %d: %w", event.Seq, err)
	}
	parsed, err := store.ParseTimestamp(timestamp)
	if err != nil {
		return false, true, err
	}
	matches := events.EventKind(kind) == event.Kind &&
		bytes.Equal([]byte(payload), event.Payload) &&
		parsed.Equal(event.Timestamp.UTC())
	return matches, true, nil
}

func convergenceTerminalSeqFrom(ctx context.Context, q rowQuerier, runID string) (uint64, error) {
	var terminalSeq sql.NullInt64
	err := q.QueryRowContext(
		ctx,
		`SELECT terminal_seq FROM convergence_segments WHERE run_id = ?`,
		strings.TrimSpace(runID),
	).Scan(&terminalSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("rundb: read convergence terminal seq for %q: %w", runID, err)
	}
	if !terminalSeq.Valid || terminalSeq.Int64 <= 0 {
		return 0, nil
	}
	return uint64(terminalSeq.Int64), nil
}

// SeedConvergenceRun persists the frozen configuration and target binding for one
// convergence segment in a single transaction.
func (r *RunDB) SeedConvergenceRun(ctx context.Context, seed ConvergenceRunSeed) (retErr error) {
	if err := r.requireContext(ctx, "seed convergence run"); err != nil {
		return err
	}
	r.convergeMu.Lock()
	defer r.convergeMu.Unlock()
	seed.ConvergenceID = strings.TrimSpace(seed.ConvergenceID)
	seed.RunID = strings.TrimSpace(seed.RunID)
	if seed.ConvergenceID == "" || seed.RunID == "" {
		return fmt.Errorf("rundb: convergence and run identity are required to seed a run")
	}
	if strings.TrimSpace(seed.State) == "" {
		seed.State = "prepared"
	}
	if strings.TrimSpace(seed.ConfigJSON) == "" {
		seed.ConfigJSON = "{}"
	}
	ts := store.FormatTimestamp(r.now())
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rundb: begin seed convergence run: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				retErr = errors.Join(retErr, fmt.Errorf("rundb: rollback seed convergence run: %w", rollbackErr))
			}
		}
	}()
	if err := seedConvergenceRunRow(ctx, tx, seed, ts); err != nil {
		return err
	}
	if err := seedConvergenceSegmentRow(ctx, tx, seed, ts); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rundb: commit seed convergence run: %w", err)
	}
	committed = true
	return nil
}

func seedConvergenceRunRow(ctx context.Context, tx *sql.Tx, seed ConvergenceRunSeed, ts string) error {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_runs
			(convergence_id, run_id, workspace_id, execution_scope, task_group_id, branch, worktree,
			 target_snapshot, config_json, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(convergence_id) DO UPDATE SET
			run_id=excluded.run_id,
			workspace_id=excluded.workspace_id,
			execution_scope=excluded.execution_scope,
			task_group_id=excluded.task_group_id,
			branch=excluded.branch,
			worktree=excluded.worktree,
			target_snapshot=excluded.target_snapshot,
			config_json=excluded.config_json,
			updated_at=excluded.updated_at
		 WHERE convergence_runs.run_id IN ('', excluded.run_id)`,
		seed.ConvergenceID, seed.RunID, seed.WorkspaceID, seed.ExecutionScope, seed.TaskGroupID,
		seed.Branch, seed.Worktree, seed.TargetSnapshot, seed.ConfigJSON, ts,
	)
	if err != nil {
		return fmt.Errorf("rundb: seed convergence run %q: %w", seed.ConvergenceID, err)
	}
	return requireConvergenceMutation(result, "seed convergence run", seed.ConvergenceID)
}

func seedConvergenceSegmentRow(ctx context.Context, tx *sql.Tx, seed ConvergenceRunSeed, ts string) error {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_segments
			(run_id, convergence_id, ordinal, previous_run_id, source_run_id, state, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
			convergence_id=excluded.convergence_id,
			ordinal=excluded.ordinal,
			previous_run_id=excluded.previous_run_id,
			source_run_id=excluded.source_run_id,
			updated_at=excluded.updated_at
		 WHERE convergence_segments.convergence_id = excluded.convergence_id`,
		seed.RunID, seed.ConvergenceID, seed.Ordinal, seed.PreviousRunID,
		seed.SourceRunID, seed.State, ts,
	)
	if err != nil {
		return fmt.Errorf("rundb: seed convergence segment %q: %w", seed.RunID, err)
	}
	return requireConvergenceMutation(result, "seed convergence segment", seed.RunID)
}

func requireConvergenceMutation(result sql.Result, operation, resourceID string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rundb: %s rows affected: %w", operation, err)
	}
	if affected != 1 {
		return fmt.Errorf(
			"%w: %s rejected stale identity %q",
			ErrConvergenceReplay,
			operation,
			resourceID,
		)
	}
	return nil
}

// ConvergenceSegment reads the projected segment row for one run.
func (r *RunDB) ConvergenceSegment(ctx context.Context, runID string) (ConvergenceSegmentRow, error) {
	if err := r.requireContext(ctx, "read convergence segment"); err != nil {
		return ConvergenceSegmentRow{}, err
	}
	return readConvergenceSegmentRow(ctx, r.db, strings.TrimSpace(runID))
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readConvergenceSegmentRow(
	ctx context.Context,
	q rowQuerier,
	runID string,
) (ConvergenceSegmentRow, error) {
	var (
		row         ConvergenceSegmentRow
		claimed     int
		terminalSeq sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT convergence_id, ordinal, previous_run_id, source_run_id, state,
			resume_cursor, resume_claimed, terminal_kind, terminal_reason, terminal_seq
		 FROM convergence_segments WHERE run_id = ?`, runID,
	).Scan(&row.ConvergenceID, &row.Ordinal, &row.PreviousRunID, &row.SourceRunID, &row.State,
		&row.ResumeCursor, &claimed, &row.TerminalKind, &row.TerminalReason, &terminalSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return ConvergenceSegmentRow{}, fmt.Errorf("%w: run %q", ErrConvergenceRunNotFound, runID)
	}
	if err != nil {
		return ConvergenceSegmentRow{}, fmt.Errorf("rundb: read convergence segment %q: %w", runID, err)
	}
	row.RunID = runID
	row.ResumeClaimed = claimed == 1
	if terminalSeq.Valid && terminalSeq.Int64 > 0 {
		row.TerminalSeq = uint64(terminalSeq.Int64)
	}
	return row, nil
}

// ConvergenceRun reads the frozen run projection by convergence identity.
func (r *RunDB) ConvergenceRun(ctx context.Context, convergenceID string) (ConvergenceRunRow, bool, error) {
	if err := r.requireContext(ctx, "read convergence run"); err != nil {
		return ConvergenceRunRow{}, false, err
	}
	row := ConvergenceRunRow{ConvergenceID: strings.TrimSpace(convergenceID)}
	err := r.db.QueryRowContext(ctx,
		`SELECT run_id, request_id, workspace_id, execution_scope, task_group_id, branch, worktree,
			target_snapshot, config_json
		 FROM convergence_runs WHERE convergence_id = ?`, row.ConvergenceID,
	).Scan(&row.RunID, &row.RequestID, &row.WorkspaceID, &row.ExecutionScope, &row.TaskGroupID,
		&row.Branch, &row.Worktree, &row.TargetSnapshot, &row.ConfigJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ConvergenceRunRow{}, false, nil
	}
	if err != nil {
		return ConvergenceRunRow{}, false, fmt.Errorf("rundb: read convergence run %q: %w", convergenceID, err)
	}
	return row, true, nil
}

// ConvergenceLatestPhase reads the most recent projected phase for one run.
func (r *RunDB) ConvergenceLatestPhase(ctx context.Context, runID string) (ConvergencePhaseRow, bool, error) {
	if err := r.requireContext(ctx, "read convergence phase"); err != nil {
		return ConvergencePhaseRow{}, false, err
	}
	var row ConvergencePhaseRow
	err := r.db.QueryRowContext(ctx,
		`SELECT phase_id, phase, round, batch_id, attempt, snapshot, state
		 FROM convergence_phases WHERE run_id = ? ORDER BY sequence DESC LIMIT 1`,
		strings.TrimSpace(runID),
	).Scan(&row.PhaseID, &row.Phase, &row.Round, &row.BatchID, &row.Attempt, &row.Snapshot, &row.State)
	if errors.Is(err, sql.ErrNoRows) {
		return ConvergencePhaseRow{}, false, nil
	}
	if err != nil {
		return ConvergencePhaseRow{}, false, fmt.Errorf("rundb: read convergence phase for %q: %w", runID, err)
	}
	return row, true, nil
}
