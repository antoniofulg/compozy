package convergence

import (
	"testing"
	"time"
)

func TestHappyPathTransitionsToClean(t *testing.T) {
	t.Parallel()
	t.Run("Should walk prepared to clean with snapshot equality and no findings", func(t *testing.T) {
		t.Parallel()
		steps := []struct{ from, to PhaseKind }{
			{"", PhaseInitialVerification},
			{PhaseInitialVerification, PhaseReview},
			{PhaseReview, PhasePostCorrectionVerification},
			{PhasePostCorrectionVerification, PhaseEvaluation},
		}
		for _, step := range steps {
			if !CanTransition(step.from, step.to) {
				t.Fatalf("expected legal transition %q -> %q", step.from, step.to)
			}
		}
		result := EvaluateTerminal(EvaluationInput{
			CurrentReviewClean:                true,
			VerificationPassedCurrentSnapshot: true,
			AcceptedActionableFindings:        0,
		})
		if !result.Terminal || !result.Outcome.IsClean() {
			t.Fatalf("expected clean terminal, got %+v", result)
		}
	})
	t.Run("Should reject an illegal transition", func(t *testing.T) {
		t.Parallel()
		if CanTransition(PhaseReview, PhaseEvaluation) {
			t.Fatal("review may not transition straight to evaluation")
		}
	})
}

func TestVerificationAttemptAccounting(t *testing.T) {
	t.Parallel()
	t.Run("Should exhaust on the third unsuccessful default attempt", func(t *testing.T) {
		t.Parallel()
		ledger := NewAttemptLedger(DefaultMaxVerificationAttempts)
		key := "make-verify-failure"
		if count, _ := ledger.Record(key, "a1"); count != 1 {
			t.Fatalf("expected attempt 1, got %d", count)
		}
		if ledger.Record(key, "a2"); ledger.Exhausted(key) {
			t.Fatal("a passing retry must be admissible after two attempts")
		}
		ledger.Record(key, "a3")
		if !ledger.Exhausted(key) {
			t.Fatal("expected exhaustion after the third attempt")
		}
		result := EvaluateTerminal(EvaluationInput{VerificationAttemptsExhausted: true})
		if result.Outcome.Reason != ParkedVerificationFailed {
			t.Fatalf("expected verification_failed park, got %+v", result.Outcome)
		}
	})
	t.Run("Should ignore duplicate attempt identities", func(t *testing.T) {
		t.Parallel()
		ledger := NewAttemptLedger(3)
		ledger.Record("k", "a1")
		if count, applied := ledger.Record("k", "a1"); applied || count != 1 {
			t.Fatalf("expected idempotent duplicate, got %d applied=%v", count, applied)
		}
	})
}

func TestRoundAdmissionBoundary(t *testing.T) {
	t.Parallel()
	t.Run("Should admit a round just before the boundary", func(t *testing.T) {
		t.Parallel()
		clock := RoundClock{CompletedRounds: 0, MaxRounds: 6,
			Elapsed: 89*time.Minute + 59*time.Second, AdmissionTimeout: 90 * time.Minute}
		if ok, _ := clock.CanAdmitRound(); !ok {
			t.Fatal("expected admission at 89m59s")
		}
	})
	t.Run("Should reject the next round after the boundary passes", func(t *testing.T) {
		t.Parallel()
		clock := RoundClock{CompletedRounds: 1, MaxRounds: 6,
			Elapsed: 90 * time.Minute, AdmissionTimeout: 90 * time.Minute}
		ok, reason := clock.CanAdmitRound()
		if ok || reason != ParkedTimeLimit {
			t.Fatalf("expected time_limit rejection, got ok=%v reason=%s", ok, reason)
		}
	})
	t.Run("Should reject round seven with max_rounds", func(t *testing.T) {
		t.Parallel()
		clock := RoundClock{CompletedRounds: 6, MaxRounds: 6,
			Elapsed: 10 * time.Minute, AdmissionTimeout: 90 * time.Minute}
		ok, reason := clock.CanAdmitRound()
		if ok || reason != ParkedMaxRounds {
			t.Fatalf("expected max_rounds rejection, got ok=%v reason=%s", ok, reason)
		}
	})
}

func TestTerminalPriority(t *testing.T) {
	t.Parallel()
	allParks := EvaluationInput{
		ApprovalPending: true, WorkspaceDiverged: true, VerificationAttemptsExhausted: true,
		FindingAttemptsExhausted: true, OscillationReached: true, NoProgressReached: true,
		MaxRoundsReached: true, TimeLimitReached: true, RuntimeUnavailable: true,
	}
	withClean := func(in EvaluationInput) EvaluationInput {
		in.CurrentReviewClean = true
		in.VerificationPassedCurrentSnapshot = true
		return in
	}
	cases := []struct {
		name string
		in   EvaluationInput
		kind TerminalKind
		reas ParkedReason
	}{
		{"cancellation beats clean", withClean(EvaluationInput{CancellationAccepted: true}), TerminalCancelled, ""},
		{"failed beats clean", withClean(EvaluationInput{DurableStateUntrusted: true}), TerminalFailed, ""},
		{"clean beats every limit", withClean(allParks), TerminalClean, ""},
		{"approval first among parks", allParks, TerminalParked, ParkedApprovalRequired},
		{"workspace before verification", parksFrom(allParks, "approval"), TerminalParked, ParkedWorkspaceChanged},
		{
			"verification before fix",
			parksFrom(allParks, "approval", "workspace"),
			TerminalParked,
			ParkedVerificationFailed,
		},
		{"max_rounds before time_limit", roundVsTime(), TerminalParked, ParkedMaxRounds},
		{
			"runtime unavailable last",
			EvaluationInput{RuntimeUnavailable: true},
			TerminalParked,
			ParkedRuntimeUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run("Should apply priority: "+tc.name, func(t *testing.T) {
			t.Parallel()
			result := EvaluateTerminal(tc.in)
			if result.Outcome.Kind != tc.kind || result.Outcome.Reason != tc.reas {
				t.Fatalf("expected %s/%s, got %+v", tc.kind, tc.reas, result.Outcome)
			}
			if tc.kind != TerminalClean && result.Outcome.IsClean() {
				t.Fatal("non-clean outcome must never report clean")
			}
		})
	}
	t.Run("Should advance to the next round when no condition matches", func(t *testing.T) {
		t.Parallel()
		result := EvaluateTerminal(EvaluationInput{AcceptedActionableFindings: 1})
		if result.Terminal || !result.NextRound {
			t.Fatalf("expected next round, got %+v", result)
		}
	})
}

func parksFrom(in EvaluationInput, cleared ...string) EvaluationInput {
	for _, name := range cleared {
		switch name {
		case "approval":
			in.ApprovalPending = false
		case "workspace":
			in.WorkspaceDiverged = false
		}
	}
	return in
}

func roundVsTime() EvaluationInput {
	return EvaluationInput{MaxRoundsReached: true, TimeLimitReached: true}
}

func TestProgressResetAndNoProgress(t *testing.T) {
	t.Parallel()
	t.Run("Should reset no-progress on any measurable improvement", func(t *testing.T) {
		t.Parallel()
		signals := []ProgressSignals{
			{FindingResolved: true},
			{SeverityDecreased: true},
			{VerificationGatePassed: true},
		}
		for i, signal := range signals {
			ledger := NewProgressLedger()
			ledger.Record(1, ProgressSignals{})
			if count, _ := ledger.Record(2, signal); count != 0 {
				t.Fatalf("signal %d should reset the counter, got %d", i, count)
			}
		}
	})
	t.Run("Should not reset for code-only changes and park at two rounds", func(t *testing.T) {
		t.Parallel()
		ledger := NewProgressLedger()
		ledger.Record(1, ProgressSignals{})
		if count, _ := ledger.Record(2, ProgressSignals{}); count != 2 {
			t.Fatalf("expected two consecutive no-progress rounds, got %d", count)
		}
		if !ledger.NoProgressReached(DefaultNoProgressRounds) {
			t.Fatal("expected no_progress to be reached at two rounds")
		}
	})
	t.Run("Should process a duplicate round evaluation once", func(t *testing.T) {
		t.Parallel()
		ledger := NewProgressLedger()
		ledger.Record(1, ProgressSignals{})
		ledger.Record(2, ProgressSignals{})
		count, applied := ledger.Record(2, ProgressSignals{FindingResolved: true})
		if applied || count != 2 {
			t.Fatalf("expected idempotent duplicate evaluation, got %d applied=%v", count, applied)
		}
	})
}

func TestOscillationCounting(t *testing.T) {
	t.Parallel()
	t.Run("Should park on the second disappearance-and-return cycle", func(t *testing.T) {
		t.Parallel()
		tracker := NewOscillationTracker()
		fp := FindingFingerprint("finding-a")
		tracker.Observe(fp, true)  // first appearance, no cycle
		tracker.Observe(fp, false) // disappear
		if c := tracker.Observe(fp, true); c != 1 {
			t.Fatalf("expected one cycle, got %d", c)
		}
		tracker.Observe(fp, false) // disappear
		if c := tracker.Observe(fp, true); c != 2 {
			t.Fatalf("expected two cycles, got %d", c)
		}
		if !tracker.Reached(DefaultOscillationCycles) {
			t.Fatal("expected oscillation to be reached at two cycles")
		}
	})
	t.Run("Should not count a still-present finding as a new cycle", func(t *testing.T) {
		t.Parallel()
		tracker := NewOscillationTracker()
		fp := FindingFingerprint("finding-b")
		tracker.Observe(fp, true)
		if c := tracker.Observe(fp, true); c != 0 {
			t.Fatalf("line movement (same identity, still present) must not add a cycle, got %d", c)
		}
	})
}
