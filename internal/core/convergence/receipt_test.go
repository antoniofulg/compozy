package convergence

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fixtureClock is the deterministic UTC clock the receipt tests bind to so byte
// output is reproducible, matching the _tests.md fixture 2026-07-24T12:00:00Z.
func fixtureClock() time.Time {
	return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
}

// fixtureSnapshot builds a complete, terminal convergence snapshot whose evidence
// fields deliberately mix an absolute host path (must be redacted) with a
// run-relative reference (must be retained) so redaction and evidence-retention
// are both observable in one receipt.
func fixtureSnapshot() Snapshot {
	exit := 0
	return Snapshot{
		ConvergenceID: "cvg-1",
		RequestID:     "request-1",
		Segment: Segment{
			RunID:         "run-2",
			Ordinal:       1,
			PreviousRunID: "run-1",
			SourceRunID:   "task-run-0",
			State:         SegmentPrepared,
			ResumeCursor:  "resume:42",
		},
		Target: TargetBinding{
			WorkspaceID:    "ws-1",
			ExecutionScope: "task-group",
			TaskGroupID:    "tg-1",
			Branch:         "/private/tmp/secret-checkout/feature",
			Worktree:       "/private/tmp/secret-checkout",
			Snapshot:       "sha-target",
		},
		Config: FrozenConfiguration{
			ProfileName:    "default",
			ModelSetupName: "balanced",
			Limits: Limits{
				MaxReviewRounds:         5,
				MaxFindingAttempts:      3,
				MaxVerificationAttempts: 2,
				NoProgressRounds:        2,
				ReviewAdmissionTimeout:  90 * time.Second,
				OscillationCycles:       2,
			},
			Verification:       []string{"make", "verify"},
			VerificationSource: SourceWorkspace,
			BaseRoute:          Route{IDE: "claude", Model: "opus", ReasoningEffort: "high"},
			LimitSources: ProfileSources{
				MaxReviewRounds:         SourceWorkspace,
				MaxFindingAttempts:      SourceWorkspace,
				MaxVerificationAttempts: SourceWorkspace,
				NoProgressRounds:        SourceWorkspace,
				ReviewAdmissionTimeout:  SourceWorkspace,
				OscillationCycles:       SourceWorkspace,
			},
			Review: ResolvedRoute{
				Role:    RoleReview,
				Primary: Route{IDE: "claude", Model: "reviewer", ReasoningEffort: "high"},
				Sources: RouteSources{
					IDE:             SourceSetupBase,
					Model:           SourceSetupBase,
					ReasoningEffort: SourceSetupBase,
				},
				Fallback:    &Route{IDE: "codex", Model: "reviewer-fallback", ReasoningEffort: "medium"},
				HasFallback: true,
			},
			Correction: map[Severity]ResolvedRoute{
				SeverityHigh: {
					Role:    RoleCorrection,
					Primary: Route{IDE: "claude", Model: "fixer", ReasoningEffort: "high"},
					Sources: RouteSources{
						IDE:             SourceSeverityOverride,
						Model:           SourceSeverityOverride,
						ReasoningEffort: SourceSetupBase,
					},
				},
			},
			Warnings: []string{"auto_commit is disabled"},
		},
		Routes: []RouteSelection{{
			PhaseID:             "phase-1",
			Role:                "correction",
			Primary:             "claude/fixer",
			Selected:            "codex/fixer-fallback",
			ConfigurationSource: string(SourceSeverityOverride),
			FallbackReason:      "primary unavailable at /private/tmp/runtime.sock",
		}},
		Rounds: []RoundState{{
			RoundID:    "round-1",
			Number:     1,
			AdmittedAt: fixtureClock(),
			Progress: ProgressState{
				Resolved:        true,
				NoProgressCount: 0,
			},
		}},
		Findings: []Finding{{
			Fingerprint: "fp-abc",
			State:       FindingActionable,
			Severity:    SeverityHigh,
			SnapshotSeq: 40,
			Attempts:    1,
			FirstSeq:    12,
			EvidenceRef: "evidence/finding-fp-abc.json",
		}},
		Batches: []BatchState{{
			BatchID:             "batch-1",
			PhaseID:             "phase-1",
			FindingFingerprints: []string{"fp-abc"},
			BeforeSnapshot:      "sha-0",
			AfterSnapshot:       "sha-1",
			Status:              "changed",
			AffectedPathsRef:    "/private/tmp/secret-checkout/evidence/paths.json",
		}},
		Observations: []FindingObservation{{
			ObservationID: "observation-1",
			Fingerprint:   "fp-abc",
			Snapshot:      "sha-1",
			SnapshotSeq:   40,
			Severity:      "high",
			Outcome:       "created",
			ReviewID:      "review-1",
		}},
		Dispositions: []FindingDisposition{{
			DecisionID:  "decision-1",
			Fingerprint: "fp-duplicate",
			Disposition: "duplicate",
			ActorKind:   "daemon",
			Reason:      "matches evidence at /private/tmp/secret-checkout/evidence/other.json",
			Snapshot:    "sha-1",
			SnapshotSeq: 41,
		}},
		Verification: []VerificationResult{{
			VerificationID:     "vr-1",
			PhaseID:            "phase-1",
			CommandFingerprint: "cmd-fp",
			Snapshot:           "sha-1",
			ExitCode:           &exit,
			Passed:             true,
			Attempt:            1,
			EvidencePath:       "/private/tmp/secret-checkout/.compozy/verify.log",
		}},
		Approvals: []ApprovalProposal{{
			ProposalID:  "pr-1",
			Fingerprint: "fp-abc",
			Action:      "delete_file",
			Snapshot:    "sha-1",
			Decision:    "approve",
			Reason:      "approved path /private/tmp/secret-checkout/file.go",
			EvidenceRef: "evidence/proposal-pr-1.json",
		}},
		Terminal: &TerminalOutcome{Kind: TerminalParked, Reason: ParkedApprovalRequired},
		LastSeq:  42,
	}
}

func TestBuildReceiptCompletenessAndRedaction(t *testing.T) {
	// UT-033 [happy,privacy,error]: build a complete receipt with source
	// sequence/checksum, redact forbidden fields, retain relative evidence
	// references, and detect stale/corrupt projections.
	t.Parallel()

	snap := fixtureSnapshot()

	t.Run("happy: complete receipt binds source sequence and checksum", func(t *testing.T) {
		t.Parallel()
		receipt := BuildReceipt(snap, fixtureClock())
		if receipt.SchemaVersion != ReceiptSchemaVersion {
			t.Fatalf("schema version = %q, want %q", receipt.SchemaVersion, ReceiptSchemaVersion)
		}
		if receipt.FingerprintAlgorithm != FingerprintAlgorithm {
			t.Fatalf("fingerprint algorithm = %q, want %q", receipt.FingerprintAlgorithm, FingerprintAlgorithm)
		}
		if receipt.SourceSeq != snap.LastSeq {
			t.Fatalf("source seq = %d, want %d", receipt.SourceSeq, snap.LastSeq)
		}
		if receipt.RunID != "run-2" || receipt.ConvergenceID != "cvg-1" || receipt.PreviousRunID != "run-1" {
			t.Fatalf("identities = %+v", receipt)
		}
		if receipt.RequestID != "request-1" {
			t.Fatalf("request identity = %q", receipt.RequestID)
		}
		if len(receipt.ConfiguredRoutes) != 2 || len(receipt.SelectedRoutes) != 1 ||
			len(receipt.Batches) != 1 || len(receipt.Observations) != 1 ||
			len(receipt.Dispositions) != 1 || len(receipt.Overrides) != 1 ||
			len(receipt.UnresolvedWork) != 1 {
			t.Fatalf("complete receipt sections are missing: %+v", receipt)
		}
		if !receipt.SelectedRoutes[0].FallbackUsed ||
			receipt.Overrides[0].Source != string(SourceSeverityOverride) {
			t.Fatalf("route fallback/override projection = %+v / %+v", receipt.SelectedRoutes, receipt.Overrides)
		}
		if receipt.Commit.State != "uncommitted" || receipt.Commit.AutoCommitEnabled {
			t.Fatalf("commit projection = %+v", receipt.Commit)
		}
		if receipt.Terminal.Kind != string(TerminalParked) ||
			receipt.Terminal.Reason != string(ParkedApprovalRequired) ||
			!receipt.Terminal.ResumeAvailable {
			t.Fatalf("terminal projection = %+v", receipt.Terminal)
		}
		if receipt.Progress.ResumeCursor != "resume:42" || receipt.Progress.UnresolvedCount != 1 {
			t.Fatalf("progress projection = %+v", receipt.Progress)
		}
		if err := receipt.Validate(); err != nil {
			t.Fatalf("Validate() on a fresh receipt: %v", err)
		}
	})

	t.Run("privacy: absolute paths are redacted, relative evidence retained", func(t *testing.T) {
		t.Parallel()
		receipt := BuildReceipt(snap, fixtureClock())
		data, err := receipt.Marshal()
		if err != nil {
			t.Fatalf("Marshal(): %v", err)
		}
		body := string(data)
		if strings.Contains(body, "/private/tmp/secret-checkout") {
			t.Fatalf("receipt leaked an absolute host path:\n%s", body)
		}
		if !strings.Contains(body, "[redacted-path]") {
			t.Fatalf("expected a redacted-path marker in receipt:\n%s", body)
		}
		if receipt.Findings[0].EvidenceRef != "evidence/finding-fp-abc.json" {
			t.Fatalf("relative evidence ref was not retained: %q", receipt.Findings[0].EvidenceRef)
		}
		if receipt.Approvals[0].EvidenceRef != "evidence/proposal-pr-1.json" {
			t.Fatalf("relative approval evidence ref was not retained: %q", receipt.Approvals[0].EvidenceRef)
		}
	})

	t.Run("error: a tampered checksum is detected as corrupt", func(t *testing.T) {
		t.Parallel()
		receipt := BuildReceipt(snap, fixtureClock())
		receipt.Findings[0].State = string(FindingResolved) // silent content edit
		if err := receipt.Validate(); err == nil {
			t.Fatal("Validate() accepted a tampered receipt, want ErrReceiptCorrupt")
		} else if !errors.Is(err, ErrReceiptCorrupt) {
			t.Fatalf("Validate() error = %v, want ErrReceiptCorrupt", err)
		}
	})

	t.Run("error: malformed JSON parses as corrupt", func(t *testing.T) {
		t.Parallel()
		if _, err := ParseReceipt([]byte("{not-json")); !errors.Is(err, ErrReceiptCorrupt) {
			t.Fatalf("ParseReceipt(malformed) error = %v, want ErrReceiptCorrupt", err)
		}
	})

	t.Run("error: an empty checksum is corrupt", func(t *testing.T) {
		t.Parallel()
		receipt := BuildReceipt(snap, fixtureClock())
		receipt.Checksum = ""
		if err := receipt.Validate(); !errors.Is(err, ErrReceiptCorrupt) {
			t.Fatalf("Validate() empty checksum error = %v, want ErrReceiptCorrupt", err)
		}
	})
}

func TestReceiptSerializationIsByteStableAndRebuildsSameChecksum(t *testing.T) {
	// UT-045 [happy,replay]: canonical serialization and receipt generation remain
	// byte-stable for the fixed seed/clock and rebuild to the same checksum from
	// canonical events.
	t.Parallel()

	snap := fixtureSnapshot()

	t.Run("happy: identical snapshot and clock produce identical bytes", func(t *testing.T) {
		t.Parallel()
		first, err := BuildReceipt(snap, fixtureClock()).Marshal()
		if err != nil {
			t.Fatalf("Marshal(first): %v", err)
		}
		second, err := BuildReceipt(snap, fixtureClock()).Marshal()
		if err != nil {
			t.Fatalf("Marshal(second): %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("receipt bytes are not byte-stable:\nfirst:  %s\nsecond: %s", first, second)
		}
		if !strings.HasSuffix(string(first), "\n") {
			t.Fatal("receipt bytes must end with a trailing newline")
		}
	})

	t.Run("replay: checksum is independent of generated_at", func(t *testing.T) {
		t.Parallel()
		early := BuildReceipt(snap, fixtureClock())
		later := BuildReceipt(snap, fixtureClock().Add(48*time.Hour))
		if early.Checksum != later.Checksum {
			t.Fatalf("checksum changed with clock: %q vs %q", early.Checksum, later.Checksum)
		}
		if early.GeneratedAt.Equal(later.GeneratedAt) {
			t.Fatal("generated_at should differ between the two builds")
		}
		// The only byte difference must be the generated_at field.
		earlyBytes := mustReceiptJSONWithoutGeneratedAt(t, early)
		laterBytes := mustReceiptJSONWithoutGeneratedAt(t, later)
		if earlyBytes != laterBytes {
			t.Fatalf("receipt content differs beyond generated_at:\n%s\n%s", earlyBytes, laterBytes)
		}
	})
}

func mustReceiptJSONWithoutGeneratedAt(t *testing.T, r Receipt) string {
	t.Helper()
	r.GeneratedAt = time.Time{}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	return string(data)
}
