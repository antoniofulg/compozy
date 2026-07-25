package convergence

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildReviewHandoff(t *testing.T) {
	// 4.6: a review handoff carries only canonical context with a unique child
	// identity and rejects a missing run, phase, or snapshot binding.
	t.Parallel()

	in := ReviewHandoffInput{
		RunID:           "run-1",
		RoundNumber:     2,
		PhaseID:         "phase-review",
		Snapshot:        "snap-1",
		Route:           Route{IDE: "claude", Model: "opus", ReasoningEffort: "high"},
		Intent:          "review the current snapshot",
		OpenFindings:    []FindingFingerprint{"fp-1"},
		VerificationRef: "evidence/ver-1",
		Remaining:       RemainingLimits{ReviewRounds: 4, FindingAttempts: 3},
	}
	handoff, err := BuildReviewHandoff(in)
	if err != nil {
		t.Fatalf("BuildReviewHandoff() = %v", err)
	}
	if handoff.Role != SessionReview {
		t.Fatalf("role = %q, want review", handoff.Role)
	}
	if handoff.SessionID == "" || !strings.Contains(handoff.SessionID, "phase-review") {
		t.Fatalf("session id = %q, want a unique child identity", handoff.SessionID)
	}
	if handoff.Snapshot != in.Snapshot || handoff.Route != in.Route {
		t.Fatalf("handoff dropped snapshot or route: %+v", handoff)
	}

	for _, missing := range []ReviewHandoffInput{
		{PhaseID: "p", Snapshot: "s"},
		{RunID: "r", Snapshot: "s"},
		{RunID: "r", PhaseID: "p"},
	} {
		if _, err := BuildReviewHandoff(missing); !errors.Is(err, ErrCorrectionInvalid) {
			t.Fatalf("BuildReviewHandoff(%+v) = %v, want ErrCorrectionInvalid", missing, err)
		}
	}
}

func TestBuildCorrectionHandoffScopesToBatch(t *testing.T) {
	// 4.6: a correction handoff is scoped to one batch's findings, derives a durable
	// batch identity, and carries the batch's own findings only.
	t.Parallel()

	batch := CorrectionBatch{
		Order:               1,
		File:                "pkg/a.go",
		FindingFingerprints: []FindingFingerprint{"fp-a1", "fp-a2"},
		RouteSeverity:       SeverityHigh,
	}
	handoff, err := BuildCorrectionHandoff(CorrectionHandoffInput{
		RunID:       "run-9",
		RoundNumber: 3,
		PhaseID:     "phase-correction",
		Batch:       batch,
		Snapshot:    "snap-9",
		Route:       Route{IDE: "codex", Model: "gpt", ReasoningEffort: "medium"},
	})
	if err != nil {
		t.Fatalf("BuildCorrectionHandoff() = %v", err)
	}
	if handoff.Role != SessionCorrection {
		t.Fatalf("role = %q, want correction", handoff.Role)
	}
	if handoff.BatchID == "" || !strings.Contains(handoff.SessionID, handoff.BatchID) {
		t.Fatalf("session id %q must embed batch id %q", handoff.SessionID, handoff.BatchID)
	}
	if len(handoff.OpenFindings) != 2 {
		t.Fatalf("handoff findings = %v, want the batch's two findings", handoff.OpenFindings)
	}

	if _, err := BuildCorrectionHandoff(CorrectionHandoffInput{
		RunID: "r", RoundNumber: 1, PhaseID: "p", Snapshot: "s",
		Batch: CorrectionBatch{File: ""},
	}); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("empty batch file must be rejected, got %v", err)
	}
}

func TestHandoffsCarryNoTranscript(t *testing.T) {
	// ADR-004: a fresh session receives only canonical context; the Handoff type
	// has no field that could carry a prior conversational transcript.
	t.Parallel()
	handoff, err := BuildReviewHandoff(ReviewHandoffInput{
		RunID: "r", PhaseID: "p", Snapshot: "s",
	})
	if err != nil {
		t.Fatalf("BuildReviewHandoff() = %v", err)
	}
	// AcceptedDecisions and ArtifactRefs are pointers into fresh copies, never
	// shared with the caller's slices, so a later mutation cannot leak in.
	if handoff.AcceptedDecisions != nil || handoff.ArtifactRefs != nil {
		t.Fatalf("empty inputs must not fabricate context: %+v", handoff)
	}
}
