package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/compozy/compozy/internal/store"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrConvergenceActivationConflict reports that an activation fingerprint is
// already indexed for a different run. The global index enforces one active or
// replayable run per activation key so a duplicate activation cannot create a
// second convergence run.
var ErrConvergenceActivationConflict = errors.New("globaldb: convergence activation fingerprint conflict")

// ConvergenceRunIndexRow is the additive global index of one convergence run. The
// convergence journal and run.db stay canonical; this row indexes only the
// cross-run relations, activation and resume request fingerprints, terminal
// outcome, and receipt projection index the daemon reconstructs from canonical
// state. It is rebuildable: every field is derivable from canonical state.
type ConvergenceRunIndexRow struct {
	RunID                 string
	ConvergenceID         string
	PreviousRunID         string
	SourceRunID           string
	ActivationFingerprint string
	ResumeFingerprint     string
	TerminalOutcome       string
	TerminalReason        string
	ReceiptPath           string
	ReceiptSourceSeq      uint64
}

// UpsertConvergenceRunIndex indexes one convergence run keyed by run ID. It is
// idempotent: re-indexing the same run overwrites the reconstructible columns.
// A blank activation or resume fingerprint never erases a previously stored one,
// so a canonical-state rebuild (which does not carry the request fingerprints)
// preserves the fingerprints recorded at activation and resume. A different run
// that reuses an activation fingerprint is rejected with
// ErrConvergenceActivationConflict, enforcing one run per activation key.
func (g *GlobalDB) UpsertConvergenceRunIndex(ctx context.Context, row ConvergenceRunIndexRow) error {
	if err := g.requireContext(ctx, "upsert convergence run index"); err != nil {
		return err
	}
	row.RunID = strings.TrimSpace(row.RunID)
	row.ConvergenceID = strings.TrimSpace(row.ConvergenceID)
	if row.RunID == "" || row.ConvergenceID == "" {
		return fmt.Errorf("globaldb: convergence run index requires run and convergence identity")
	}
	receiptSourceSeq, err := strconv.ParseInt(strconv.FormatUint(row.ReceiptSourceSeq, 10), 10, 64)
	if err != nil {
		return fmt.Errorf("globaldb: receipt source sequence is out of range: %w", err)
	}
	if _, err := g.db.ExecContext(ctx,
		`INSERT INTO convergence_run_index
			(run_id, convergence_id, previous_run_id, source_run_id, activation_fingerprint,
			 resume_fingerprint, terminal_outcome, terminal_reason, receipt_path, receipt_source_seq, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(run_id) DO UPDATE SET
			convergence_id=excluded.convergence_id,
			previous_run_id=excluded.previous_run_id,
			source_run_id=excluded.source_run_id,
			activation_fingerprint=CASE WHEN excluded.activation_fingerprint = ''
				THEN convergence_run_index.activation_fingerprint ELSE excluded.activation_fingerprint END,
			resume_fingerprint=CASE WHEN excluded.resume_fingerprint = ''
				THEN convergence_run_index.resume_fingerprint ELSE excluded.resume_fingerprint END,
			terminal_outcome=excluded.terminal_outcome,
			terminal_reason=excluded.terminal_reason,
			receipt_path=excluded.receipt_path,
			receipt_source_seq=excluded.receipt_source_seq,
			updated_at=excluded.updated_at`,
		row.RunID, row.ConvergenceID, row.PreviousRunID, row.SourceRunID, row.ActivationFingerprint,
		row.ResumeFingerprint, row.TerminalOutcome, row.TerminalReason, row.ReceiptPath,
		receiptSourceSeq, store.FormatTimestamp(g.now().UTC()),
	); err != nil {
		if isConvergenceActivationConflict(err) {
			return fmt.Errorf("%w: %q", ErrConvergenceActivationConflict, row.ActivationFingerprint)
		}
		return fmt.Errorf("globaldb: upsert convergence run index %q: %w", row.RunID, err)
	}
	return nil
}

// ConvergenceRunIndex reads one indexed convergence run by run ID.
func (g *GlobalDB) ConvergenceRunIndex(
	ctx context.Context,
	runID string,
) (ConvergenceRunIndexRow, bool, error) {
	if err := g.requireContext(ctx, "read convergence run index"); err != nil {
		return ConvergenceRunIndexRow{}, false, err
	}
	row := ConvergenceRunIndexRow{RunID: strings.TrimSpace(runID)}
	var sourceSeq int64
	err := g.db.QueryRowContext(ctx,
		`SELECT convergence_id, previous_run_id, source_run_id, activation_fingerprint,
			resume_fingerprint, terminal_outcome, terminal_reason, receipt_path, receipt_source_seq
		 FROM convergence_run_index WHERE run_id = ?`, row.RunID,
	).Scan(&row.ConvergenceID, &row.PreviousRunID, &row.SourceRunID, &row.ActivationFingerprint,
		&row.ResumeFingerprint, &row.TerminalOutcome, &row.TerminalReason, &row.ReceiptPath, &sourceSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return ConvergenceRunIndexRow{}, false, nil
	}
	if err != nil {
		return ConvergenceRunIndexRow{}, false, fmt.Errorf("globaldb: read convergence run index %q: %w", runID, err)
	}
	if sourceSeq > 0 {
		row.ReceiptSourceSeq = uint64(sourceSeq)
	}
	return row, true, nil
}

// ConvergenceRunIndexByConvergenceID reads every indexed segment of one
// convergence identity in ordinal-stable order so relation history survives a
// reopen. The rows are ordered by run_id to keep the read deterministic.
func (g *GlobalDB) ConvergenceRunIndexByConvergenceID(
	ctx context.Context,
	convergenceID string,
) ([]ConvergenceRunIndexRow, error) {
	if err := g.requireContext(ctx, "list convergence run index"); err != nil {
		return nil, err
	}
	rows, err := g.db.QueryContext(ctx,
		`SELECT run_id, convergence_id, previous_run_id, source_run_id, activation_fingerprint,
			resume_fingerprint, terminal_outcome, terminal_reason, receipt_path, receipt_source_seq
		 FROM convergence_run_index WHERE convergence_id = ? ORDER BY run_id ASC`,
		strings.TrimSpace(convergenceID),
	)
	if err != nil {
		return nil, fmt.Errorf("globaldb: query convergence run index for %q: %w", convergenceID, err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]ConvergenceRunIndexRow, 0)
	for rows.Next() {
		var (
			row       ConvergenceRunIndexRow
			sourceSeq int64
		)
		if err := rows.Scan(&row.RunID, &row.ConvergenceID, &row.PreviousRunID, &row.SourceRunID,
			&row.ActivationFingerprint, &row.ResumeFingerprint, &row.TerminalOutcome, &row.TerminalReason,
			&row.ReceiptPath, &sourceSeq); err != nil {
			return nil, fmt.Errorf("globaldb: scan convergence run index: %w", err)
		}
		if sourceSeq > 0 {
			row.ReceiptSourceSeq = uint64(sourceSeq)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("globaldb: iterate convergence run index: %w", err)
	}
	return result, nil
}

// isConvergenceActivationConflict reports whether err is the activation-fingerprint
// unique-index violation. The run_id conflict is handled by the UPSERT clause, so
// the only remaining unique constraint in this statement is the activation key.
func isConvergenceActivationConflict(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
