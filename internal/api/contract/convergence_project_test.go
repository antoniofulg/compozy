package contract_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/compozy/compozy/internal/api/contract"
	"github.com/compozy/compozy/internal/core/convergence"
)

var convergenceFixtureClock = time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

// activeConvergenceSnapshot is a mid-run correction snapshot: initial verification
// passed, one actionable and one resolved finding, and the current phase bound to a
// post-correction snapshot that no verification has passed yet.
func activeConvergenceSnapshot() convergence.Snapshot {
	exit := 0
	return convergence.Snapshot{
		ConvergenceID: "conv-1",
		RequestID:     "req-1",
		Segment: convergence.Segment{
			RunID:       "run-conv-1",
			Ordinal:     1,
			SourceRunID: "task-run-1",
			State:       convergence.SegmentActive,
		},
		Target: convergence.TargetBinding{
			WorkspaceID: "payments",
			TaskGroupID: "TG-004",
			Branch:      "converge/tg-004",
			Worktree:    ".worktrees/tg-004",
			Snapshot:    "8d92abcd0000",
		},
		Config: convergence.FrozenConfiguration{
			ProfileName:    "quality",
			ModelSetupName: "balanced",
			AutoCommit:     true,
			Limits: convergence.Limits{
				MaxReviewRounds:         6,
				MaxFindingAttempts:      3,
				MaxVerificationAttempts: 3,
				NoProgressRounds:        2,
				ReviewAdmissionTimeout:  90 * time.Minute,
				OscillationCycles:       2,
			},
		},
		Phase: convergence.PhaseState{
			PhaseID:  "phase-3",
			Kind:     convergence.PhaseCorrection,
			Round:    3,
			Attempt:  2,
			Snapshot: "af01beef1111",
			State:    convergence.SegmentActive,
		},
		Routes: []convergence.RouteSelection{{
			PhaseID:             "phase-3",
			Role:                "correction",
			Primary:             "codex/fixer-critical/high",
			Selected:            "codex/fixer-critical/high",
			ConfigurationSource: "model_setup",
		}},
		Rounds: []convergence.RoundState{
			{RoundID: "round-1", Number: 1, AdmittedAt: convergenceFixtureClock},
			{RoundID: "round-3", Number: 3, AdmittedAt: convergenceFixtureClock.Add(30 * time.Minute)},
		},
		Batches: []convergence.BatchState{
			{
				BatchID:             "batch-1",
				PhaseID:             "phase-3",
				FindingFingerprints: []string{"f1", "f2", "f3"},
				Status:              "done",
				AffectedPathsRef:    "evidence/batch-1.json",
			},
			{
				BatchID:             "batch-2",
				PhaseID:             "phase-3",
				FindingFingerprints: []string{"f4"},
				Status:              "running",
				AffectedPathsRef:    "evidence/batch-2.json",
			},
		},
		Findings: []convergence.Finding{
			{
				Fingerprint: "f1",
				State:       convergence.FindingActionable,
				Severity:    convergence.SeverityCritical,
				Attempts:    2,
				FirstSeq:    10,
				EvidenceRef: "evidence/f1.json",
			},
			{
				Fingerprint: "f2",
				State:       convergence.FindingResolved,
				Severity:    convergence.SeverityHigh,
				Attempts:    1,
				FirstSeq:    11,
			},
		},
		Observations: []convergence.FindingObservation{{ObservationID: "obs-1", Fingerprint: "f1"}},
		Verification: []convergence.VerificationResult{{
			VerificationID: "v-init",
			PhaseID:        "phase-0",
			Passed:         true,
			Snapshot:       "8d92abcd0000",
			ExitCode:       &exit,
			Attempt:        1,
			EvidencePath:   "evidence/v-init.log",
		}},
		LastSeq: 42,
	}
}

// TestUT038ProjectConvergenceSnapshotSections covers UT-038: one snapshot projects
// the dedicated convergence sections, conditions, batches, routes, counters,
// evidence, and separate approval/resume availability.
func TestUT038ProjectConvergenceSnapshotSections(t *testing.T) {
	t.Parallel()

	t.Run("Should project active-run sections, conditions, and evidence from one snapshot", func(t *testing.T) {
		t.Parallel()
		projected := contract.ProjectConvergenceSnapshot(
			activeConvergenceSnapshot(),
			contract.DefaultConvergenceProjectionOptions(),
		)

		if projected.Version != contract.ConvergenceSnapshotVersion {
			t.Fatalf("version = %d, want %d", projected.Version, contract.ConvergenceSnapshotVersion)
		}
		if projected.Phase.Round != 3 || projected.Phase.Kind != string(convergence.PhaseCorrection) {
			t.Fatalf("phase = %+v, want round 3 correction", projected.Phase)
		}
		wantConditions := map[string]string{
			contract.ConvergenceConditionInitialVerification: contract.ConvergenceConditionMet,
			contract.ConvergenceConditionActionableFindings:  contract.ConvergenceConditionBlocked,
			contract.ConvergenceConditionWorkspaceStable:     contract.ConvergenceConditionMet,
			contract.ConvergenceConditionCleanReview:         contract.ConvergenceConditionPending,
			contract.ConvergenceConditionCurrentVerification: contract.ConvergenceConditionPending,
			contract.ConvergenceConditionApprovalRequired:    contract.ConvergenceConditionPending,
		}
		assertConvergenceConditions(t, projected.Conditions, wantConditions)

		if len(projected.Routes) != 1 || projected.Routes[0].Selected != "codex/fixer-critical/high" {
			t.Fatalf("routes = %+v, want one correction route", projected.Routes)
		}
		if len(projected.Batches) != 2 || len(projected.Batches[0].FindingFingerprints) != 3 {
			t.Fatalf("batches = %+v, want two batches with 3/1 findings", projected.Batches)
		}
		if projected.UnresolvedCount != 1 {
			t.Fatalf("unresolved = %d, want 1", projected.UnresolvedCount)
		}
		if projected.Findings[0].EvidenceRef != "evidence/f1.json" {
			t.Fatalf("finding evidence ref = %q, want evidence/f1.json", projected.Findings[0].EvidenceRef)
		}
		if projected.Handoff.Branch != "converge/tg-004" || projected.Handoff.Snapshot != "af01beef1111" {
			t.Fatalf("handoff = %+v, want branch/current snapshot", projected.Handoff)
		}
		if projected.Handoff.ResumeAvailable {
			t.Fatal("active run must not offer resume")
		}
		if relationKind(projected.Relations, "task-run-1") != contract.ConvergenceRelationSource {
			t.Fatalf("relations = %+v, want source relation to task-run-1", projected.Relations)
		}
		if projected.Page.Cursor != "42" {
			t.Fatalf("page cursor = %q, want 42", projected.Page.Cursor)
		}
	})

	t.Run("Should mark approval and resume as separate available actions when parked for approval", func(t *testing.T) {
		t.Parallel()
		snap := activeConvergenceSnapshot()
		snap.Segment.State = convergence.SegmentTerminal
		snap.Segment.ResumeCursor = "cursor-xyz"
		snap.Segment.ResumeClaimed = false
		snap.Terminal = &convergence.TerminalOutcome{
			Kind:   convergence.TerminalParked,
			Reason: convergence.ParkedApprovalRequired,
		}
		snap.Approvals = []convergence.ApprovalProposal{{
			ProposalID:  "p1",
			Fingerprint: "fp1",
			Action:      "weaken_test",
			Snapshot:    "af01beef1111",
			EvidenceRef: "evidence/p1.json",
		}}
		projected := contract.ProjectConvergenceSnapshot(snap, contract.DefaultConvergenceProjectionOptions())

		if projected.Terminal == nil || projected.Terminal.Kind != "parked" {
			t.Fatalf("terminal = %+v, want parked", projected.Terminal)
		}
		if projected.Terminal.Reason != string(convergence.ParkedApprovalRequired) {
			t.Fatalf("terminal reason = %q, want approval_required", projected.Terminal.Reason)
		}
		if !projected.Handoff.ResumeAvailable || projected.Handoff.ResumeCursor != "cursor-xyz" {
			t.Fatalf("handoff = %+v, want resume available with cursor", projected.Handoff)
		}
		if conditionStatus(projected.Conditions, contract.ConvergenceConditionApprovalRequired) !=
			contract.ConvergenceConditionBlocked {
			t.Fatal("approval_required condition must be blocked while a proposal is pending")
		}
		if len(projected.Approvals) != 1 || projected.Approvals[0].Decision != "" {
			t.Fatalf("approvals = %+v, want one pending proposal", projected.Approvals)
		}
	})

	t.Run("Should mark every condition met and no resume for a clean terminal", func(t *testing.T) {
		t.Parallel()
		snap := activeConvergenceSnapshot()
		snap.Findings[0].State = convergence.FindingResolved
		snap.Segment.State = convergence.SegmentTerminal
		snap.Terminal = &convergence.TerminalOutcome{Kind: convergence.TerminalClean}
		projected := contract.ProjectConvergenceSnapshot(snap, contract.DefaultConvergenceProjectionOptions())

		if projected.Terminal == nil || projected.Terminal.Status != "completed" {
			t.Fatalf("terminal = %+v, want clean->completed", projected.Terminal)
		}
		for _, condition := range projected.Conditions {
			if condition.Status != contract.ConvergenceConditionMet {
				t.Fatalf("condition %q = %q, want met for clean", condition.Kind, condition.Status)
			}
		}
		if projected.Handoff.ResumeAvailable {
			t.Fatal("clean terminal must not offer resume")
		}
	})
}

// TestUT039BoundedConvergenceProjection covers UT-039: bounded histories never drop
// current actionable findings, the terminal reason, unresolved work, the cursor, or
// child relations.
func TestUT039BoundedConvergenceProjection(t *testing.T) {
	t.Parallel()

	t.Run("Should bound closed history while retaining current state, cursor, and relations", func(t *testing.T) {
		t.Parallel()
		snap := largeConvergenceSnapshot(10, 490, 80, 200, 80)
		opts := contract.ConvergenceProjectionOptions{
			MaxFindings:     50,
			MaxRounds:       20,
			MaxBatches:      30,
			MaxVerification: 15,
			Children: []contract.ConvergenceRelation{
				{Kind: contract.ConvergenceRelationContinuation, RunID: "child-a"},
				{Kind: contract.ConvergenceRelationContinuation, RunID: "child-b"},
				{Kind: contract.ConvergenceRelationChild, RunID: "child-c"},
			},
		}
		projected := contract.ProjectConvergenceSnapshot(snap, opts)

		for i := 0; i < 10; i++ {
			fp := "open-" + strconv.Itoa(i)
			if !hasFinding(projected.Findings, fp) {
				t.Fatalf("bounded projection dropped current actionable finding %q", fp)
			}
		}
		if projected.Page.Findings.Total != 500 || !projected.Page.Findings.Truncated {
			t.Fatalf("findings page = %+v, want total 500 truncated", projected.Page.Findings)
		}
		if projected.UnresolvedCount != 10 {
			t.Fatalf("unresolved = %d, want 10", projected.UnresolvedCount)
		}
		if projected.Terminal == nil || projected.Terminal.Reason != string(convergence.ParkedMaxRounds) {
			t.Fatalf("terminal = %+v, want parked max_rounds reason retained", projected.Terminal)
		}
		if projected.Page.Cursor != "99999" {
			t.Fatalf("cursor = %q, want 99999", projected.Page.Cursor)
		}
		if len(projected.Rounds) != 20 || projected.Page.Rounds.Total != 80 {
			t.Fatalf("rounds = %d (page %+v), want 20 shown of 80", len(projected.Rounds), projected.Page.Rounds)
		}
		// The retained rounds must be the most recent tail, in order.
		if projected.Rounds[len(projected.Rounds)-1].Number != 80 {
			t.Fatalf("last round = %d, want 80 (tail retained)", projected.Rounds[len(projected.Rounds)-1].Number)
		}
		if len(projected.Batches) != 30 || len(projected.Verification) != 15 {
			t.Fatalf("batches/verification = %d/%d, want 30/15", len(projected.Batches), len(projected.Verification))
		}
		for _, runID := range []string{"child-a", "child-b", "child-c"} {
			if !hasRelation(projected.Relations, runID) {
				t.Fatalf("bounded projection dropped child relation %q", runID)
			}
		}
	})

	t.Run("Should retain every actionable finding even when open work exceeds the limit", func(t *testing.T) {
		t.Parallel()
		snap := largeConvergenceSnapshot(10, 0, 1, 1, 1)
		projected := contract.ProjectConvergenceSnapshot(snap, contract.ConvergenceProjectionOptions{MaxFindings: 5})
		if len(projected.Findings) != 10 {
			t.Fatalf("findings = %d, want all 10 actionable retained despite limit 5", len(projected.Findings))
		}
		if projected.UnresolvedCount != 10 {
			t.Fatalf("unresolved = %d, want 10", projected.UnresolvedCount)
		}
	})
}

// largeConvergenceSnapshot builds a deterministic large parked snapshot with the
// requested counts of open findings, closed findings, rounds, batches, and
// verifications. Sequences increase with index so tail bounding is observable.
func largeConvergenceSnapshot(openCount, closedCount, rounds, batches, verifications int) convergence.Snapshot {
	snap := convergence.Snapshot{
		ConvergenceID: "conv-large",
		Segment:       convergence.Segment{RunID: "run-large", State: convergence.SegmentTerminal},
		Terminal: &convergence.TerminalOutcome{
			Kind:   convergence.TerminalParked,
			Reason: convergence.ParkedMaxRounds,
		},
		LastSeq: 99999,
	}
	severities := []convergence.Severity{
		convergence.SeverityLow,
		convergence.SeverityMedium,
		convergence.SeverityHigh,
		convergence.SeverityCritical,
	}
	seq := uint64(0)
	for i := 0; i < openCount; i++ {
		seq++
		snap.Findings = append(snap.Findings, convergence.Finding{
			Fingerprint: convergence.FindingFingerprint("open-" + strconv.Itoa(i)),
			State:       convergence.FindingActionable,
			Severity:    severities[i%len(severities)],
			FirstSeq:    seq,
		})
	}
	for i := 0; i < closedCount; i++ {
		seq++
		snap.Findings = append(snap.Findings, convergence.Finding{
			Fingerprint: convergence.FindingFingerprint("closed-" + strconv.Itoa(i)),
			State:       convergence.FindingResolved,
			Severity:    severities[i%len(severities)],
			FirstSeq:    seq,
		})
	}
	for i := 1; i <= rounds; i++ {
		snap.Rounds = append(snap.Rounds, convergence.RoundState{
			RoundID: "round-" + strconv.Itoa(i),
			Number:  i,
		})
	}
	for i := 1; i <= batches; i++ {
		snap.Batches = append(snap.Batches, convergence.BatchState{
			BatchID: "batch-" + strconv.Itoa(i),
			Status:  "done",
		})
	}
	for i := 1; i <= verifications; i++ {
		snap.Verification = append(snap.Verification, convergence.VerificationResult{
			VerificationID: "verify-" + strconv.Itoa(i),
			Passed:         true,
			EvidencePath:   "evidence/verify-" + strconv.Itoa(i) + ".log",
		})
	}
	return snap
}

func assertConvergenceConditions(t *testing.T, conditions []contract.ConvergenceCondition, want map[string]string) {
	t.Helper()
	if len(conditions) != len(want) {
		t.Fatalf("conditions = %d, want %d", len(conditions), len(want))
	}
	for kind, status := range want {
		if got := conditionStatus(conditions, kind); got != status {
			t.Fatalf("condition %q = %q, want %q", kind, got, status)
		}
	}
}

func conditionStatus(conditions []contract.ConvergenceCondition, kind string) string {
	for i := range conditions {
		if conditions[i].Kind == kind {
			return conditions[i].Status
		}
	}
	return ""
}

func relationKind(relations []contract.ConvergenceRelation, runID string) string {
	for i := range relations {
		if relations[i].RunID == runID {
			return relations[i].Kind
		}
	}
	return ""
}

func hasRelation(relations []contract.ConvergenceRelation, runID string) bool {
	return relationKind(relations, runID) != ""
}

func hasFinding(findings []contract.ConvergenceFinding, fingerprint string) bool {
	for i := range findings {
		if findings[i].Fingerprint == fingerprint {
			return true
		}
	}
	return false
}
