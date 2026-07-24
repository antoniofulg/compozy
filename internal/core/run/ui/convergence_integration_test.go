package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/compozy/compozy/internal/api/contract"
	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/store/convergencestore"
	"github.com/compozy/compozy/internal/store/rundb"
	"github.com/compozy/compozy/pkg/compozy/events"
	"github.com/compozy/compozy/pkg/compozy/events/kinds"
)

// convergenceITClock is the fixed fixture clock; the accompanying fixed seed for
// IT-038 is 20260724 (the fixture date), used for deterministic history counts.
var convergenceITClock = time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

const convergenceITSeed = 20260724

// TestIT038ConvergenceLargeHistoriesStayBounded covers IT-038: large histories,
// one-MiB command output, and many children remain bounded, complete, ordered, and
// memory-stable across the real run.db store, the API projection, and the TUI view.
func TestIT038ConvergenceLargeHistoriesStayBounded(t *testing.T) {
	t.Parallel()

	t.Run("Should keep one-MiB output by reference across store, API, and UI", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db, err := rundb.Open(ctx, filepath.Join(t.TempDir(), "run", "run.db"))
		if err != nil {
			t.Fatalf("rundb.Open() = %v", err)
		}
		defer func() { _ = db.Close() }()
		store := convergencestore.New(db)

		const runID = "run-it038"
		const convergenceID = "cvg-it038"
		const phaseID = "phase-it038"
		seedConvergenceRun(t, store, runID, convergenceID)

		// A phase must exist before a verification can bind to it.
		applyConvergenceEvent(t, store, runID, 2, events.EventKindConvergencePhaseStarted,
			kinds.ConvergencePhaseStartedPayload{
				ConvergenceIdentifiers: convergenceIdentifiers(convergenceID, runID, phaseID),
				Phase:                  string(convergence.PhaseCorrection),
				Round:                  1,
				Attempt:                1,
				Snapshot:               "snap-1",
				Outcome:                "started",
			})
		// Two actionable findings and one resolved finding.
		seq := uint64(3)
		for _, finding := range []struct {
			fp, state, outcome string
		}{
			{"fp-open-1", "actionable", "created"},
			{"fp-open-2", "actionable", "created"},
			{"fp-done-1", "resolved", "resolved"},
		} {
			applyConvergenceEvent(t, store, runID, seq, events.EventKindConvergenceFindingChanged,
				kinds.ConvergenceFindingChangedPayload{
					ConvergenceIdentifiers: convergenceIdentifiers(convergenceID, runID, finding.fp),
					State:                  finding.state,
					Severity:               "high",
					Snapshot:               "snap-1",
					EvidenceRef:            "evidence/" + finding.fp + ".json",
					Outcome:                finding.outcome,
				})
			seq++
		}

		// One-MiB verification output persisted on disk and referenced by path only.
		oneMiBPath := filepath.Join(t.TempDir(), "verify.log")
		if writeErr := os.WriteFile(oneMiBPath, make([]byte, 1<<20), 0o600); writeErr != nil {
			t.Fatalf("write 1MiB evidence = %v", writeErr)
		}
		exit := 0
		applyConvergenceEvent(t, store, runID, seq, events.EventKindConvergenceVerificationComplete,
			kinds.ConvergenceVerificationCompletedPayload{
				ConvergenceIdentifiers: convergenceIdentifiers(convergenceID, runID, "verify-1"),
				CommandFingerprint:     "make-verify",
				Snapshot:               "snap-1",
				ExitCode:               &exit,
				EvidencePath:           oneMiBPath,
				Outcome:                "passed",
			})
		seq++
		applyConvergenceEvent(t, store, runID, seq, events.EventKindConvergenceSegmentParked,
			kinds.ConvergenceSegmentParkedPayload{
				ConvergenceIdentifiers: parkedIdentifiers(convergenceID, runID),
				Reason:                 string(convergence.ParkedApprovalRequired),
				Snapshot:               "snap-1",
				UnresolvedCount:        2,
				ReceiptPath:            "convergence-receipt.json",
				ResumeAvailable:        true,
				Outcome:                "parked",
			})

		domainSnapshot, err := store.Snapshot(ctx, runID)
		if err != nil {
			t.Fatalf("store.Snapshot() = %v", err)
		}

		children := make([]contract.ConvergenceRelation, 0, 200)
		for i := 0; i < 200; i++ {
			children = append(children, contract.ConvergenceRelation{
				Kind:  contract.ConvergenceRelationChild,
				RunID: "child-" + strconv.Itoa(i),
			})
		}
		opts := contract.DefaultConvergenceProjectionOptions()
		opts.Children = children
		response := contract.NewConvergenceSnapshotResponse(domainSnapshot, opts)
		projected := response.Convergence

		// Complete current state survives the store->API round trip.
		if projected.UnresolvedCount != 2 {
			t.Fatalf("unresolved = %d, want 2", projected.UnresolvedCount)
		}
		if projected.Terminal == nil || projected.Terminal.Reason != string(convergence.ParkedApprovalRequired) {
			t.Fatalf("terminal = %+v, want parked approval_required", projected.Terminal)
		}
		if !projected.Handoff.ResumeAvailable || projected.Handoff.ResumeCursor == "" {
			t.Fatalf("handoff = %+v, want resume with cursor", projected.Handoff)
		}
		// Raw evidence is referenced by path/checksum, never inlined.
		if len(projected.Verification) != 1 || projected.Verification[0].EvidencePath != oneMiBPath {
			t.Fatalf("verification = %+v, want single result referencing evidence path", projected.Verification)
		}
		// The bounded summary is orders of magnitude smaller than the raw output.
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("marshal response = %v", err)
		}
		if len(encoded) >= (1 << 18) {
			t.Fatalf("projected snapshot = %d bytes, want << 1MiB (raw output must stay by reference)", len(encoded))
		}
		// Many children remain queryable through the bounded projection and UI view,
		// alongside the segment source-lineage relation.
		mdl := newRemoteConvergenceModel(RemoteConvergenceAttachOptions{Convergence: projected, HasConvergence: true})
		childCount, hasSource := 0, false
		for _, relation := range mdl.view.relations {
			switch relation.Kind {
			case contract.ConvergenceRelationChild:
				childCount++
			case contract.ConvergenceRelationSource:
				hasSource = true
			}
		}
		if childCount != 200 {
			t.Fatalf("ui child relations = %d, want 200 retained", childCount)
		}
		if !hasSource {
			t.Fatal("source-lineage relation must be retained alongside children")
		}
		openInView := 0
		for _, finding := range mdl.view.findings {
			if finding.open {
				openInView++
			}
		}
		if openInView != 2 {
			t.Fatalf("open findings in ui view = %d, want 2 actionable retained", openInView)
		}
	})

	t.Run("Should stay memory-stable and ordered as history size grows", func(t *testing.T) {
		t.Parallel()
		opts := contract.ConvergenceProjectionOptions{
			MaxFindings:     50,
			MaxRounds:       20,
			MaxBatches:      30,
			MaxVerification: 15,
		}

		small := projectITHistory(t, 10, 500, 80, opts)
		large := projectITHistory(t, 10, 5000, 400, opts)

		if len(small.Findings) != len(large.Findings) {
			t.Fatalf("finding output size changed with input: %d vs %d (not memory-stable)",
				len(small.Findings), len(large.Findings))
		}
		if len(large.Rounds) != 20 {
			t.Fatalf("rounds = %d, want bounded to 20", len(large.Rounds))
		}
		if large.Rounds[len(large.Rounds)-1].Number != 400 {
			t.Fatalf("last round = %d, want ordered tail 400", large.Rounds[len(large.Rounds)-1].Number)
		}
		if large.Page.Findings.Total != 5010 || !large.Page.Findings.Truncated {
			t.Fatalf("findings page = %+v, want total 5010 truncated", large.Page.Findings)
		}
		if large.UnresolvedCount != 10 {
			t.Fatalf("unresolved = %d, want 10 actionable retained", large.UnresolvedCount)
		}
	})
}

func projectITHistory(
	t *testing.T,
	openCount, closedCount, rounds int,
	opts contract.ConvergenceProjectionOptions,
) contract.ConvergenceSnapshot {
	t.Helper()
	// convergenceITSeed keeps the generated history deterministic across runs.
	severities := []convergence.Severity{
		convergence.SeverityLow,
		convergence.SeverityMedium,
		convergence.SeverityHigh,
		convergence.SeverityCritical,
	}
	snap := convergence.Snapshot{
		ConvergenceID: "cvg-scale",
		Segment:       convergence.Segment{RunID: "run-scale", State: convergence.SegmentTerminal},
		Terminal: &convergence.TerminalOutcome{
			Kind:   convergence.TerminalParked,
			Reason: convergence.ParkedMaxRounds,
		},
		LastSeq: uint64(convergenceITSeed),
	}
	seq := uint64(0)
	for i := 0; i < openCount; i++ {
		seq++
		snap.Findings = append(snap.Findings, convergence.Finding{
			Fingerprint: convergence.FindingFingerprint("open-" + strconv.Itoa(i)),
			State:       convergence.FindingActionable,
			Severity:    severities[(convergenceITSeed+i)%len(severities)],
			FirstSeq:    seq,
		})
	}
	for i := 0; i < closedCount; i++ {
		seq++
		snap.Findings = append(snap.Findings, convergence.Finding{
			Fingerprint: convergence.FindingFingerprint("closed-" + strconv.Itoa(i)),
			State:       convergence.FindingResolved,
			Severity:    severities[(convergenceITSeed+i)%len(severities)],
			FirstSeq:    seq,
		})
	}
	for i := 1; i <= rounds; i++ {
		snap.Rounds = append(snap.Rounds, convergence.RoundState{RoundID: "round-" + strconv.Itoa(i), Number: i})
	}
	return contract.ProjectConvergenceSnapshot(snap, opts)
}

func seedConvergenceRun(t *testing.T, store *convergencestore.Store, runID, convergenceID string) {
	t.Helper()
	segment := convergence.Segment{RunID: runID, SourceRunID: "task-src", State: convergence.SegmentPrepared}
	target := convergence.TargetBinding{
		WorkspaceID: "payments",
		TaskGroupID: "TG-004",
		Branch:      "converge/tg-004",
		Worktree:    ".worktrees/tg-004",
		Snapshot:    "snap-frozen",
	}
	config := convergence.FrozenConfiguration{
		ProfileName:    "quality",
		ModelSetupName: "balanced",
		AutoCommit:     true,
		Limits: convergence.Limits{
			MaxReviewRounds:    6,
			MaxFindingAttempts: 3,
			NoProgressRounds:   2,
		},
	}
	if err := store.Seed(context.Background(), convergenceID, segment, target, config); err != nil {
		t.Fatalf("store.Seed() = %v", err)
	}
}

func applyConvergenceEvent(
	t *testing.T,
	store *convergencestore.Store,
	runID string,
	seq uint64,
	kind events.EventKind,
	payload any,
) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload = %v", kind, err)
	}
	if err := store.Apply(context.Background(), events.Event{
		SchemaVersion: events.SchemaVersion,
		RunID:         runID,
		Seq:           seq,
		Timestamp:     convergenceITClock,
		Kind:          kind,
		Payload:       raw,
	}); err != nil {
		t.Fatalf("store.Apply(%s) = %v", kind, err)
	}
}

func convergenceIdentifiers(convergenceID, runID, resourceID string) kinds.ConvergenceIdentifiers {
	return kinds.ConvergenceIdentifiers{
		RequestID:     "req-it038",
		ActorID:       "actor-it038",
		ResourceID:    resourceID,
		CorrelationID: convergenceIDForResource(convergenceID, runID, resourceID),
	}
}

// convergenceIDForResource picks the correlation id each projector expects: a
// verification correlates to its phase, everything else to the convergence id.
func convergenceIDForResource(convergenceID, _, resourceID string) string {
	if resourceID == "verify-1" {
		return "phase-it038"
	}
	return convergenceID
}

func parkedIdentifiers(convergenceID, runID string) kinds.ConvergenceIdentifiers {
	return kinds.ConvergenceIdentifiers{
		RequestID:     "req-it038",
		ActorID:       "actor-it038",
		ResourceID:    runID,
		CorrelationID: convergenceID,
	}
}
