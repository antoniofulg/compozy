package convergence

import (
	"errors"
	"testing"
)

func TestClassifyPhaseRecoveryEveryUncertainRow(t *testing.T) {
	// UT-023: table-test every uncertain-outcome row for a verification, review, or
	// coordinator phase: no record, pending/incomplete, completed success,
	// completed failure, fingerprint mismatch, and corrupt/unreadable.
	t.Parallel()

	complete := PhaseEvidence{
		HasRecord:          true,
		ResultComplete:     true,
		ResultReadable:     true,
		SnapshotMatches:    true,
		FingerprintMatches: true,
	}
	tests := []struct {
		name      string
		evidence  PhaseEvidence
		wantState RecoveryState
		wantErr   error
	}{
		{
			name:      "no record starts cleanly",
			evidence:  PhaseEvidence{HasRecord: false, ExpectedSnapshotUnchanged: true},
			wantState: RecoveryStart,
		},
		{
			name: "pending incomplete retries",
			evidence: PhaseEvidence{
				HasRecord: true, ResultReadable: true, ResultComplete: false,
			},
			wantState: RecoveryRetry,
		},
		{
			name:      "completed success replays",
			evidence:  complete,
			wantState: RecoveryReplay,
		},
		{
			name:      "completed failure replays without rerun",
			evidence:  complete, // a completed failure is still a durable result to replay
			wantState: RecoveryReplay,
		},
		{
			name: "fingerprint mismatch is unknown and parks under trust",
			evidence: PhaseEvidence{
				HasRecord: true, ResultReadable: true, ResultComplete: true,
				SnapshotMatches: true, FingerprintMatches: false,
				CanonicalHistoryTrusted: true,
			},
			wantState: RecoveryPark,
			wantErr:   ErrUnknownOutcome,
		},
		{
			name: "corrupt unreadable is unknown and parks under trust",
			evidence: PhaseEvidence{
				HasRecord: true, ResultReadable: false,
				CanonicalHistoryTrusted: true,
			},
			wantState: RecoveryPark,
			wantErr:   ErrUnknownOutcome,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := ClassifyPhaseRecovery(tc.evidence)
			assertRecovery(t, decision, err, tc.wantState, tc.wantErr)
		})
	}
}

func TestClassifyPhaseRecoveryReconstructsFromTrustedExit(t *testing.T) {
	// A missing verification result with trusted exit evidence reconstructs pass or
	// fail rather than re-running the command.
	t.Parallel()
	decision, err := ClassifyPhaseRecovery(PhaseEvidence{
		HasRecord: true, ResultReadable: true, ResultComplete: false,
		ExitEvidenceTrusted: true,
	})
	assertRecovery(t, decision, err, RecoveryReconstruct, nil)
	if decision.RepeatsSideEffect() {
		t.Fatal("reconstruct must not repeat a side effect")
	}
}

func TestClassifyCorrectionRecoveryUnchangedSnapshotSafeRetry(t *testing.T) {
	// UT-024: a lost fixer response with a proven unchanged snapshot preserves the
	// consumed attempt and allows a safe retry under a new phase identity.
	t.Parallel()
	decision, err := ClassifyCorrectionRecovery(CorrectionEvidence{
		HasRecord: true, ResultReadable: true, ResultComplete: false,
		EvidenceConsistent: true, SnapshotChanged: false,
	})
	assertRecovery(t, decision, err, RecoverySafeRetry, nil)
	if !decision.RepeatsSideEffect() {
		t.Fatal("a proven-unchanged safe retry repeats the phase without a duplicate edit")
	}
	if decision.State.RecoveryClass() != RecoveryRetryable {
		t.Fatalf("RecoveryClass() = %q, want retryable", decision.State.RecoveryClass())
	}
	if err := AdmitRetry(decision.State.RecoveryClass()); err != nil {
		t.Fatalf("AdmitRetry() = %v, want nil for a safe retry", err)
	}
}

func TestClassifyCorrectionRecoveryChangedOwnedVerifyReview(t *testing.T) {
	// UT-025: a lost fixer response with a changed owned snapshot forbids repeat and
	// schedules verification followed by a fresh read-only review.
	t.Parallel()
	decision, err := ClassifyCorrectionRecovery(CorrectionEvidence{
		HasRecord: true, ResultReadable: true, ResultComplete: false,
		EvidenceConsistent: true, SnapshotChanged: true, ChangeOwned: true,
	})
	assertRecovery(t, decision, err, RecoveryVerifyReview, nil)
	if decision.RepeatsSideEffect() {
		t.Fatal("a changed owned snapshot must never repeat the correction")
	}
}

func TestClassifyCorrectionRecoveryUnknownParkOrFail(t *testing.T) {
	// UT-026: insufficient or conflicting evidence returns UNKNOWN_OUTCOME; it parks
	// when canonical history is trusted and fails when it is not.
	t.Parallel()

	tests := []struct {
		name      string
		evidence  CorrectionEvidence
		wantState RecoveryState
		wantErr   error
	}{
		{
			name: "conflicting evidence parks under trust",
			evidence: CorrectionEvidence{
				HasRecord: true, ResultReadable: true, ResultComplete: false,
				EvidenceConsistent: false, CanonicalHistoryTrusted: true,
			},
			wantState: RecoveryPark,
			wantErr:   ErrUnknownOutcome,
		},
		{
			name: "conflicting evidence fails without trust",
			evidence: CorrectionEvidence{
				HasRecord: true, ResultReadable: true, ResultComplete: false,
				EvidenceConsistent: false, CanonicalHistoryTrusted: false,
			},
			wantState: RecoveryFail,
			wantErr:   ErrIntegrityFailed,
		},
		{
			name: "changed but unattributable evidence is unknown",
			evidence: CorrectionEvidence{
				HasRecord: true, ResultReadable: true, ResultComplete: false,
				EvidenceConsistent: true, SnapshotChanged: true, ChangeOwned: false,
				CanonicalHistoryTrusted: true,
			},
			wantState: RecoveryPark,
			wantErr:   ErrUnknownOutcome,
		},
		{
			name: "corrupt correction evidence fails without trust",
			evidence: CorrectionEvidence{
				HasRecord: true, ResultReadable: false, CanonicalHistoryTrusted: false,
			},
			wantState: RecoveryFail,
			wantErr:   ErrIntegrityFailed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := ClassifyCorrectionRecovery(tc.evidence)
			assertRecovery(t, decision, err, tc.wantState, tc.wantErr)
			if decision.RepeatsSideEffect() {
				t.Fatal("an unknown outcome must never repeat a side effect")
			}
			if err := AdmitRetry(decision.State.RecoveryClass()); err == nil {
				t.Fatal("AdmitRetry() must deny a free retry of unknown work")
			}
		})
	}
}

// assertRecovery checks a recovery decision's state, error sentinel, and terminal
// invariants: only a fail carries a failed terminal, and a park never does.
func assertRecovery(
	t *testing.T,
	decision RecoveryDecision,
	err error,
	wantState RecoveryState,
	wantErr error,
) {
	t.Helper()
	if decision.State != wantState {
		t.Fatalf("state = %q, want %q", decision.State, wantState)
	}
	if wantErr == nil {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	} else if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	switch wantState {
	case RecoveryFail:
		if decision.Terminal == nil || decision.Terminal.Kind != TerminalFailed {
			t.Fatalf("fail must carry a failed terminal, got %+v", decision.Terminal)
		}
	case RecoveryPark:
		if decision.Terminal != nil {
			t.Fatalf("a trusted park must not carry a terminal, got %+v", decision.Terminal)
		}
	}
}
