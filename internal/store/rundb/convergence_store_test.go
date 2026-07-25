package rundb

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/compozy/compozy/pkg/compozy/events"
	"github.com/compozy/compozy/pkg/compozy/events/kinds"
)

func TestConvergenceSchemaAndProjectionRoundTrip(t *testing.T) {
	// IT-002: the additive schema preserves a complete frozen run and every
	// convergence event/projection family across a real SQLite reopen.
	t.Parallel()

	ctx := context.Background()
	db := openTestRunDB(t, "run-cvg-roundtrip")
	path := db.Path()
	seed := ConvergenceRunSeed{
		ConvergenceID:  "cvg-1",
		RunID:          "run-cvg-roundtrip",
		Ordinal:        0,
		SourceRunID:    "task-run-1",
		State:          "prepared",
		WorkspaceID:    "ws-1",
		ExecutionScope: "task-group",
		TaskGroupID:    "TG-002",
		Branch:         "feature/convergence",
		Worktree:       "worktrees/TG-002",
		TargetSnapshot: "sha-1",
		ConfigJSON:     `{"profile_name":"default"}`,
	}
	if err := db.SeedConvergenceRun(ctx, seed); err != nil {
		t.Fatalf("SeedConvergenceRun() = %v", err)
	}

	base := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	preflight := convergenceTestEvent(t, seed.RunID, 1, base,
		events.EventKindConvergencePreflightCompleted,
		kinds.ConvergencePreflightCompletedPayload{
			ConvergenceIdentifiers: testConvergenceIDs(seed.ConvergenceID, seed.RunID),
			TargetSnapshot:         "sha-1",
			ConfigFingerprint:      "config-fp",
			RouteSummary:           "review=claude correction=codex",
			Warnings:               []string{"auto_commit disabled"},
			Outcome:                "accepted",
		})
	phase := convergenceTestEvent(t, seed.RunID, 2, base.Add(time.Second),
		events.EventKindConvergencePhaseStarted,
		kinds.ConvergencePhaseStartedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("phase-1", seed.ConvergenceID),
			Phase:                  "initial_verification",
			Round:                  1,
			Attempt:                1,
			Snapshot:               "sha-1",
			Outcome:                "started",
		})
	route := convergenceTestEvent(t, seed.RunID, 3, base.Add(2*time.Second),
		events.EventKindConvergenceRouteSelected,
		kinds.ConvergenceRouteSelectedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("phase-1", seed.ConvergenceID),
			Role:                   "review",
			Primary:                "claude/reviewer",
			Selected:               "claude/reviewer",
			ConfigurationSource:    "setup-base",
			Outcome:                "primary",
		})
	verification := convergenceTestEvent(t, seed.RunID, 4, base.Add(3*time.Second),
		events.EventKindConvergenceVerificationComplete,
		kinds.ConvergenceVerificationCompletedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("verify-1", "phase-1"),
			CommandFingerprint:     "cmd-fp",
			Snapshot:               "sha-1",
			ExitCode:               intPointer(0),
			EvidencePath:           "evidence/verify-1.log",
			Outcome:                "passed",
		})
	review := convergenceTestEvent(t, seed.RunID, 5, base.Add(4*time.Second),
		events.EventKindConvergenceReviewCompleted,
		kinds.ConvergenceReviewCompletedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("review-1", "phase-1"),
			Snapshot:               "sha-1",
			FindingCount:           1,
			ArtifactPath:           "evidence/review-1.json",
			ReadOnlyEnforced:       true,
			Outcome:                "findings",
		})
	finding := convergenceTestEvent(t, seed.RunID, 6, base.Add(5*time.Second),
		events.EventKindConvergenceFindingChanged,
		kinds.ConvergenceFindingChangedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("finding-fp", "review-1"),
			State:                  "actionable",
			Severity:               "high",
			Snapshot:               "sha-1",
			EvidenceRef:            "evidence/finding.json",
			Outcome:                "created",
		})
	batch := convergenceTestEvent(t, seed.RunID, 7, base.Add(6*time.Second),
		events.EventKindConvergenceBatchCompleted,
		kinds.ConvergenceBatchCompletedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("batch-1", "phase-1"),
			FindingFingerprints:    []string{"finding-fp"},
			BeforeSnapshot:         "sha-1",
			AfterSnapshot:          "sha-2",
			AffectedPathsRef:       "evidence/batch-1-paths.json",
			Outcome:                "changed",
		})
	disposition := convergenceTestEvent(t, seed.RunID, 8, base.Add(7*time.Second),
		events.EventKindConvergenceFindingChanged,
		kinds.ConvergenceFindingChangedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("finding-duplicate", "review-1"),
			State:                  "duplicate",
			Severity:               "medium",
			Snapshot:               "sha-2",
			EvidenceRef:            "evidence/duplicate.json",
			DispositionReason:      "same semantic identity",
			Outcome:                "duplicate",
		})
	progress := convergenceTestEvent(t, seed.RunID, 9, base.Add(8*time.Second),
		events.EventKindConvergenceProgressEvaluated,
		kinds.ConvergenceProgressEvaluatedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("round-1", seed.ConvergenceID),
			Resolved:               false,
			SeverityDecreased:      true,
			VerificationImproved:   true,
			NoProgressCount:        0,
			OscillationCount:       0,
			Outcome:                "progress",
		})
	approval := convergenceTestEvent(t, seed.RunID, 10, base.Add(9*time.Second),
		events.EventKindConvergenceApprovalRequested,
		kinds.ConvergenceApprovalRequestedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("proposal-1", seed.ConvergenceID),
			ProposalFingerprint:    "proposal-fp",
			Snapshot:               "sha-1",
			Action:                 "protected-change",
			EvidenceRef:            "evidence/proposal.json",
			Outcome:                "requested",
		})
	approvalDecision := convergenceTestEvent(t, seed.RunID, 11, base.Add(10*time.Second),
		events.EventKindConvergenceApprovalDecided,
		kinds.ConvergenceApprovalDecidedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("proposal-1", seed.ConvergenceID),
			ProposalFingerprint:    "proposal-fp",
			Snapshot:               "sha-1",
			Decision:               "approve",
			Reason:                 "authorized",
			Outcome:                "approved",
		})
	parked := convergenceTestEvent(t, seed.RunID, 12, base.Add(11*time.Second),
		events.EventKindConvergenceSegmentParked,
		kinds.ConvergenceSegmentParkedPayload{
			ConvergenceIdentifiers: testConvergenceIDs(seed.RunID, seed.ConvergenceID),
			Reason:                 "approval_required",
			Snapshot:               "sha-2",
			UnresolvedCount:        1,
			ReceiptPath:            ReceiptFileNameForTest,
			ResumeAvailable:        true,
			Outcome:                "parked",
		})
	for _, event := range []events.Event{
		preflight,
		phase,
		route,
		verification,
		review,
		finding,
		batch,
		disposition,
		progress,
		approval,
		approvalDecision,
		parked,
	} {
		if err := db.ApplyConvergenceEvent(ctx, event); err != nil {
			t.Fatalf("ApplyConvergenceEvent(%s) = %v", event.Kind, err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(reopen) = %v", err)
	}
	defer func() { _ = reopened.Close() }()

	run, ok, err := reopened.ConvergenceRun(ctx, seed.ConvergenceID)
	if err != nil || !ok {
		t.Fatalf("ConvergenceRun() = %+v, %t, %v", run, ok, err)
	}
	if run.RunID != seed.RunID || run.RequestID != "request-1" ||
		run.ConfigJSON != seed.ConfigJSON || run.TargetSnapshot != "sha-1" {
		t.Fatalf("run projection = %+v", run)
	}
	segment, err := reopened.ConvergenceSegment(ctx, seed.RunID)
	if err != nil || segment.TerminalKind != "parked" || segment.TerminalSeq != 12 {
		t.Fatalf("ConvergenceSegment() = %+v, %v", segment, err)
	}
	latestPhase, ok, err := reopened.ConvergenceLatestPhase(ctx, seed.RunID)
	if err != nil || !ok || latestPhase.PhaseID != "phase-1" || latestPhase.Attempt != 1 {
		t.Fatalf("ConvergenceLatestPhase() = %+v, %t, %v", latestPhase, ok, err)
	}
	rounds, err := reopened.ConvergenceRounds(ctx, seed.RunID)
	if err != nil || len(rounds) != 1 || rounds[0].RoundID != "round-1" {
		t.Fatalf("ConvergenceRounds() = %+v, %v", rounds, err)
	}
	findings, err := reopened.ConvergenceFindings(ctx, seed.RunID)
	if err != nil || len(findings) != 1 || findings[0].Fingerprint != "finding-fp" {
		t.Fatalf("ConvergenceFindings() = %+v, %v", findings, err)
	}
	verifications, err := reopened.ConvergenceVerifications(ctx, seed.RunID)
	if err != nil || len(verifications) != 1 || !verifications[0].Passed ||
		verifications[0].Attempt != 1 {
		t.Fatalf("ConvergenceVerifications() = %+v, %v", verifications, err)
	}
	approvals, err := reopened.ConvergenceApprovals(ctx, seed.RunID)
	if err != nil || len(approvals) != 1 || approvals[0].Decision != "approve" {
		t.Fatalf("ConvergenceApprovals() = %+v, %v", approvals, err)
	}
	routes, err := reopened.ConvergenceRoutes(ctx, seed.RunID)
	if err != nil || len(routes) != 1 || routes[0].Selected != "claude/reviewer" {
		t.Fatalf("ConvergenceRoutes() = %+v, %v", routes, err)
	}
	batches, err := reopened.ConvergenceBatches(ctx, seed.RunID)
	if err != nil || len(batches) != 1 ||
		!reflect.DeepEqual(batches[0].FindingFingerprints, []string{"finding-fp"}) {
		t.Fatalf("ConvergenceBatches() = %+v, %v", batches, err)
	}
	observations, err := reopened.ConvergenceObservations(ctx, seed.RunID)
	if err != nil || len(observations) != 1 || observations[0].Snapshot != "sha-1" {
		t.Fatalf("ConvergenceObservations() = %+v, %v", observations, err)
	}
	dispositions, err := reopened.ConvergenceDispositions(ctx, seed.RunID)
	if err != nil || len(dispositions) != 1 || dispositions[0].Snapshot != "sha-2" {
		t.Fatalf("ConvergenceDispositions() = %+v, %v", dispositions, err)
	}

	completedDB := openTestRunDB(t, "run-cvg-completed")
	defer func() { _ = completedDB.Close() }()
	seedConvergenceTestRun(t, completedDB, "cvg-completed", "run-cvg-completed")
	completed := convergenceTestEvent(t, "run-cvg-completed", 1, base,
		events.EventKindConvergenceSegmentCompleted,
		kinds.ConvergenceSegmentCompletedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("run-cvg-completed", "cvg-completed"),
			Snapshot:               "sha-clean",
			VerificationID:         "verify-clean",
			ReviewID:               "review-clean",
			ReceiptPath:            ReceiptFileNameForTest,
			Outcome:                "clean",
		})
	if err := completedDB.ApplyConvergenceEvent(ctx, completed); err != nil {
		t.Fatalf("ApplyConvergenceEvent(completed) = %v", err)
	}
	completedSegment, err := completedDB.ConvergenceSegment(ctx, "run-cvg-completed")
	if err != nil || completedSegment.TerminalKind != "clean" {
		t.Fatalf("completed segment = %+v, %v", completedSegment, err)
	}
}

func TestApplyConvergenceEventRollsBackEventAndProjectionTogether(t *testing.T) {
	// IT-008: a real SQLite projection failure aborts the transaction before the
	// canonical event or any projection can advance; retrying the same sequence
	// after the injected failure succeeds.
	t.Parallel()

	ctx := context.Background()
	db := openTestRunDB(t, "run-cvg-rollback")
	defer func() { _ = db.Close() }()
	seedConvergenceTestRun(t, db, "cvg-rollback", "run-cvg-rollback")

	if _, err := db.db.ExecContext(ctx, `CREATE TABLE injected_commit_parent (
		id INTEGER PRIMARY KEY
	)`); err != nil {
		t.Fatalf("create commit parent = %v", err)
	}
	if _, err := db.db.ExecContext(ctx, `CREATE TABLE injected_commit_guard (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER NOT NULL,
		FOREIGN KEY(parent_id) REFERENCES injected_commit_parent(id)
			DEFERRABLE INITIALLY DEFERRED
	)`); err != nil {
		t.Fatalf("create commit guard = %v", err)
	}
	if _, err := db.db.ExecContext(ctx, `CREATE TRIGGER inject_phase_failure
		AFTER INSERT ON convergence_phases
		BEGIN
			INSERT INTO injected_commit_guard (id, parent_id) VALUES (1, 999);
		END;`); err != nil {
		t.Fatalf("create failure trigger = %v", err)
	}
	event := convergenceTestEvent(t, "run-cvg-rollback", 1, fixtureConvergenceTime(),
		events.EventKindConvergencePhaseStarted,
		kinds.ConvergencePhaseStartedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("phase-fail", "cvg-rollback"),
			Phase:                  "initial_verification",
			Attempt:                1,
			Snapshot:               "sha-1",
			Outcome:                "started",
		})
	if err := db.ApplyConvergenceEvent(ctx, event); err == nil {
		t.Fatal("ApplyConvergenceEvent() error = nil")
	}
	assertConvergenceEventCount(t, db, 0)
	if _, ok, err := db.ConvergenceLatestPhase(ctx, "run-cvg-rollback"); err != nil || ok {
		t.Fatalf("phase after rollback = ok:%t err:%v", ok, err)
	}
	if _, err := db.db.ExecContext(ctx, `DROP TRIGGER inject_phase_failure`); err != nil {
		t.Fatalf("drop failure trigger = %v", err)
	}
	if err := db.ApplyConvergenceEvent(ctx, event); err != nil {
		t.Fatalf("ApplyConvergenceEvent(retry) = %v", err)
	}
	assertConvergenceEventCount(t, db, 1)
}

func TestApplyConvergenceEventReplayAndTerminalGuards(t *testing.T) {
	// UT-035 and IT-010: exact at-least-once replay is a no-op; conflicting duplicate
	// sequences, out-of-order appends, stale resource identities, and post-terminal
	// events are rejected without advancing canonical state.
	t.Parallel()

	ctx := context.Background()
	db := openTestRunDB(t, "run-cvg-guards")
	defer func() { _ = db.Close() }()
	seedConvergenceTestRun(t, db, "cvg-guards", "run-cvg-guards")

	phase := convergenceTestEvent(t, "run-cvg-guards", 2, fixtureConvergenceTime(),
		events.EventKindConvergencePhaseStarted,
		kinds.ConvergencePhaseStartedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("phase-1", "cvg-guards"),
			Phase:                  "initial_verification",
			Attempt:                1,
			Snapshot:               "sha-1",
			Outcome:                "started",
		})
	if err := db.ApplyConvergenceEvent(ctx, phase); err != nil {
		t.Fatalf("ApplyConvergenceEvent(first) = %v", err)
	}
	if err := db.StoreEventBatch(ctx, []events.Event{phase}); err != nil {
		t.Fatalf("StoreEventBatch(exact replay) = %v", err)
	}
	assertConvergenceEventCount(t, db, 1)

	conflicting := phase
	conflicting.Payload = json.RawMessage(`{"different":true}`)
	if err := db.StoreEventBatch(ctx, []events.Event{conflicting}); !errors.Is(err, ErrConvergenceReplay) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}
	outOfOrder := phase
	outOfOrder.Seq = 1
	if err := db.StoreEventBatch(ctx, []events.Event{outOfOrder}); !errors.Is(err, ErrConvergenceReplay) {
		t.Fatalf("out-of-order error = %v", err)
	}
	staleIdentity := convergenceTestEvent(t, "run-cvg-guards", 3, fixtureConvergenceTime().Add(time.Second),
		events.EventKindConvergencePhaseStarted,
		kinds.ConvergencePhaseStartedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("phase-stale", "different-convergence"),
			Phase:                  "review",
			Round:                  1,
			Attempt:                1,
			Snapshot:               "sha-1",
			Outcome:                "started",
		})
	if err := db.StoreEventBatch(ctx, []events.Event{staleIdentity}); !errors.Is(err, ErrConvergenceReplay) {
		t.Fatalf("stale convergence identity error = %v", err)
	}

	staleTerminal := convergenceTestEvent(t, "run-cvg-guards", 3, fixtureConvergenceTime().Add(time.Second),
		events.EventKindConvergenceSegmentCompleted,
		kinds.ConvergenceSegmentCompletedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("different-run", "cvg-guards"),
			Snapshot:               "sha-1",
			VerificationID:         "verify-1",
			ReviewID:               "review-1",
			ReceiptPath:            ReceiptFileNameForTest,
			Outcome:                "clean",
		})
	if err := db.ApplyConvergenceEvent(ctx, staleTerminal); !errors.Is(err, ErrConvergenceReplay) {
		t.Fatalf("stale terminal error = %v", err)
	}
	assertConvergenceEventCount(t, db, 1)

	terminal := staleTerminal
	terminal.Payload = mustJSON(t, kinds.ConvergenceSegmentCompletedPayload{
		ConvergenceIdentifiers: testConvergenceIDs("run-cvg-guards", "cvg-guards"),
		Snapshot:               "sha-1",
		VerificationID:         "verify-1",
		ReviewID:               "review-1",
		ReceiptPath:            ReceiptFileNameForTest,
		Outcome:                "clean",
	})
	if err := db.ApplyConvergenceEvent(ctx, terminal); err != nil {
		t.Fatalf("ApplyConvergenceEvent(terminal) = %v", err)
	}
	postTerminal := phase
	postTerminal.Seq = 4
	postTerminal.Timestamp = fixtureConvergenceTime().Add(2 * time.Second)
	if err := db.ApplyConvergenceEvent(ctx, postTerminal); !errors.Is(err, ErrConvergenceReplay) {
		t.Fatalf("post-terminal error = %v", err)
	}
	assertConvergenceEventCount(t, db, 2)
}

func TestClaimConvergenceResumeClaimsOnceAndPreservesHistory(t *testing.T) {
	// Resume persistence boundary: only a parked segment may resume, the observed cursor and
	// convergence identity must still match, and replay returns the one linked
	// segment without mutating consumed history.
	t.Parallel()

	ctx := context.Background()
	db := openTestRunDB(t, "run-cvg-parked")
	defer func() { _ = db.Close() }()
	seedConvergenceTestRun(t, db, "cvg-resume", "run-cvg-parked")
	parked := convergenceTestEvent(t, "run-cvg-parked", 1, fixtureConvergenceTime(),
		events.EventKindConvergenceSegmentParked,
		kinds.ConvergenceSegmentParkedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("run-cvg-parked", "cvg-resume"),
			Reason:                 "approval_required",
			Snapshot:               "sha-1",
			UnresolvedCount:        1,
			ReceiptPath:            ReceiptFileNameForTest,
			ResumeAvailable:        true,
			Outcome:                "parked",
		})
	if err := db.ApplyConvergenceEvent(ctx, parked); err != nil {
		t.Fatalf("ApplyConvergenceEvent(parked) = %v", err)
	}

	segment, claimed, err := db.ClaimConvergenceResume(
		ctx,
		"cvg-resume",
		"run-cvg-parked",
		"resume:1",
		"run-cvg-resumed",
	)
	if err != nil || !claimed {
		t.Fatalf("ClaimConvergenceResume() = %+v, %t, %v", segment, claimed, err)
	}
	if segment.Ordinal != 1 || segment.PreviousRunID != "run-cvg-parked" ||
		segment.ConvergenceID != "cvg-resume" {
		t.Fatalf("resumed segment = %+v", segment)
	}
	replayed, claimed, err := db.ClaimConvergenceResume(
		ctx,
		"cvg-resume",
		"run-cvg-parked",
		"resume:1",
		"ignored-new-run",
	)
	if err != nil || claimed || !reflect.DeepEqual(replayed, segment) {
		t.Fatalf("replay = %+v, %t, %v; want %+v", replayed, claimed, err, segment)
	}
	if _, _, err := db.ClaimConvergenceResume(
		ctx,
		"wrong-convergence",
		"run-cvg-parked",
		"resume:1",
		"run-wrong",
	); !errors.Is(err, ErrConvergenceReplay) {
		t.Fatalf("wrong convergence error = %v", err)
	}
	previous, err := db.ConvergenceSegment(ctx, "run-cvg-parked")
	if err != nil {
		t.Fatalf("ConvergenceSegment(previous) = %v", err)
	}
	if previous.TerminalKind != "parked" || previous.TerminalSeq != 1 || !previous.ResumeClaimed {
		t.Fatalf("previous history mutated incorrectly: %+v", previous)
	}
}

func TestClaimConvergenceResumeConcurrentLoserReplaysWinner(t *testing.T) {
	// Single-claim invariant: racing requests create one linked segment and the
	// loser deterministically replays it.
	t.Parallel()

	ctx := context.Background()
	db := openTestRunDB(t, "run-cvg-race")
	defer func() { _ = db.Close() }()
	seedConvergenceTestRun(t, db, "cvg-race", "run-cvg-race")
	parked := convergenceTestEvent(t, "run-cvg-race", 1, fixtureConvergenceTime(),
		events.EventKindConvergenceSegmentParked,
		kinds.ConvergenceSegmentParkedPayload{
			ConvergenceIdentifiers: testConvergenceIDs("run-cvg-race", "cvg-race"),
			Reason:                 "approval_required",
			Snapshot:               "sha-1",
			UnresolvedCount:        1,
			ReceiptPath:            ReceiptFileNameForTest,
			ResumeAvailable:        true,
			Outcome:                "parked",
		})
	if err := db.ApplyConvergenceEvent(ctx, parked); err != nil {
		t.Fatalf("ApplyConvergenceEvent(parked) = %v", err)
	}

	type result struct {
		segment ConvergenceSegmentRow
		claimed bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for _, newRunID := range []string{"run-race-a", "run-race-b"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			segment, claimed, err := db.ClaimConvergenceResume(
				ctx,
				"cvg-race",
				"run-cvg-race",
				"resume:1",
				newRunID,
			)
			results <- result{segment: segment, claimed: claimed, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var (
		winner     ConvergenceSegmentRow
		claimed    int
		allResults []result
	)
	for item := range results {
		if item.err != nil {
			t.Fatalf("ClaimConvergenceResume(race) = %v", item.err)
		}
		if item.claimed {
			claimed++
			winner = item.segment
		}
		allResults = append(allResults, item)
	}
	if claimed != 1 {
		t.Fatalf("claimed results = %d, want 1: %+v", claimed, allResults)
	}
	for _, item := range allResults {
		if !reflect.DeepEqual(item.segment, winner) {
			t.Fatalf("race result = %+v, winner = %+v", item.segment, winner)
		}
	}
}

func TestTerminalConvergenceSegmentsReplayAfterReopenWithoutMutation(t *testing.T) {
	// IT-032: clean, parked, failed, and canceled segment results remain readable
	// after restart; repeated terminal requests are reads and add no canonical
	// event, projection, or side effect.
	t.Parallel()

	cases := []struct {
		name   string
		kind   string
		reason string
		event  events.EventKind
	}{
		{name: "clean", kind: "clean", event: events.EventKindRunCompleted},
		{name: "parked", kind: "parked", reason: "approval_required", event: events.EventKindRunParked},
		{name: "failed", kind: "failed", event: events.EventKindRunFailed},
		{name: "canceled", kind: "canceled", event: events.EventKindRunCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			runID := "run-terminal-" + tc.name
			db := openTestRunDB(t, runID)
			path := db.Path()
			seedConvergenceTestRun(t, db, "cvg-"+tc.name, runID)
			if _, err := db.db.ExecContext(ctx,
				`UPDATE convergence_segments
				 SET state='terminal', terminal_kind=?, terminal_reason=?, terminal_seq=1
				 WHERE run_id=?`,
				tc.kind,
				tc.reason,
				runID,
			); err != nil {
				t.Fatalf("seed terminal projection = %v", err)
			}
			if err := db.StoreEventBatch(ctx, []events.Event{
				convergenceTestEvent(t, runID, 1, fixtureConvergenceTime(), tc.event, map[string]any{
					"outcome": tc.kind,
				}),
			}); err != nil {
				t.Fatalf("StoreEventBatch(terminal) = %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("Close() = %v", err)
			}
			reopened, err := Open(ctx, path)
			if err != nil {
				t.Fatalf("Open(reopen) = %v", err)
			}
			defer func() { _ = reopened.Close() }()

			for request := 0; request < 3; request++ {
				segment, err := reopened.ConvergenceSegment(ctx, runID)
				if err != nil {
					t.Fatalf("ConvergenceSegment(request %d) = %v", request, err)
				}
				if segment.TerminalKind != tc.kind || segment.TerminalReason != tc.reason {
					t.Fatalf("terminal replay = %+v", segment)
				}
			}
			assertConvergenceEventCount(t, reopened, 1)
		})
	}
}

const ReceiptFileNameForTest = "convergence-receipt.json"

func seedConvergenceTestRun(t *testing.T, db *RunDB, convergenceID, runID string) {
	t.Helper()
	if err := db.SeedConvergenceRun(context.Background(), ConvergenceRunSeed{
		ConvergenceID:  convergenceID,
		RunID:          runID,
		State:          "prepared",
		WorkspaceID:    "ws-test",
		ExecutionScope: "task-group",
		TargetSnapshot: "sha-1",
		ConfigJSON:     `{}`,
	}); err != nil {
		t.Fatalf("SeedConvergenceRun() = %v", err)
	}
}

func convergenceTestEvent(
	t *testing.T,
	runID string,
	seq uint64,
	timestamp time.Time,
	kind events.EventKind,
	payload any,
) events.Event {
	t.Helper()
	return mustEvent(t, runID, seq, timestamp, kind, payload)
}

func testConvergenceIDs(resourceID, correlationID string) kinds.ConvergenceIdentifiers {
	return kinds.ConvergenceIdentifiers{
		RequestID:     "request-1",
		ActorID:       "actor-1",
		ResourceID:    resourceID,
		CorrelationID: correlationID,
	}
}

func intPointer(value int) *int {
	return &value
}

func fixtureConvergenceTime() time.Time {
	return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
}

func assertConvergenceEventCount(t *testing.T, db *RunDB, want int) {
	t.Helper()
	result, err := db.ListEvents(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("ListEvents() = %v", err)
	}
	if len(result.Events) != want {
		t.Fatalf("event count = %d, want %d", len(result.Events), want)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() = %v", err)
	}
	return raw
}
