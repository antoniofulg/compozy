package daemon

import (
	"errors"
	"testing"

	"github.com/compozy/compozy/internal/api/contract"
	"github.com/compozy/compozy/internal/core/convergence"
)

func parkedSnapshot(cursor string) convergence.Snapshot {
	return convergence.Snapshot{
		ConvergenceID: "cvg-1",
		Segment: convergence.Segment{
			RunID:        "run-1",
			Ordinal:      1,
			ResumeCursor: cursor,
			Terminal: &convergence.TerminalOutcome{
				Kind:   convergence.TerminalParked,
				Reason: convergence.ParkedNoProgress,
			},
		},
	}
}

// TestAuthorizeResumeTarget implements the coordinator half of UT-030: a parked
// segment with a matching cursor may resume; clean, canceled, failed, and stale
// cursors are rejected before any claim.
func TestAuthorizeResumeTarget(t *testing.T) {
	t.Parallel()
	t.Run("Should allow a parked segment with a matching cursor", func(t *testing.T) {
		t.Parallel()
		if err := authorizeResumeTarget(parkedSnapshot("cur-1"), "cur-1"); err != nil {
			t.Fatalf("expected resume to be allowed, got %v", err)
		}
	})
	t.Run("Should reject a stale cursor as resume_cursor_stale", func(t *testing.T) {
		t.Parallel()
		err := authorizeResumeTarget(parkedSnapshot("cur-1"), "cur-2")
		if !errors.Is(err, convergence.ErrResumeCursorStale) {
			t.Fatalf("expected ErrResumeCursorStale, got %v", err)
		}
	})
	nonParked := []struct {
		name string
		kind convergence.TerminalKind
	}{
		{"clean", convergence.TerminalClean},
		{"canceled", convergence.TerminalCancelled},
		{"failed", convergence.TerminalFailed},
	}
	for _, tc := range nonParked {
		t.Run("Should reject a "+tc.name+" segment as not_parked", func(t *testing.T) {
			t.Parallel()
			snap := parkedSnapshot("cur-1")
			snap.Segment.Terminal = &convergence.TerminalOutcome{Kind: tc.kind}
			if err := authorizeResumeTarget(snap, "cur-1"); !errors.Is(err, convergence.ErrNotParked) {
				t.Fatalf("expected ErrNotParked, got %v", err)
			}
		})
	}
	t.Run("Should reject an active segment as not_parked", func(t *testing.T) {
		t.Parallel()
		snap := convergence.Snapshot{Segment: convergence.Segment{RunID: "run-1", ResumeCursor: "cur-1"}}
		if err := authorizeResumeTarget(snap, "cur-1"); !errors.Is(err, convergence.ErrNotParked) {
			t.Fatalf("expected ErrNotParked for a live segment, got %v", err)
		}
	})
	t.Run("Should carry immutable prior identity into the resume request", func(t *testing.T) {
		t.Parallel()
		req := buildResumeRequest(parkedSnapshot("cur-1"), "cur-1", "run-2")
		if req.PreviousRunID != "run-1" || req.NewRunID != "run-2" || req.ExpectedCursor != "cur-1" {
			t.Fatalf("resume request drift: %+v", req)
		}
		if req.ConvergenceID != "cvg-1" {
			t.Fatalf("convergence identity drift: %q", req.ConvergenceID)
		}
	})
}

// TestDecideApproval implements UT-031: protected approvals require authority,
// reason, and fingerprint/snapshot binding; identical decisions replay
// idempotently; conflicting or stale decisions are rejected; deciding never
// resumes the run.
func TestDecideApproval(t *testing.T) {
	t.Parallel()
	proposal := convergence.ApprovalProposal{
		ProposalID:  "prop-1",
		Fingerprint: "fp-1",
		Action:      "weaken_verification",
		Snapshot:    "snap-1",
	}
	user := convergencePrincipal{Role: convergencePrincipalUser, RunAuthority: true}
	base := contract.ApprovalDecisionRequest{
		ProposalID:          "prop-1",
		Decision:            contract.ConvergenceDecisionApprove,
		Reason:              "reviewed and accepted the scoped exception",
		ExpectedFingerprint: "fp-1",
		ExpectedSnapshot:    "snap-1",
	}
	t.Run("Should record a valid decision without resuming", func(t *testing.T) {
		t.Parallel()
		got, err := decideApproval(proposal, base, user)
		if err != nil {
			t.Fatalf("expected decision, got %v", err)
		}
		if got.Replayed {
			t.Fatalf("first decision must not be a replay")
		}
		if got.Proposal.Decision != contract.ConvergenceDecisionApprove {
			t.Fatalf("decision not recorded: %+v", got.Proposal)
		}
	})
	t.Run("Should deny a caller without run authority", func(t *testing.T) {
		t.Parallel()
		_, err := decideApproval(proposal, base, convergencePrincipal{Role: convergencePrincipalReviewer})
		if !errors.Is(err, errConvergenceApprovalUnauthorized) {
			t.Fatalf("expected unauthorized, got %v", err)
		}
	})
	t.Run("Should reject a missing reason", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Reason = "   "
		if _, err := decideApproval(proposal, req, user); !errors.Is(err, errConvergenceApprovalInvalid) {
			t.Fatalf("expected invalid, got %v", err)
		}
	})
	t.Run("Should reject an unknown decision", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Decision = "maybe"
		if _, err := decideApproval(proposal, req, user); !errors.Is(err, errConvergenceApprovalInvalid) {
			t.Fatalf("expected invalid, got %v", err)
		}
	})
	t.Run("Should reject a changed fingerprint as stale", func(t *testing.T) {
		t.Parallel()
		req := base
		req.ExpectedFingerprint = "fp-2"
		if _, err := decideApproval(proposal, req, user); !errors.Is(err, convergence.ErrApprovalStale) {
			t.Fatalf("expected stale, got %v", err)
		}
	})
	t.Run("Should reject a changed snapshot as stale", func(t *testing.T) {
		t.Parallel()
		req := base
		req.ExpectedSnapshot = "snap-2"
		if _, err := decideApproval(proposal, req, user); !errors.Is(err, convergence.ErrApprovalStale) {
			t.Fatalf("expected stale, got %v", err)
		}
	})
	t.Run("Should replay an identical decision idempotently", func(t *testing.T) {
		t.Parallel()
		decided := proposal
		decided.Decision = contract.ConvergenceDecisionApprove
		decided.Reason = "reviewed and accepted the scoped exception"
		got, err := decideApproval(decided, base, user)
		if err != nil {
			t.Fatalf("expected replay, got %v", err)
		}
		if !got.Replayed {
			t.Fatalf("expected an idempotent replay")
		}
	})
	t.Run("Should reject a conflicting decision on a resolved proposal", func(t *testing.T) {
		t.Parallel()
		decided := proposal
		decided.Decision = contract.ConvergenceDecisionReject
		decided.Reason = "previously rejected"
		if _, err := decideApproval(decided, base, user); !errors.Is(err, convergence.ErrApprovalStale) {
			t.Fatalf("expected stale on conflicting decision, got %v", err)
		}
	})
}

// TestConvergenceCancellationAndDivergence implements UT-032: a cancellation
// forbids later phase admission, the cancel-versus-completion race is resolved by
// durable sequence, and an unexplained boundary snapshot mismatch parks as
// workspace_changed with expected/observed evidence.
func TestConvergenceCancellationAndDivergence(t *testing.T) {
	t.Parallel()
	t.Run("Should forbid a new phase once cancellation is accepted", func(t *testing.T) {
		t.Parallel()
		if err := admitPhaseUnderCancellation(true); !errors.Is(err, errConvergenceCanceled) {
			t.Fatalf("expected canceled, got %v", err)
		}
		if err := admitPhaseUnderCancellation(false); err != nil {
			t.Fatalf("expected admission when not canceled, got %v", err)
		}
	})
	t.Run("Should admit only completions committed before cancellation", func(t *testing.T) {
		t.Parallel()
		if !phaseCompletionAdmitted(4, 5) {
			t.Fatalf("a completion before the cancellation must be admitted")
		}
		if phaseCompletionAdmitted(5, 5) || phaseCompletionAdmitted(6, 5) {
			t.Fatalf("a completion at or after the cancellation must not be admitted")
		}
	})
	t.Run("Should accept an equal boundary snapshot", func(t *testing.T) {
		t.Parallel()
		outcome, evidence := classifyBoundarySnapshot("snap-1", "snap-1", false)
		if outcome != nil || evidence.Diverged {
			t.Fatalf("equal snapshots must not diverge: %+v", evidence)
		}
	})
	t.Run("Should accept a phase-owned change", func(t *testing.T) {
		t.Parallel()
		outcome, evidence := classifyBoundarySnapshot("snap-1", "snap-2", true)
		if outcome != nil || evidence.Diverged {
			t.Fatalf("owned change must be accepted: %+v", evidence)
		}
	})
	t.Run("Should park an unexplained divergence with evidence", func(t *testing.T) {
		t.Parallel()
		outcome, evidence := classifyBoundarySnapshot("snap-1", "snap-2", false)
		if outcome == nil {
			t.Fatalf("unexplained divergence must park")
		}
		if outcome.Kind != convergence.TerminalParked || outcome.Reason != convergence.ParkedWorkspaceChanged {
			t.Fatalf("expected parked workspace_changed, got %+v", outcome)
		}
		if !evidence.Diverged || evidence.Expected != "snap-1" || evidence.Observed != "snap-2" {
			t.Fatalf("expected/observed evidence drift: %+v", evidence)
		}
	})
}
