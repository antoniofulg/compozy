package rundb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/compozy/compozy/internal/store"
)

const convergenceTerminalParked = "parked"

// ClaimConvergenceResume claims a parked segment's resume cursor at most once and
// links a new active segment to the same convergence identity. The claim and the
// new segment commit in one transaction, so two racing resumes cannot both win:
// the loser sees the cursor already claimed and replays the linked segment. A
// cursor mismatch or a non-parked previous segment is rejected without creating a
// segment, and the immutable consumed history stays on the previous run. The
// returned bool reports whether this call performed the claim (true) or replayed
// an existing one (false).
func (r *RunDB) ClaimConvergenceResume(
	ctx context.Context,
	expectedConvergenceID string,
	previousRunID string,
	expectedCursor string,
	newRunID string,
) (result ConvergenceSegmentRow, didClaim bool, retErr error) {
	if err := r.requireContext(ctx, "claim convergence resume"); err != nil {
		return ConvergenceSegmentRow{}, false, err
	}
	r.convergeMu.Lock()
	defer r.convergeMu.Unlock()
	previousRunID = strings.TrimSpace(previousRunID)
	expectedConvergenceID = strings.TrimSpace(expectedConvergenceID)
	newRunID = strings.TrimSpace(newRunID)
	if expectedConvergenceID == "" || previousRunID == "" || newRunID == "" {
		return ConvergenceSegmentRow{}, false, fmt.Errorf(
			"%w: resume requires convergence, previous, and new run identity", ErrConvergenceReplay)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ConvergenceSegmentRow{}, false, fmt.Errorf("rundb: begin claim resume: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				retErr = errors.Join(
					retErr,
					fmt.Errorf("rundb: rollback claim convergence resume: %w", rollbackErr),
				)
			}
		}
	}()

	prev, err := loadResumeTarget(ctx, tx, expectedConvergenceID, previousRunID, expectedCursor)
	if err != nil {
		return ConvergenceSegmentRow{}, false, err
	}

	ts := store.FormatTimestamp(r.now())
	if prev.ResumeClaimed {
		result, err = finishResumeReplay(ctx, tx, previousRunID, &committed)
		return result, false, err
	}
	didClaim, err = claimResumeCursor(ctx, tx, previousRunID, expectedCursor, ts)
	if err != nil {
		return ConvergenceSegmentRow{}, false, err
	}
	if !didClaim {
		result, err = finishResumeReplay(ctx, tx, previousRunID, &committed)
		return result, false, err
	}

	result = ConvergenceSegmentRow{
		RunID:         newRunID,
		ConvergenceID: prev.ConvergenceID,
		Ordinal:       prev.Ordinal + 1,
		PreviousRunID: previousRunID,
		SourceRunID:   prev.SourceRunID,
		State:         "prepared",
	}
	if err := insertResumeSegment(ctx, tx, result, ts); err != nil {
		return ConvergenceSegmentRow{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ConvergenceSegmentRow{}, false, fmt.Errorf("rundb: commit claim resume: %w", err)
	}
	committed = true
	return result, true, nil
}

func loadResumeTarget(
	ctx context.Context,
	tx *sql.Tx,
	expectedConvergenceID, previousRunID, expectedCursor string,
) (ConvergenceSegmentRow, error) {
	prev, err := readConvergenceSegmentRow(ctx, tx, previousRunID)
	if err != nil {
		return ConvergenceSegmentRow{}, err
	}
	if prev.ConvergenceID != expectedConvergenceID {
		return ConvergenceSegmentRow{}, fmt.Errorf(
			"%w: run %q belongs to convergence %q, not %q",
			ErrConvergenceReplay,
			previousRunID,
			prev.ConvergenceID,
			expectedConvergenceID,
		)
	}
	if err := validateResumeTarget(prev, expectedCursor); err != nil {
		return ConvergenceSegmentRow{}, err
	}
	return prev, nil
}

func validateResumeTarget(prev ConvergenceSegmentRow, expectedCursor string) error {
	if prev.TerminalKind != convergenceTerminalParked {
		return fmt.Errorf("%w: resume target %q is not parked", ErrConvergenceReplay, prev.RunID)
	}
	if prev.ResumeCursor != expectedCursor {
		return fmt.Errorf("%w: resume cursor for %q is stale", ErrConvergenceReplay, prev.RunID)
	}
	return nil
}

func claimResumeCursor(
	ctx context.Context,
	tx *sql.Tx,
	previousRunID, expectedCursor, ts string,
) (bool, error) {
	result, err := tx.ExecContext(ctx,
		`UPDATE convergence_segments SET resume_claimed=1, updated_at=?
		 WHERE run_id=? AND resume_cursor=? AND resume_claimed=0`,
		ts, previousRunID, expectedCursor,
	)
	if err != nil {
		return false, fmt.Errorf("rundb: claim resume cursor for %q: %w", previousRunID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rundb: resume claim rows affected: %w", err)
	}
	return affected == 1, nil
}

func insertResumeSegment(ctx context.Context, tx *sql.Tx, segment ConvergenceSegmentRow, ts string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO convergence_segments
			(run_id, convergence_id, ordinal, previous_run_id, source_run_id, state, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'prepared', ?)`,
		segment.RunID, segment.ConvergenceID, segment.Ordinal, segment.PreviousRunID, segment.SourceRunID, ts,
	); err != nil {
		return fmt.Errorf("rundb: insert resumed segment %q: %w", segment.RunID, err)
	}
	return nil
}

func finishResumeReplay(
	ctx context.Context,
	tx *sql.Tx,
	previousRunID string,
	committed *bool,
) (ConvergenceSegmentRow, error) {
	segment, err := replayClaimedSegment(ctx, tx, previousRunID)
	if err != nil {
		return ConvergenceSegmentRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return ConvergenceSegmentRow{}, fmt.Errorf("rundb: commit resume replay: %w", err)
	}
	*committed = true
	return segment, nil
}

func replayClaimedSegment(
	ctx context.Context,
	tx *sql.Tx,
	previousRunID string,
) (ConvergenceSegmentRow, error) {
	var segment ConvergenceSegmentRow
	err := tx.QueryRowContext(ctx,
		`SELECT run_id, convergence_id, ordinal, previous_run_id, source_run_id, state
		 FROM convergence_segments WHERE previous_run_id = ? ORDER BY ordinal DESC LIMIT 1`,
		strings.TrimSpace(previousRunID),
	).Scan(&segment.RunID, &segment.ConvergenceID, &segment.Ordinal,
		&segment.PreviousRunID, &segment.SourceRunID, &segment.State)
	if errors.Is(err, sql.ErrNoRows) {
		return ConvergenceSegmentRow{}, fmt.Errorf(
			"%w: claimed resume for %q has no linked segment", ErrConvergenceReplay, previousRunID)
	}
	if err != nil {
		return ConvergenceSegmentRow{}, fmt.Errorf("rundb: replay claimed segment for %q: %w", previousRunID, err)
	}
	return segment, nil
}
