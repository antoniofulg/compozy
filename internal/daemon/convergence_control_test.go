package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/compozy/compozy/internal/api/contract"
	apicore "github.com/compozy/compozy/internal/api/core"
	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/core/model"
	workspacecfg "github.com/compozy/compozy/internal/core/workspace"
	"github.com/compozy/compozy/internal/store/convergencestore"
	"github.com/compozy/compozy/internal/store/globaldb"
	"github.com/compozy/compozy/pkg/compozy/events"
	"github.com/compozy/compozy/pkg/compozy/events/kinds"
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
	base := contract.ApprovalDecisionRequest{
		ProposalID:          "prop-1",
		Decision:            contract.ConvergenceDecisionApprove,
		Reason:              "reviewed and accepted the scoped exception",
		ExpectedFingerprint: "fp-1",
		ExpectedSnapshot:    "snap-1",
	}
	t.Run("Should record a valid decision without resuming", func(t *testing.T) {
		t.Parallel()
		got, err := decideApproval(proposal, base)
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
	t.Run("Should reject a missing reason", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Reason = "   "
		if _, err := decideApproval(proposal, req); !errors.Is(err, errConvergenceApprovalInvalid) {
			t.Fatalf("expected invalid, got %v", err)
		}
	})
	t.Run("Should reject a missing expected fingerprint", func(t *testing.T) {
		t.Parallel()
		req := base
		req.ExpectedFingerprint = "   "
		if _, err := decideApproval(proposal, req); !errors.Is(err, errConvergenceApprovalInvalid) {
			t.Fatalf("expected invalid, got %v", err)
		}
	})
	t.Run("Should reject a missing expected snapshot", func(t *testing.T) {
		t.Parallel()
		req := base
		req.ExpectedSnapshot = "   "
		if _, err := decideApproval(proposal, req); !errors.Is(err, errConvergenceApprovalInvalid) {
			t.Fatalf("expected invalid, got %v", err)
		}
	})
	t.Run("Should reject an unknown decision", func(t *testing.T) {
		t.Parallel()
		req := base
		req.Decision = "maybe"
		if _, err := decideApproval(proposal, req); !errors.Is(err, errConvergenceApprovalInvalid) {
			t.Fatalf("expected invalid, got %v", err)
		}
	})
	t.Run("Should reject a changed fingerprint as stale", func(t *testing.T) {
		t.Parallel()
		req := base
		req.ExpectedFingerprint = "fp-2"
		if _, err := decideApproval(proposal, req); !errors.Is(err, convergence.ErrApprovalStale) {
			t.Fatalf("expected stale, got %v", err)
		}
	})
	t.Run("Should reject a changed snapshot as stale", func(t *testing.T) {
		t.Parallel()
		req := base
		req.ExpectedSnapshot = "snap-2"
		if _, err := decideApproval(proposal, req); !errors.Is(err, convergence.ErrApprovalStale) {
			t.Fatalf("expected stale, got %v", err)
		}
	})
	t.Run("Should replay an identical decision idempotently", func(t *testing.T) {
		t.Parallel()
		decided := proposal
		decided.Decision = contract.ConvergenceDecisionApprove
		decided.Reason = "reviewed and accepted the scoped exception"
		got, err := decideApproval(decided, base)
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
		if _, err := decideApproval(decided, base); !errors.Is(err, convergence.ErrApprovalStale) {
			t.Fatalf("expected stale on conflicting decision, got %v", err)
		}
	})
}

func TestConvergenceControlRejectsUnknownAndWrongModeRunsWithoutCreatingStore(t *testing.T) {
	t.Parallel()

	approval := contract.ApprovalDecisionRequest{
		ProposalID:          "proposal-1",
		Decision:            contract.ConvergenceDecisionApprove,
		Reason:              "reviewed",
		ExpectedFingerprint: "fp-1",
		ExpectedSnapshot:    "snap-1",
	}
	operations := []struct {
		name   string
		invoke func(context.Context, *RunManager, string) error
	}{
		{
			name: "approval",
			invoke: func(ctx context.Context, manager *RunManager, runID string) error {
				return manager.DecideConvergenceApproval(ctx, runID, approval)
			},
		},
		{
			name: "resume",
			invoke: func(ctx context.Context, manager *RunManager, runID string) error {
				_, err := manager.ResumeConvergence(
					ctx,
					runID,
					contract.ConvergenceResumeRequest{ExpectedCursor: "resume:1"},
				)
				return err
			},
		},
	}
	targets := []struct {
		name      string
		suffix    string
		seedRun   bool
		runMode   string
		runStatus string
	}{
		{name: "unknown run", suffix: "unknown"},
		{
			name:      "wrong-mode run",
			suffix:    "wrong-mode",
			seedRun:   true,
			runMode:   runModeTask,
			runStatus: runStatusCompleted,
		},
	}

	for _, operation := range operations {
		for _, target := range targets {
			t.Run("Should return not found for "+operation.name+" on "+target.name, func(t *testing.T) {
				t.Parallel()
				env := newRunManagerTestEnv(t, runManagerTestDeps{})
				ctx := context.Background()
				runID := "run-" + operation.name + "-" + target.suffix
				if target.seedRun {
					workspace, err := env.globalDB.Register(ctx, env.workspaceRoot, operation.name+"-wrong-mode")
					if err != nil {
						t.Fatalf("Register() error = %v", err)
					}
					if _, err := env.globalDB.PutRun(ctx, globaldb.Run{
						RunID:            runID,
						WorkspaceID:      workspace.ID,
						Mode:             target.runMode,
						Status:           target.runStatus,
						PresentationMode: defaultPresentationMode,
						StartedAt:        time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC),
					}); err != nil {
						t.Fatalf("PutRun() error = %v", err)
					}
				}

				err := operation.invoke(ctx, env.manager, runID)
				if !errors.Is(err, globaldb.ErrRunNotFound) {
					t.Fatalf("%s(%q) error = %v, want ErrRunNotFound", operation.name, runID, err)
				}
				runDBPath := env.manager.runArtifacts(runID).RunDBPath
				if _, statErr := os.Stat(runDBPath); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("%s(%q) created run store %q (stat error = %v)",
						operation.name, runID, runDBPath, statErr)
				}
			})
		}
	}
}

func TestConvergenceApprovalAndResumeControlPlane(t *testing.T) {
	t.Parallel()

	const (
		sourceRunID   = "run-convergence-1"
		resumedRunID  = "run-convergence-2"
		convergenceID = "cvg-control"
	)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	verification := []string{"make", "verify"}
	focusedMaxRounds := 3
	focusedProfile := "focused"
	resumeSetup := "resume-setup"
	reviewIDE := "claude"
	reviewModel := "review-setup"
	reviewEffort := "high"
	correctionIDE := "codex"
	correctionModel := "correct-setup"
	correctionEffort := "low"
	env := newRunManagerTestEnv(t, runManagerTestDeps{
		now: func() time.Time {
			return now
		},
		buildRunID: func(*model.RuntimeConfig) (string, error) {
			return resumedRunID, nil
		},
		loadProjectConfig: func(context.Context, string) (workspacecfg.ProjectConfig, error) {
			return workspacecfg.ProjectConfig{
				Convergence: convergence.Config{
					Verification: convergence.VerificationConfig{Command: &verification},
					Profiles: map[string]convergence.ProfileConfig{
						focusedProfile: {MaxReviewRounds: &focusedMaxRounds},
					},
					ModelSetups: map[string]convergence.ModelSetupConfig{
						resumeSetup: {
							Review: convergence.ReviewSetupConfig{
								RouteConfig: convergence.RouteConfig{
									IDE:             &reviewIDE,
									Model:           &reviewModel,
									ReasoningEffort: &reviewEffort,
								},
							},
							Correction: convergence.CorrectionSetupConfig{
								RouteConfig: convergence.RouteConfig{
									IDE:             &correctionIDE,
									Model:           &correctionModel,
									ReasoningEffort: &correctionEffort,
								},
							},
						},
					},
				},
			}, nil
		},
	})
	ctx := context.Background()
	workspace, err := env.globalDB.Register(ctx, env.workspaceRoot, "control-test")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := env.globalDB.PutRun(ctx, globaldb.Run{
		RunID:            sourceRunID,
		WorkspaceID:      workspace.ID,
		Mode:             runModeConvergence,
		Status:           runStatusParked,
		PresentationMode: defaultPresentationMode,
		StartedAt:        now,
	}); err != nil {
		t.Fatalf("PutRun(source) error = %v", err)
	}
	if err := reserveRunDirectory(env.manager.runArtifacts(sourceRunID).RunDir); err != nil {
		t.Fatalf("reserve source run directory: %v", err)
	}
	db, err := env.manager.openRunDB(ctx, sourceRunID)
	if err != nil {
		t.Fatalf("open source run db: %v", err)
	}
	store := convergencestore.New(db)
	sourceConfig := convergence.FrozenConfiguration{
		ProfileName:    "quality",
		ModelSetupName: "balanced",
		Limits:         convergence.Limits{MaxReviewRounds: 5},
		Verification:   append([]string(nil), verification...),
		BaseRoute: convergence.Route{
			IDE:             "codex",
			Model:           "base",
			ReasoningEffort: "medium",
		},
		Review: convergence.ResolvedRoute{
			Role:    convergence.RoleReview,
			Primary: convergence.Route{IDE: "codex", Model: "review-old", ReasoningEffort: "medium"},
		},
		Correction: map[convergence.Severity]convergence.ResolvedRoute{
			convergence.SeverityCritical: {
				Role:    convergence.RoleCorrection,
				Primary: convergence.Route{IDE: "codex", Model: "correct-old", ReasoningEffort: "high"},
			},
			convergence.SeverityHigh: {
				Role:    convergence.RoleCorrection,
				Primary: convergence.Route{IDE: "codex", Model: "correct-old", ReasoningEffort: "high"},
			},
			convergence.SeverityMedium: {
				Role:    convergence.RoleCorrection,
				Primary: convergence.Route{IDE: "codex", Model: "correct-old", ReasoningEffort: "high"},
			},
			convergence.SeverityLow: {
				Role:    convergence.RoleCorrection,
				Primary: convergence.Route{IDE: "codex", Model: "correct-old", ReasoningEffort: "high"},
			},
		},
	}
	if err := store.Seed(
		ctx,
		convergenceID,
		convergence.Segment{RunID: sourceRunID, Ordinal: 1, State: convergence.SegmentPrepared},
		convergence.TargetBinding{WorkspaceID: workspace.ID, Snapshot: "snap-1"},
		sourceConfig,
	); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	applyDaemonConvergenceEvent(t, store, sourceRunID, 1, now, events.EventKindConvergenceApprovalRequested,
		kinds.ConvergenceApprovalRequestedPayload{
			ConvergenceIdentifiers: kinds.ConvergenceIdentifiers{
				RequestID:     "request-1",
				ActorID:       "fixer-1",
				ResourceID:    "proposal-1",
				CorrelationID: convergenceID,
			},
			ProposalFingerprint: "fp-1",
			Snapshot:            "snap-1",
			Action:              "weaken_verification",
			EvidenceRef:         "evidence/proposal-1.json",
			Outcome:             "pending",
		})
	applyDaemonConvergenceEvent(t, store, sourceRunID, 2, now.Add(time.Second),
		events.EventKindConvergenceSegmentParked,
		kinds.ConvergenceSegmentParkedPayload{
			ConvergenceIdentifiers: kinds.ConvergenceIdentifiers{
				RequestID:     "request-1",
				ActorID:       "runtime",
				ResourceID:    sourceRunID,
				CorrelationID: convergenceID,
			},
			Reason:          string(convergence.ParkedApprovalRequired),
			Snapshot:        "snap-1",
			UnresolvedCount: 1,
			ReceiptPath:     "convergence-receipt.json",
			ResumeAvailable: true,
			Outcome:         "parked",
		})
	if err := db.Close(); err != nil {
		t.Fatalf("close source run db: %v", err)
	}

	approval := contract.ApprovalDecisionRequest{
		ProposalID:          "proposal-1",
		Decision:            contract.ConvergenceDecisionApprove,
		Reason:              "approved after inspection",
		ExpectedFingerprint: "fp-1",
		ExpectedSnapshot:    "snap-1",
	}
	invalidApproval := approval
	invalidApproval.Reason = ""
	err = env.manager.DecideConvergenceApproval(ctx, sourceRunID, invalidApproval)
	assertConvergenceControlProblem(
		t,
		err,
		http.StatusUnprocessableEntity,
		string(contract.CodeValidationError),
	)
	if err := env.manager.DecideConvergenceApproval(ctx, sourceRunID, approval); err != nil {
		t.Fatalf("DecideConvergenceApproval() error = %v", err)
	}
	approved, err := env.manager.ConvergenceSnapshot(ctx, sourceRunID)
	if err != nil {
		t.Fatalf("ConvergenceSnapshot(approved) error = %v", err)
	}
	if len(approved.Convergence.Approvals) != 1 ||
		approved.Convergence.Approvals[0].Decision != contract.ConvergenceDecisionApprove {
		t.Fatalf("approved snapshot = %#v, want one approved proposal", approved.Convergence.Approvals)
	}
	if _, err := env.globalDB.GetRun(ctx, resumedRunID); !errors.Is(err, globaldb.ErrRunNotFound) {
		t.Fatalf("approval created continuation: err = %v", err)
	}

	staleApproval := approval
	staleApproval.ExpectedFingerprint = "fp-stale"
	err = env.manager.DecideConvergenceApproval(ctx, sourceRunID, staleApproval)
	assertConvergenceControlProblem(
		t,
		err,
		http.StatusConflict,
		convergence.CodeApprovalStale,
	)

	_, err = env.manager.ResumeConvergence(ctx, sourceRunID, contract.ConvergenceResumeRequest{
		ExpectedCursor: "resume:stale",
	})
	assertConvergenceControlProblem(
		t,
		err,
		http.StatusConflict,
		convergence.CodeResumeCursorStale,
	)

	_, err = env.manager.ResumeConvergence(ctx, sourceRunID, contract.ConvergenceResumeRequest{
		ExpectedCursor: "resume:2",
		Profile:        "missing-profile",
	})
	assertConvergenceControlProblem(
		t,
		err,
		http.StatusUnprocessableEntity,
		convergence.CodeConfigInvalid,
	)
	notClaimed, err := env.manager.ConvergenceSnapshot(ctx, sourceRunID)
	if err != nil {
		t.Fatalf("ConvergenceSnapshot(after invalid resume config) error = %v", err)
	}
	if notClaimed.Convergence.Segment.ResumeClaimed {
		t.Fatal("invalid prospective config consumed the source resume cursor")
	}

	resumed, err := env.manager.ResumeConvergence(ctx, sourceRunID, contract.ConvergenceResumeRequest{
		ExpectedCursor: "resume:2",
		Profile:        focusedProfile,
		ModelSetup:     resumeSetup,
		ReviewOverride: &contract.ConvergencePhaseRouteOverride{
			Model:           "review-new",
			ReasoningEffort: "max",
		},
		CorrectionOverride: &contract.ConvergencePhaseRouteOverride{
			IDE:   "claude",
			Model: "correct-new",
		},
	})
	if err != nil {
		t.Fatalf("ResumeConvergence() error = %v", err)
	}
	if resumed.RunID != resumedRunID || resumed.Mode != runModeConvergence {
		t.Fatalf("resumed run = %#v, want linked convergence run %q", resumed, resumedRunID)
	}
	err = env.manager.DecideConvergenceApproval(ctx, resumedRunID, approval)
	assertConvergenceControlProblem(
		t,
		err,
		http.StatusConflict,
		convergence.CodeNotParked,
	)
	if resumed.Status != runStatusParked {
		t.Fatalf("resumed run status = %q, want %q until a coordinator starts it", resumed.Status, runStatusParked)
	}
	if resumed.EndedAt == nil {
		t.Fatal("resumed placeholder has no end time, want terminal daemon lifecycle metadata")
	}
	activeRuns, err := env.globalDB.CountActiveRuns(ctx)
	if err != nil {
		t.Fatalf("CountActiveRuns() error = %v", err)
	}
	if activeRuns != 0 {
		t.Fatalf("active runs after resume = %d, want 0 without a continuation executor", activeRuns)
	}
	continuation, err := env.manager.ConvergenceSnapshot(ctx, resumedRunID)
	if err != nil {
		t.Fatalf("ConvergenceSnapshot(resumed) error = %v", err)
	}
	if continuation.Convergence.Segment.PreviousRunID != sourceRunID ||
		continuation.Convergence.ConvergenceID != convergenceID {
		t.Fatalf("continuation snapshot = %#v, want linked convergence identity", continuation.Convergence)
	}
	if continuation.Convergence.Config.Profile != "focused" ||
		continuation.Convergence.Config.ModelSetup != "resume-setup" {
		t.Fatalf("continuation config = %#v, want prospective profile and model setup", continuation.Convergence.Config)
	}
	if continuation.Convergence.Config.Limits.MaxReviewRounds != focusedMaxRounds {
		t.Fatalf("continuation max review rounds = %d, want selected profile value %d",
			continuation.Convergence.Config.Limits.MaxReviewRounds, focusedMaxRounds)
	}
	sourceAfterResume, err := env.manager.ConvergenceSnapshot(ctx, sourceRunID)
	if err != nil {
		t.Fatalf("ConvergenceSnapshot(source after resume) error = %v", err)
	}
	if sourceAfterResume.Convergence.Config.Profile != "quality" ||
		sourceAfterResume.Convergence.Config.ModelSetup != "balanced" {
		t.Fatalf("source config changed after prospective resume: %#v", sourceAfterResume.Convergence.Config)
	}

	continuationDB, err := env.manager.openRunDB(ctx, resumedRunID)
	if err != nil {
		t.Fatalf("open continuation run db: %v", err)
	}
	continuationStore := convergencestore.New(continuationDB)
	continuationDomain, err := continuationStore.Snapshot(ctx, resumedRunID)
	if err != nil {
		t.Fatalf("Snapshot(continuation config) error = %v", err)
	}
	if continuationDomain.Config.Review.Primary !=
		(convergence.Route{IDE: reviewIDE, Model: "review-new", ReasoningEffort: "max"}) {
		t.Fatalf("continuation review route = %#v, want prospective override", continuationDomain.Config.Review)
	}
	if continuationDomain.Config.Review.Sources.Model != convergence.SourceResumeOverride ||
		continuationDomain.Config.Review.Sources.ReasoningEffort != convergence.SourceResumeOverride {
		t.Fatalf("continuation review sources = %#v, want resume override provenance",
			continuationDomain.Config.Review.Sources)
	}
	for severity, route := range continuationDomain.Config.Correction {
		if route.Primary != (convergence.Route{
			IDE:             "claude",
			Model:           "correct-new",
			ReasoningEffort: correctionEffort,
		}) {
			t.Fatalf("continuation %s correction route = %#v, want prospective override", severity, route)
		}
		if route.Sources.IDE != convergence.SourceResumeOverride ||
			route.Sources.Model != convergence.SourceResumeOverride {
			t.Fatalf("continuation %s correction sources = %#v, want resume override provenance",
				severity, route.Sources)
		}
	}
	applyDaemonConvergenceEvent(
		t,
		continuationStore,
		resumedRunID,
		1,
		now.Add(2*time.Second),
		events.EventKindConvergenceSegmentCompleted,
		kinds.ConvergenceSegmentCompletedPayload{
			ConvergenceIdentifiers: kinds.ConvergenceIdentifiers{
				RequestID:     "request-2",
				ActorID:       "runtime",
				ResourceID:    resumedRunID,
				CorrelationID: convergenceID,
			},
			Snapshot:       "snap-2",
			VerificationID: "verification-2",
			ReviewID:       "review-2",
			ReceiptPath:    convergence.ReceiptFileName,
			Outcome:        string(convergence.TerminalClean),
		},
	)
	terminalSnapshot, err := continuationStore.Snapshot(ctx, resumedRunID)
	if err != nil {
		t.Fatalf("Snapshot(terminal continuation) error = %v", err)
	}
	receipt := convergence.ReceiptMetadata{
		RelativePath: convergence.ReceiptFileName,
		SourceSeq:    terminalSnapshot.LastSeq,
		Checksum:     "receipt-checksum",
	}
	if err := convergencestore.NewGlobalIndex(env.globalDB).Index(ctx, terminalSnapshot, receipt); err != nil {
		t.Fatalf("Index(terminal continuation) error = %v", err)
	}
	if err := continuationDB.Close(); err != nil {
		t.Fatalf("close continuation run db: %v", err)
	}

	_, err = env.manager.ResumeConvergence(
		ctx,
		resumedRunID,
		contract.ConvergenceResumeRequest{ExpectedCursor: "resume:unused"},
	)
	assertConvergenceControlProblem(
		t,
		err,
		http.StatusConflict,
		convergence.CodeNotParked,
	)

	replayed, err := env.manager.ResumeConvergence(ctx, sourceRunID, contract.ConvergenceResumeRequest{
		ExpectedCursor: "resume:2",
	})
	if err != nil {
		t.Fatalf("ResumeConvergence(replay terminal) error = %v", err)
	}
	if replayed.RunID != resumedRunID {
		t.Fatalf("ResumeConvergence(replay terminal).RunID = %q, want %q", replayed.RunID, resumedRunID)
	}
	indexed, ok, err := env.globalDB.ConvergenceRunIndex(ctx, resumedRunID)
	if err != nil {
		t.Fatalf("ConvergenceRunIndex(terminal continuation) error = %v", err)
	}
	if !ok {
		t.Fatal("ConvergenceRunIndex(terminal continuation) missing")
	}
	if indexed.TerminalOutcome != string(convergence.TerminalClean) ||
		indexed.ReceiptPath != receipt.RelativePath ||
		indexed.ReceiptSourceSeq != receipt.SourceSeq {
		t.Fatalf("terminal continuation index erased on replay: %#v", indexed)
	}
}

func TestResumeConvergenceRepairsClaimedContinuation(t *testing.T) {
	t.Parallel()

	const (
		sourceRunID       = "run-convergence-interrupted"
		continuationRunID = "run-convergence-repaired"
		convergenceID     = "cvg-interrupted"
		resumeCursor      = "resume:1"
	)
	now := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
	env := newRunManagerTestEnv(t, runManagerTestDeps{
		now: func() time.Time {
			return now
		},
		buildRunID: func(*model.RuntimeConfig) (string, error) {
			return continuationRunID, nil
		},
	})
	ctx := context.Background()
	workspace, err := env.globalDB.Register(ctx, env.workspaceRoot, "resume-repair-test")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := env.globalDB.PutRun(ctx, globaldb.Run{
		RunID:            sourceRunID,
		WorkspaceID:      workspace.ID,
		Mode:             runModeConvergence,
		Status:           runStatusParked,
		PresentationMode: defaultPresentationMode,
		StartedAt:        now,
	}); err != nil {
		t.Fatalf("PutRun(source) error = %v", err)
	}
	if err := reserveRunDirectory(env.manager.runArtifacts(sourceRunID).RunDir); err != nil {
		t.Fatalf("reserve source run directory: %v", err)
	}
	db, err := env.manager.openRunDB(ctx, sourceRunID)
	if err != nil {
		t.Fatalf("open source run db: %v", err)
	}
	store := convergencestore.New(db)
	target := convergence.TargetBinding{WorkspaceID: workspace.ID, Snapshot: "snap-interrupted"}
	config := convergence.FrozenConfiguration{ProfileName: "quality", ModelSetupName: "balanced"}
	if err := store.Seed(
		ctx,
		convergenceID,
		convergence.Segment{RunID: sourceRunID, Ordinal: 1, State: convergence.SegmentPrepared},
		target,
		config,
	); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	applyDaemonConvergenceEvent(t, store, sourceRunID, 1, now, events.EventKindConvergenceSegmentParked,
		kinds.ConvergenceSegmentParkedPayload{
			ConvergenceIdentifiers: kinds.ConvergenceIdentifiers{
				RequestID:     "request-interrupted",
				ActorID:       "runtime",
				ResourceID:    sourceRunID,
				CorrelationID: convergenceID,
			},
			Reason:          string(convergence.ParkedNoProgress),
			Snapshot:        target.Snapshot,
			UnresolvedCount: 1,
			ResumeAvailable: true,
			Outcome:         "parked",
		})
	snapshot, err := store.Snapshot(ctx, sourceRunID)
	if err != nil {
		t.Fatalf("Snapshot(source) error = %v", err)
	}
	segment, claimed, err := store.ClaimResume(
		ctx,
		buildResumeRequest(snapshot, resumeCursor, continuationRunID),
	)
	if err != nil {
		t.Fatalf("ClaimResume() error = %v", err)
	}
	if !claimed {
		t.Fatal("ClaimResume() replayed, want initial claim")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close source run db: %v", err)
	}

	continuationArtifacts := env.manager.runArtifacts(continuationRunID)
	if err := reserveRunDirectory(continuationArtifacts.RunDir); err != nil {
		t.Fatalf("reserve continuation run directory: %v", err)
	}
	continuationEndedAt := now
	if _, err := env.globalDB.PutRun(ctx, globaldb.Run{
		RunID:            segment.RunID,
		WorkspaceID:      workspace.ID,
		ParentRunID:      sourceRunID,
		Mode:             runModeConvergence,
		Status:           runStatusParked,
		PresentationMode: defaultPresentationMode,
		StartedAt:        now,
		EndedAt:          &continuationEndedAt,
	}); err != nil {
		t.Fatalf("PutRun(interrupted continuation) error = %v", err)
	}

	resumed, err := env.manager.ResumeConvergence(ctx, sourceRunID, contract.ConvergenceResumeRequest{
		ExpectedCursor: resumeCursor,
	})
	if err != nil {
		t.Fatalf("ResumeConvergence(replay) error = %v", err)
	}
	if resumed.RunID != continuationRunID {
		t.Fatalf("ResumeConvergence(replay).RunID = %q, want %q", resumed.RunID, continuationRunID)
	}
	repaired, err := env.manager.ConvergenceSnapshot(ctx, continuationRunID)
	if err != nil {
		t.Fatalf("ConvergenceSnapshot(repaired continuation) error = %v", err)
	}
	if repaired.Convergence.ConvergenceID != convergenceID ||
		repaired.Convergence.Segment.PreviousRunID != sourceRunID {
		t.Fatalf("repaired continuation = %#v, want linked convergence projection", repaired.Convergence)
	}
	indexed, ok, err := env.globalDB.ConvergenceRunIndex(ctx, continuationRunID)
	if err != nil {
		t.Fatalf("ConvergenceRunIndex() error = %v", err)
	}
	if !ok || indexed.ConvergenceID != convergenceID || indexed.PreviousRunID != sourceRunID {
		t.Fatalf("continuation index = %#v, %v; want repaired relation", indexed, ok)
	}
}

func applyDaemonConvergenceEvent(
	t *testing.T,
	store *convergencestore.Store,
	runID string,
	seq uint64,
	at time.Time,
	kind events.EventKind,
	payload any,
) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", kind, err)
	}
	if err := store.Apply(context.Background(), events.Event{
		SchemaVersion: events.SchemaVersion,
		RunID:         runID,
		Seq:           seq,
		Timestamp:     at,
		Kind:          kind,
		Payload:       raw,
	}); err != nil {
		t.Fatalf("store.Apply(%s) error = %v", kind, err)
	}
}

func assertConvergenceControlProblem(
	t *testing.T,
	err error,
	wantStatus int,
	wantCode string,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("control error = nil, want status %d code %q", wantStatus, wantCode)
	}
	var problem *apicore.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("control error type = %T, want *core.Problem: %v", err, err)
	}
	if problem.Status != wantStatus || problem.Code != wantCode {
		t.Fatalf(
			"control problem = status %d code %q, want status %d code %q",
			problem.Status,
			problem.Code,
			wantStatus,
			wantCode,
		)
	}
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
