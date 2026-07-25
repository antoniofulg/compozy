package convergence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIT017UnchangedFixerInterruptionSafeRetry(t *testing.T) {
	// IT-017: a fixer response lost with unchanged Git state recovers to a safe
	// retry, preserves attempt accounting, and applies no duplicate edit.
	t.Parallel()
	root := initGitRepo(t)
	before := captureDigest(t, root)

	// The fixer session is interrupted before returning; it made no durable change.
	after := captureDigest(t, root)
	changed := before != after

	// Attempt accounting recorded when the batch started must survive the loss.
	ledger := NewAttemptLedger(3)
	ledger.Record("finding-fp", "attempt-1")
	consumedBefore := ledger.Count("finding-fp")

	decision, err := ClassifyCorrectionRecovery(CorrectionEvidence{
		HasRecord: true, ResultReadable: true, ResultComplete: false,
		EvidenceConsistent: true, SnapshotChanged: changed,
	})
	if err != nil {
		t.Fatalf("ClassifyCorrectionRecovery() = %v", err)
	}
	if decision.State != RecoverySafeRetry {
		t.Fatalf("state = %q, want safe retry", decision.State)
	}
	if before != after {
		t.Fatal("no duplicate edit may exist on an unchanged worktree")
	}
	if ledger.Count("finding-fp") != consumedBefore {
		t.Fatal("the consumed attempt must be preserved, not reset")
	}
}

func TestIT018ChangedFixerInterruptionVerifyReview(t *testing.T) {
	// IT-018: a fixer response lost after a durable Git change recovers to
	// verification then a fresh read-only review, never a correction retry.
	t.Parallel()
	root := initGitRepo(t)
	before := captureDigest(t, root)

	// The fixer made a durable owned change before losing its terminal response.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// fixed\n"), 0o600); err != nil {
		t.Fatalf("apply owned change: %v", err)
	}
	after := captureDigest(t, root)
	if before == after {
		t.Fatal("a durable change must move the snapshot digest")
	}

	decision, err := ClassifyCorrectionRecovery(CorrectionEvidence{
		HasRecord: true, ResultReadable: true, ResultComplete: false,
		EvidenceConsistent: true, SnapshotChanged: before != after, ChangeOwned: true,
	})
	if err != nil {
		t.Fatalf("ClassifyCorrectionRecovery() = %v", err)
	}
	if decision.State != RecoveryVerifyReview {
		t.Fatalf("state = %q, want verify then review", decision.State)
	}
	if decision.RepeatsSideEffect() {
		t.Fatal("a durable owned change must never repeat the correction")
	}
}

func TestIT019CorruptEvidenceUnknownOutcome(t *testing.T) {
	// IT-019: corrupt process evidence with a changed snapshot yields UNKNOWN_OUTCOME
	// with no blind repeat; park or fail is selected by run-store integrity trust.
	t.Parallel()
	root := initGitRepo(t)
	before := captureDigest(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// x\n"), 0o600); err != nil {
		t.Fatalf("apply change: %v", err)
	}
	after := captureDigest(t, root)
	changed := before != after

	t.Run("trusted run store parks", func(t *testing.T) {
		t.Parallel()
		decision, err := ClassifyCorrectionRecovery(CorrectionEvidence{
			HasRecord: true, ResultReadable: false, SnapshotChanged: changed,
			CanonicalHistoryTrusted: true,
		})
		if !errors.Is(err, ErrUnknownOutcome) {
			t.Fatalf("err = %v, want ErrUnknownOutcome", err)
		}
		if decision.State != RecoveryPark || decision.RepeatsSideEffect() {
			t.Fatalf("decision = %+v, want a non-repeating park", decision)
		}
	})

	t.Run("untrusted run store fails", func(t *testing.T) {
		t.Parallel()
		decision, err := ClassifyCorrectionRecovery(CorrectionEvidence{
			HasRecord: true, ResultReadable: false, SnapshotChanged: changed,
			CanonicalHistoryTrusted: false,
		})
		if !errors.Is(err, ErrIntegrityFailed) {
			t.Fatalf("err = %v, want ErrIntegrityFailed", err)
		}
		if decision.State != RecoveryFail || decision.Terminal == nil ||
			decision.Terminal.Kind != TerminalFailed {
			t.Fatalf("decision = %+v, want a failed terminal", decision)
		}
	})
}

func TestIT020RestartDuringEveryPhaseCompletesCheckpointFirst(t *testing.T) {
	// IT-020: a restart during any nonterminal phase completes or rejects the prior
	// checkpoint before scheduling any new side effect. A completed checkpoint
	// replays; an incomplete one recovers without a blind repeat.
	t.Parallel()

	phases := []PhaseKind{
		PhaseInitialVerification,
		PhasePreReviewCorrection,
		PhaseReview,
		PhaseCorrection,
		PhasePostCorrectionVerification,
		PhaseEvaluation,
	}
	completed := PhaseEvidence{
		HasRecord: true, ResultComplete: true, ResultReadable: true,
		SnapshotMatches: true, FingerprintMatches: true,
	}
	incomplete := PhaseEvidence{HasRecord: true, ResultReadable: true, ResultComplete: false}

	for _, phase := range phases {
		t.Run("completed checkpoint replays during "+string(phase), func(t *testing.T) {
			t.Parallel()
			decision, err := ClassifyPhaseRecovery(completed)
			if err != nil {
				t.Fatalf("ClassifyPhaseRecovery() = %v", err)
			}
			if decision.State != RecoveryReplay {
				t.Fatalf("completed %s recovery = %q, want replay", phase, decision.State)
			}
			if decision.RepeatsSideEffect() {
				t.Fatalf("replaying a completed %s must not repeat a side effect", phase)
			}
		})
		t.Run("incomplete checkpoint retries during "+string(phase), func(t *testing.T) {
			t.Parallel()
			decision, err := ClassifyPhaseRecovery(incomplete)
			if err != nil {
				t.Fatalf("ClassifyPhaseRecovery() = %v", err)
			}
			if decision.State != RecoveryRetry {
				t.Fatalf("incomplete %s recovery = %q, want retry", phase, decision.State)
			}
			if err := AdmitRetry(decision.State.RecoveryClass()); err != nil {
				t.Fatalf("incomplete %s retry must be admitted: %v", phase, err)
			}
		})
	}
}
