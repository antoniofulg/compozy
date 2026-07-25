// Package convergencestore is the durable convergence store. It binds the pure
// convergence domain types to the per-run SQLite projection in internal/store/rundb
// without either package importing the other: rundb owns convergence-free row
// readers and the guarded canonical append, and this package maps those rows to
// and from the domain Snapshot the receipt and read models project from. run.db
// stays canonical; the receipt and global index are reconstructible from a
// Snapshot returned here.
package convergencestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/store/rundb"
	"github.com/compozy/compozy/pkg/compozy/events"
)

// Store is the run-local canonical convergence store backed by one run.db.
type Store struct {
	db *rundb.RunDB
}

// ConvergenceStore is the durable store contract a coordinator consumes. It is
// declared here (not in the pure domain) because Apply accepts the transport event
// envelope, which the pure domain must not import.
type ConvergenceStore interface {
	Apply(ctx context.Context, event events.Event) error
	Snapshot(ctx context.Context, runID string) (convergence.Snapshot, error)
	ClaimResume(ctx context.Context, req convergence.ResumeRequest) (convergence.Segment, bool, error)
}

var _ ConvergenceStore = (*Store)(nil)

// New wraps a per-run store as a durable convergence store.
func New(db *rundb.RunDB) *Store {
	return &Store{db: db}
}

// Apply appends one canonical convergence event and its projection atomically. It
// maps the store's replay rejection to the domain replay sentinel so callers match
// a single error identity regardless of which layer detected the conflict.
func (s *Store) Apply(ctx context.Context, event events.Event) error {
	err := s.db.ApplyConvergenceEvent(ctx, event)
	if err != nil && errors.Is(err, rundb.ErrConvergenceReplay) {
		return fmt.Errorf("%w: %s", convergence.ErrReplayConflict, err.Error())
	}
	return err
}

// Seed persists the frozen configuration and target binding for one segment. The
// coordinator calls it at preflight so the receipt and snapshot carry the full
// frozen configuration, which no event payload carries.
func (s *Store) Seed(
	ctx context.Context,
	convergenceID string,
	segment convergence.Segment,
	target convergence.TargetBinding,
	config convergence.FrozenConfiguration,
) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("convergencestore: marshal frozen config: %w", err)
	}
	state := segment.State
	if state == "" {
		state = convergence.SegmentPrepared
	}
	return s.db.SeedConvergenceRun(ctx, rundb.ConvergenceRunSeed{
		ConvergenceID:  convergenceID,
		RunID:          segment.RunID,
		Ordinal:        segment.Ordinal,
		PreviousRunID:  segment.PreviousRunID,
		SourceRunID:    segment.SourceRunID,
		State:          string(state),
		WorkspaceID:    target.WorkspaceID,
		ExecutionScope: target.ExecutionScope,
		TaskGroupID:    target.TaskGroupID,
		Branch:         target.Branch,
		Worktree:       target.Worktree,
		TargetSnapshot: target.Snapshot,
		ConfigJSON:     string(configJSON),
	})
}

// Snapshot reconstructs the canonical projected state of one convergence segment.
func (s *Store) Snapshot(ctx context.Context, runID string) (convergence.Snapshot, error) {
	segmentRow, err := s.db.ConvergenceSegment(ctx, runID)
	if err != nil {
		return convergence.Snapshot{}, err
	}
	segment := segmentFromRow(segmentRow)
	snapshot := convergence.Snapshot{
		ConvergenceID: segmentRow.ConvergenceID,
		Segment:       segment,
		Terminal:      segment.Terminal,
	}
	if err := s.fillRun(ctx, segmentRow.ConvergenceID, &snapshot); err != nil {
		return convergence.Snapshot{}, err
	}
	if err := s.fillProjections(ctx, runID, &snapshot); err != nil {
		return convergence.Snapshot{}, err
	}
	if snapshot.LastSeq, err = s.db.CurrentMaxSequence(ctx); err != nil {
		return convergence.Snapshot{}, err
	}
	return snapshot, nil
}

func (s *Store) fillRun(ctx context.Context, convergenceID string, snapshot *convergence.Snapshot) error {
	runRow, ok, err := s.db.ConvergenceRun(ctx, convergenceID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	snapshot.RequestID = runRow.RequestID
	snapshot.Target = convergence.TargetBinding{
		WorkspaceID:    runRow.WorkspaceID,
		ExecutionScope: runRow.ExecutionScope,
		TaskGroupID:    runRow.TaskGroupID,
		Branch:         runRow.Branch,
		Worktree:       runRow.Worktree,
		Snapshot:       runRow.TargetSnapshot,
	}
	if runRow.ConfigJSON != "" && runRow.ConfigJSON != "{}" {
		var config convergence.FrozenConfiguration
		if err := json.Unmarshal([]byte(runRow.ConfigJSON), &config); err != nil {
			return fmt.Errorf("convergencestore: decode frozen config %q: %w", convergenceID, err)
		}
		snapshot.Config = config
	}
	return nil
}

func (s *Store) fillProjections(ctx context.Context, runID string, snapshot *convergence.Snapshot) error {
	phaseRow, ok, err := s.db.ConvergenceLatestPhase(ctx, runID)
	if err != nil {
		return err
	}
	if ok {
		snapshot.Phase = phaseFromRow(phaseRow)
	}
	routeRows, err := s.db.ConvergenceRoutes(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Routes = routesFromRows(routeRows)
	roundRows, err := s.db.ConvergenceRounds(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Rounds = roundsFromRows(roundRows)
	batchRows, err := s.db.ConvergenceBatches(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Batches = batchesFromRows(batchRows)
	findingRows, err := s.db.ConvergenceFindings(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Findings = findingsFromRows(findingRows)
	observationRows, err := s.db.ConvergenceObservations(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Observations = observationsFromRows(observationRows)
	dispositionRows, err := s.db.ConvergenceDispositions(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Dispositions = dispositionsFromRows(dispositionRows)
	verificationRows, err := s.db.ConvergenceVerifications(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Verification = verificationsFromRows(verificationRows)
	approvalRows, err := s.db.ConvergenceApprovals(ctx, runID)
	if err != nil {
		return err
	}
	snapshot.Approvals = approvalsFromRows(approvalRows)
	return nil
}

// ClaimResume claims a parked segment's resume cursor at most once and returns the
// new (or replayed) segment.
func (s *Store) ClaimResume(
	ctx context.Context,
	req convergence.ResumeRequest,
) (convergence.Segment, bool, error) {
	row, claimed, err := s.db.ClaimConvergenceResume(
		ctx,
		req.ConvergenceID,
		req.PreviousRunID,
		req.ExpectedCursor,
		req.NewRunID,
	)
	if err != nil {
		if errors.Is(err, rundb.ErrConvergenceReplay) {
			return convergence.Segment{}, false, fmt.Errorf("%w: %s", convergence.ErrReplayConflict, err.Error())
		}
		return convergence.Segment{}, false, err
	}
	return segmentFromRow(row), claimed, nil
}
