package convergencestore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/store/rundb"
	"github.com/compozy/compozy/pkg/compozy/events"
	"github.com/compozy/compozy/pkg/compozy/events/kinds"
)

func TestStoreSnapshotReconstructsReceiptInputFromRunDB(t *testing.T) {
	// IT-002 adapter boundary: the domain snapshot and receipt are reconstructed
	// from persisted run.db rows, including request identity and frozen routes.
	t.Parallel()

	ctx := context.Background()
	db, err := rundb.Open(ctx, filepath.Join(t.TempDir(), "run-adapter", "run.db"))
	if err != nil {
		t.Fatalf("rundb.Open() = %v", err)
	}
	defer func() { _ = db.Close() }()
	store := New(db)
	segment := convergence.Segment{
		RunID:       "run-adapter",
		SourceRunID: "task-run-1",
		State:       convergence.SegmentPrepared,
	}
	target := convergence.TargetBinding{
		WorkspaceID:    "ws-1",
		ExecutionScope: "task-group",
		TaskGroupID:    "TG-002",
		Branch:         "feature/convergence",
		Worktree:       "worktrees/TG-002",
		Snapshot:       "sha-1",
	}
	config := convergence.FrozenConfiguration{
		ProfileName:    "default",
		ModelSetupName: "balanced",
		BaseRoute:      convergence.Route{IDE: "claude", Model: "task-model", ReasoningEffort: "high"},
		Review: convergence.ResolvedRoute{
			Role:    convergence.RoleReview,
			Primary: convergence.Route{IDE: "claude", Model: "review-model", ReasoningEffort: "high"},
			Sources: convergence.RouteSources{
				IDE:             convergence.SourceSetupBase,
				Model:           convergence.SourceSetupBase,
				ReasoningEffort: convergence.SourceSetupBase,
			},
		},
	}
	if err := store.Seed(ctx, "cvg-adapter", segment, target, config); err != nil {
		t.Fatalf("Seed() = %v", err)
	}
	payload, err := json.Marshal(kinds.ConvergencePreflightCompletedPayload{
		ConvergenceIdentifiers: kinds.ConvergenceIdentifiers{
			RequestID:     "request-adapter",
			ActorID:       "actor-1",
			ResourceID:    "cvg-adapter",
			CorrelationID: "run-adapter",
		},
		TargetSnapshot:    "sha-1",
		ConfigFingerprint: "config-fp",
		RouteSummary:      "review=claude",
		Warnings:          []string{},
		Outcome:           "accepted",
	})
	if err != nil {
		t.Fatalf("json.Marshal() = %v", err)
	}
	if err := store.Apply(ctx, events.Event{
		SchemaVersion: events.SchemaVersion,
		RunID:         "run-adapter",
		Seq:           1,
		Timestamp:     time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
		Kind:          events.EventKindConvergencePreflightCompleted,
		Payload:       payload,
	}); err != nil {
		t.Fatalf("Apply(preflight) = %v", err)
	}

	snapshot, err := store.Snapshot(ctx, "run-adapter")
	if err != nil {
		t.Fatalf("Snapshot() = %v", err)
	}
	if snapshot.RequestID != "request-adapter" || snapshot.Target != target ||
		snapshot.Config.Review.Primary.Model != "review-model" || snapshot.LastSeq != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	receipt := convergence.BuildReceipt(snapshot, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if len(receipt.ConfiguredRoutes) != 1 ||
		receipt.ConfiguredRoutes[0].Primary.Model != "review-model" ||
		receipt.SourceSeq != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
}
