package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestConvergenceRunIndexRoundTripsRelationsAndRejectsDuplicateActivation(t *testing.T) {
	// IT-003: the additive global index survives reopen, preserves
	// reconstructible run relations and receipt metadata, and enforces one run per
	// activation fingerprint while allowing idempotent rebuilds.
	t.Parallel()

	ctx := context.Background()
	db := openTestGlobalDB(t)
	path := db.Path()
	seedConvergenceGlobalRun(t, db, "run-cvg-1")
	seedConvergenceGlobalRun(t, db, "run-cvg-2")

	first := ConvergenceRunIndexRow{
		RunID:                 "run-cvg-1",
		ConvergenceID:         "cvg-1",
		SourceRunID:           "task-run-1",
		ActivationFingerprint: "activation-fp",
		TerminalOutcome:       "parked",
		TerminalReason:        "approval_required",
		ReceiptPath:           "convergence-receipt.json",
		ReceiptSourceSeq:      21,
	}
	if err := db.UpsertConvergenceRunIndex(ctx, first); err != nil {
		t.Fatalf("UpsertConvergenceRunIndex(first) = %v", err)
	}
	rebuild := first
	rebuild.ActivationFingerprint = ""
	rebuild.ReceiptSourceSeq = 22
	if err := db.UpsertConvergenceRunIndex(ctx, rebuild); err != nil {
		t.Fatalf("UpsertConvergenceRunIndex(rebuild) = %v", err)
	}
	second := ConvergenceRunIndexRow{
		RunID:             "run-cvg-2",
		ConvergenceID:     "cvg-1",
		PreviousRunID:     "run-cvg-1",
		SourceRunID:       "task-run-1",
		ResumeFingerprint: "resume-fp",
		TerminalOutcome:   "clean",
		ReceiptPath:       "convergence-receipt.json",
		ReceiptSourceSeq:  44,
	}
	if err := db.UpsertConvergenceRunIndex(ctx, second); err != nil {
		t.Fatalf("UpsertConvergenceRunIndex(second) = %v", err)
	}
	conflict := second
	conflict.ActivationFingerprint = "activation-fp"
	if err := db.UpsertConvergenceRunIndex(ctx, conflict); !errors.Is(err, ErrConvergenceActivationConflict) {
		t.Fatalf("duplicate activation error = %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(reopen) = %v", err)
	}
	defer func() { _ = reopened.Close() }()

	rows, err := reopened.ConvergenceRunIndexByConvergenceID(ctx, "cvg-1")
	if err != nil {
		t.Fatalf("ConvergenceRunIndexByConvergenceID() = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].RunID != "run-cvg-1" || rows[0].ActivationFingerprint != "activation-fp" ||
		rows[0].ReceiptSourceSeq != 22 {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].RunID != "run-cvg-2" || rows[1].PreviousRunID != "run-cvg-1" ||
		rows[1].ResumeFingerprint != "resume-fp" {
		t.Fatalf("second row = %+v", rows[1])
	}
}

func seedConvergenceGlobalRun(t *testing.T, db *GlobalDB, runID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO workspaces (id, root_dir, name, created_at, updated_at)
		 VALUES ('ws-cvg', '/tmp/ws-cvg', 'convergence', '2026-07-24T12:00:00Z', '2026-07-24T12:00:00Z')`,
	); err != nil {
		t.Fatalf("seed workspace = %v", err)
	}
	if _, err := db.db.ExecContext(ctx,
		`INSERT INTO runs
			(run_id, workspace_id, mode, status, presentation_mode, started_at)
		 VALUES (?, 'ws-cvg', 'convergence', 'running', 'stream', '2026-07-24T12:00:00Z')`,
		runID,
	); err != nil {
		t.Fatalf("seed run %q = %v", runID, err)
	}
}
