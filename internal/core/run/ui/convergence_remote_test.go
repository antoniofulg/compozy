package ui

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/compozy/compozy/internal/api/contract"

	tea "charm.land/bubbletea/v2"
)

var convergenceViewClock = time.Date(2026, 7, 24, 12, 30, 0, 0, time.UTC)

func activeConvergenceContract() contract.ConvergenceSnapshot {
	return contract.ConvergenceSnapshot{
		Version:       contract.ConvergenceSnapshotVersion,
		ConvergenceID: "conv-1",
		Segment:       contract.ConvergenceSegment{RunID: "run-conv-1", State: "active", SourceRunID: "task-run-1"},
		Target: contract.ConvergenceTarget{
			WorkspaceID: "payments",
			TaskGroupID: "TG-004",
			Branch:      "converge/tg-004",
			Worktree:    ".worktrees/tg-004",
		},
		Config: contract.ConvergenceConfigSummary{
			Profile:    "quality",
			ModelSetup: "balanced",
			AutoCommit: true,
			Limits: contract.ConvergenceLimits{
				MaxReviewRounds:        6,
				MaxFindingAttempts:     3,
				NoProgressRounds:       2,
				ReviewAdmissionTimeout: "1h30m0s",
			},
		},
		Phase: contract.ConvergencePhase{PhaseID: "phase-3", Kind: "correction", Round: 3, Attempt: 2, State: "active"},
		Conditions: []contract.ConvergenceCondition{
			{Kind: contract.ConvergenceConditionInitialVerification, Status: contract.ConvergenceConditionMet},
			{Kind: contract.ConvergenceConditionActionableFindings, Status: contract.ConvergenceConditionBlocked},
			{Kind: contract.ConvergenceConditionWorkspaceStable, Status: contract.ConvergenceConditionMet},
			{Kind: contract.ConvergenceConditionCleanReview, Status: contract.ConvergenceConditionPending},
			{Kind: contract.ConvergenceConditionCurrentVerification, Status: contract.ConvergenceConditionPending},
			{Kind: contract.ConvergenceConditionApprovalRequired, Status: contract.ConvergenceConditionPending},
		},
		Routes: []contract.ConvergenceRoute{
			{
				PhaseID:             "phase-3",
				Role:                "correction",
				Selected:            "codex/fixer-critical/high",
				ConfigurationSource: "model_setup",
			},
		},
		Rounds: []contract.ConvergenceRound{
			{RoundID: "round-1", Number: 1, AdmittedAt: convergenceViewClock.Add(-20 * time.Minute)},
		},
		Batches: []contract.ConvergenceBatch{
			{
				BatchID:             "batch-1",
				Status:              "done",
				FindingFingerprints: []string{"f1", "f2", "f3"},
				AffectedPathsRef:    "evidence/b1.json",
			},
		},
		Findings: []contract.ConvergenceFinding{
			{
				Fingerprint: "f1",
				State:       "actionable",
				Severity:    "critical",
				Attempts:    2,
				EvidenceRef: "evidence/f1.json",
			},
		},
		Handoff: contract.ConvergenceHandoff{
			Branch: "converge/tg-004", Worktree: ".worktrees/tg-004", Snapshot: "af01beef1111", AutoCommit: true,
		},
		Relations: []contract.ConvergenceRelation{
			{Kind: contract.ConvergenceRelationSource, RunID: "task-run-1"},
		},
		Page: contract.ConvergencePage{
			Findings: contract.ConvergenceSectionPage{Total: 1, Shown: 1},
			Cursor:   "42",
		},
		UnresolvedCount: 1,
		LastSeq:         42,
	}
}

func parkedApprovalContract() contract.ConvergenceSnapshot {
	snap := activeConvergenceContract()
	snap.Segment.State = "terminal"
	snap.Terminal = &contract.ConvergenceTerminal{Kind: "parked", Reason: "approval_required", Status: "parked"}
	snap.Approvals = []contract.ConvergenceApproval{
		{
			ProposalID:  "p1",
			Fingerprint: "fp-1",
			Action:      "weaken_test",
			Snapshot:    "af01beef1111",
			EvidenceRef: "evidence/p1.json",
		},
	}
	snap.Conditions[5].Status = contract.ConvergenceConditionBlocked
	snap.Handoff.TerminalKind = "parked"
	snap.Handoff.TerminalReason = "approval_required"
	snap.Handoff.ResumeAvailable = true
	snap.Handoff.ResumeCursor = "cursor-xyz"
	return snap
}

// TestUT038ConvergenceViewSectionsAndActions covers UT-038 at the TUI layer: the
// dedicated sections, conditions, routes, counters, evidence, and the separation of
// the approval and resume actions all project from one snapshot.
func TestUT038ConvergenceViewSectionsAndActions(t *testing.T) {
	t.Parallel()

	t.Run("Should project all dedicated sections from one active snapshot", func(t *testing.T) {
		t.Parallel()
		view := projectConvergenceView(activeConvergenceContract(), convergenceViewClock)
		if view.status != convergenceStatusActive || view.round != 3 || view.maxRounds != 6 {
			t.Fatalf("header = %+v, want active round 3/6", view)
		}
		if view.targetLabel != "payments / TG-004" {
			t.Fatalf("target = %q, want payments / TG-004", view.targetLabel)
		}
		if view.profile != "quality" || view.setup != "balanced" {
			t.Fatalf("profile/setup = %q/%q", view.profile, view.setup)
		}
		if len(view.routes) != 1 || view.routes[0].route != "codex/fixer-critical/high" {
			t.Fatalf("routes = %+v", view.routes)
		}
		if len(view.conditions) != 6 {
			t.Fatalf("conditions = %d, want 6", len(view.conditions))
		}
		if view.limits.attempt != 2 || view.limits.maxAttempts != 3 || !view.limits.admissionActive {
			t.Fatalf("limits = %+v, want attempts 2/3 admission active", view.limits)
		}
		if len(view.batches) != 1 || view.batches[0].findingCount != 3 {
			t.Fatalf("batches = %+v", view.batches)
		}
		if view.findings[0].evidenceRef != "evidence/f1.json" || !view.findings[0].open {
			t.Fatalf("finding = %+v, want open with evidence", view.findings[0])
		}
		if view.unresolved != 1 {
			t.Fatalf("unresolved = %d, want 1", view.unresolved)
		}
		if view.approveEnabled || view.resumeEnabled {
			t.Fatal("active run must expose neither approve nor resume")
		}
	})

	t.Run("Should expose approval and resume as separate available actions when parked", func(t *testing.T) {
		t.Parallel()
		view := projectConvergenceView(parkedApprovalContract(), convergenceViewClock)
		if !view.approveEnabled {
			t.Fatal("parked approval must enable the approve/reject action")
		}
		if !view.resumeEnabled {
			t.Fatal("parked resumable segment must enable the resume action")
		}
		if view.status != convergenceStatusParked || view.terminalReason != "approval_required" {
			t.Fatalf("view = %+v, want parked approval_required", view)
		}
	})
}

// TestUT038ConvergenceApprovalResumeSeparation covers the ADR-006 rule that approval
// and resume are distinct interactions: a reason is mandatory, and an approval never
// resumes the run.
func TestUT038ConvergenceApprovalResumeSeparation(t *testing.T) {
	t.Parallel()

	t.Run("Should require a reason and never auto-resume on approval", func(t *testing.T) {
		t.Parallel()
		var approvals []contract.ApprovalDecisionRequest
		resumeCalls := 0
		mdl := newRemoteConvergenceModel(RemoteConvergenceAttachOptions{
			Convergence:    parkedApprovalContract(),
			HasConvergence: true,
			Approve: func(_ context.Context, req contract.ApprovalDecisionRequest) error {
				approvals = append(approvals, req)
				return nil
			},
			Resume: func(_ context.Context, _ contract.ConvergenceResumeRequest) error {
				resumeCalls++
				return nil
			},
		})

		mdl.beginApproval(convergenceActionApprove)
		if !mdl.prompt.active || mdl.prompt.action != convergenceActionApprove {
			t.Fatalf("prompt = %+v, want active approve prompt", mdl.prompt)
		}
		if cmd := mdl.submitPrompt(); cmd != nil {
			t.Fatal("submit with empty reason must not fire a decision")
		}
		if !mdl.prompt.active || mdl.lastError == "" {
			t.Fatal("empty reason must keep the prompt open and report the requirement")
		}

		mdl.prompt.reason = "weakening approved by lead"
		cmd := mdl.submitPrompt()
		if cmd == nil {
			t.Fatal("submit with reason must fire the decision")
		}
		if mdl.prompt.active {
			t.Fatal("prompt must close after submission")
		}
		if _, ok := cmd().(convergenceActionResultMsg); !ok {
			t.Fatal("approval command must yield an action-result message")
		}
		if len(approvals) != 1 {
			t.Fatalf("approval calls = %d, want 1", len(approvals))
		}
		got := approvals[0]
		if got.Decision != contract.ConvergenceDecisionApprove || got.Reason != "weakening approved by lead" {
			t.Fatalf("decision = %+v, want approve with reason", got)
		}
		if got.ProposalID != "p1" || got.ExpectedFingerprint != "fp-1" || got.ExpectedSnapshot != "af01beef1111" {
			t.Fatalf("decision binding = %+v, want proposal/fingerprint/snapshot", got)
		}
		if resumeCalls != 0 {
			t.Fatal("approval must never auto-resume the run")
		}
	})

	t.Run("Should resume only through the separate resume action", func(t *testing.T) {
		t.Parallel()
		var resumes []contract.ConvergenceResumeRequest
		mdl := newRemoteConvergenceModel(RemoteConvergenceAttachOptions{
			Convergence:    parkedApprovalContract(),
			HasConvergence: true,
			Resume: func(_ context.Context, req contract.ConvergenceResumeRequest) error {
				resumes = append(resumes, req)
				return nil
			},
		})
		mdl.beginResume()
		if !mdl.prompt.active || mdl.prompt.action != convergenceActionResume {
			t.Fatalf("prompt = %+v, want active resume prompt", mdl.prompt)
		}
		cmd := mdl.submitPrompt()
		if cmd == nil {
			t.Fatal("resume submit must fire a command")
		}
		cmd()
		if len(resumes) != 1 || resumes[0].ExpectedCursor != "cursor-xyz" {
			t.Fatalf("resume calls = %+v, want one with cursor-xyz", resumes)
		}
	})

	t.Run("Should refuse resume for a non-resumable segment", func(t *testing.T) {
		t.Parallel()
		mdl := newRemoteConvergenceModel(RemoteConvergenceAttachOptions{
			Convergence:    activeConvergenceContract(),
			HasConvergence: true,
		})
		mdl.beginResume()
		if mdl.prompt.active {
			t.Fatal("resume must not open for an active (non-parked) segment")
		}
		if mdl.lastError == "" {
			t.Fatal("resume refusal must report why")
		}
	})
}

// TestUT038ConvergenceReasonKeyEntry verifies typed reason characters accumulate.
func TestUT038ConvergenceReasonKeyEntry(t *testing.T) {
	t.Parallel()
	mdl := newRemoteConvergenceModel(RemoteConvergenceAttachOptions{
		Convergence:    parkedApprovalContract(),
		HasConvergence: true,
	})
	mdl.beginApproval(convergenceActionReject)
	mdl.handlePromptKey(tea.KeyPressMsg{Text: "n"})
	mdl.handlePromptKey(tea.KeyPressMsg{Text: "o"})
	if mdl.prompt.reason != "no" {
		t.Fatalf("reason = %q, want no", mdl.prompt.reason)
	}
}

// TestUT039ConvergenceViewPagination covers UT-039 at the TUI layer: the bounded view
// keeps current actionable findings, terminal reason, unresolved work, cursor, and
// child relations, and scroll navigation never runs past the retained findings.
func TestUT039ConvergenceViewPagination(t *testing.T) {
	t.Parallel()

	snap := contract.ConvergenceSnapshot{
		Version:  contract.ConvergenceSnapshotVersion,
		Segment:  contract.ConvergenceSegment{RunID: "run-large", State: "terminal"},
		Terminal: &contract.ConvergenceTerminal{Kind: "parked", Reason: "max_rounds", Status: "parked"},
		Handoff: contract.ConvergenceHandoff{
			TerminalKind:    "parked",
			TerminalReason:  "max_rounds",
			ResumeAvailable: true,
			ResumeCursor:    "cur",
		},
		Relations: []contract.ConvergenceRelation{{Kind: contract.ConvergenceRelationChild, RunID: "child-a"}},
		Page: contract.ConvergencePage{
			Findings: contract.ConvergenceSectionPage{Total: 500, Shown: 50, Truncated: true},
			Cursor:   "99999",
		},
		UnresolvedCount: 10,
		LastSeq:         99999,
	}
	for i := 0; i < 10; i++ {
		snap.Findings = append(snap.Findings, contract.ConvergenceFinding{
			Fingerprint: "open-" + strconv.Itoa(i), State: "actionable", Severity: "high",
		})
	}
	for i := 0; i < 40; i++ {
		snap.Findings = append(snap.Findings, contract.ConvergenceFinding{
			Fingerprint: "closed-" + strconv.Itoa(i), State: "resolved", Severity: "low",
		})
	}

	mdl := newRemoteConvergenceModel(RemoteConvergenceAttachOptions{Convergence: snap, HasConvergence: true})

	t.Run("Should retain current state and relations under bounding", func(t *testing.T) {
		if mdl.view.findingsTotal != 500 || mdl.view.findingsShown != 50 {
			t.Fatalf("counters = shown %d/total %d, want 50/500", mdl.view.findingsShown, mdl.view.findingsTotal)
		}
		if mdl.view.unresolved != 10 || mdl.view.terminalReason != "max_rounds" {
			t.Fatalf("view = unresolved %d reason %q, want 10 max_rounds", mdl.view.unresolved, mdl.view.terminalReason)
		}
		if mdl.view.page.Cursor != "99999" {
			t.Fatalf("cursor = %q, want 99999", mdl.view.page.Cursor)
		}
		if len(mdl.view.relations) != 1 || mdl.view.relations[0].RunID != "child-a" {
			t.Fatalf("relations = %+v, want child-a retained", mdl.view.relations)
		}
		openCount := 0
		for _, finding := range mdl.view.findings {
			if finding.open {
				openCount++
			}
		}
		if openCount != 10 {
			t.Fatalf("open findings in view = %d, want all 10 retained", openCount)
		}
	})

	t.Run("Should clamp scroll navigation to the retained findings", func(t *testing.T) {
		mdl.scrollBy(1000)
		if mdl.scroll > mdl.maxScroll() {
			t.Fatalf("scroll = %d exceeded maxScroll %d", mdl.scroll, mdl.maxScroll())
		}
		mdl.scrollBy(-1000)
		if mdl.scroll != 0 {
			t.Fatalf("scroll = %d, want clamped to 0", mdl.scroll)
		}
	})
}
