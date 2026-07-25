package convergence

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/compozy/compozy/internal/core/convergence/verification"
	"github.com/compozy/compozy/internal/core/worktree"
)

func fixedClock() func() time.Time {
	instant := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return instant }
}

// initGitRepo initializes a deterministic temporary Git repository with one
// committed file and returns its root. It provides the real snapshot evidence the
// recovery and verification tests bind to.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("integration fixtures require a POSIX shell and git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for convergence integration fixtures")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "convergence@example.com")
	runGit(t, root, "config", "user.name", "Convergence Test")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-07-24T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-07-24T12:00:00Z",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
}

func captureDigest(t *testing.T, root string) string {
	t.Helper()
	snapshot, err := worktree.Capture(context.Background(), root)
	if err != nil {
		t.Fatalf("worktree.Capture() = %v", err)
	}
	if !snapshot.IsSupported() {
		t.Fatalf("snapshot unsupported: %s", snapshot.UnsupportedReason())
	}
	return snapshot.Digest()
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

// runVerification executes one authoritative verification against the worktree
// with the real runner and returns the result.
func runVerification(t *testing.T, root, command string, attempt int) verification.Result {
	t.Helper()
	snapshot := captureDigest(t, root)
	req := verification.Request{
		VerificationID:   "ver",
		PhaseID:          "phase",
		Attempt:          attempt,
		WorktreeRoot:     root,
		Command:          []string{"/bin/sh", command},
		ExpectedSnapshot: snapshot,
		EvidenceDir:      filepath.Join(t.TempDir(), "evidence"),
		SummaryByteLimit: 512,
	}
	result, err := verification.NewRunner().Run(context.Background(), req)
	if err != nil {
		t.Fatalf("verification Run() = %v", err)
	}
	return result
}

func TestIT015InitialVerificationCorrectionLoop(t *testing.T) {
	// IT-015: an initial verification failure becomes correction work; review is
	// admitted only after verification passes, and default exhaustion parks with
	// exact verification-failed evidence.
	t.Parallel()
	root := initGitRepo(t)
	scripts := t.TempDir()
	failScript := writeScript(t, scripts, "fail.sh", "#!/bin/sh\necho 'gate failed' 1>&2\nexit 1\n")
	passScript := writeScript(t, scripts, "pass.sh", "#!/bin/sh\necho 'gate passed'\nexit 0\n")
	snapshot := captureDigest(t, root)

	t.Run("passing verification after correction admits review", func(t *testing.T) {
		t.Parallel()
		first := runVerification(t, root, failScript, 1)
		if verification.Authorize(first, snapshot).Authorized {
			t.Fatal("failing verification must not authorize progress")
		}
		if !CanTransition(PhaseInitialVerification, PhasePreReviewCorrection) {
			t.Fatal("a verification failure must transition to pre-review correction")
		}
		// Correction repairs the baseline; re-verification passes.
		second := runVerification(t, root, passScript, 2)
		if !verification.Authorize(second, snapshot).Authorized {
			t.Fatal("passing verification on the unchanged snapshot must authorize progress")
		}
		if !CanTransition(PhaseInitialVerification, PhaseReview) {
			t.Fatal("review must be admitted only after verification passes")
		}
	})

	t.Run("three failed default attempts park verification_failed", func(t *testing.T) {
		t.Parallel()
		ledger := NewAttemptLedger(3)
		var key string
		reviewAdmitted := false
		for attempt := 1; attempt <= 3; attempt++ {
			result := runVerification(t, root, failScript, attempt)
			if verification.Authorize(result, snapshot).Authorized {
				reviewAdmitted = true
				break
			}
			key = result.FailureFingerprint
			ledger.Record(key, result.VerificationID+strconv.Itoa(attempt))
		}
		if reviewAdmitted {
			t.Fatal("review must never be admitted while verification keeps failing")
		}
		if !ledger.Exhausted(key) {
			t.Fatalf("three failed attempts must exhaust the default limit, count=%d", ledger.Count(key))
		}
		decision := EvaluateTerminal(EvaluationInput{VerificationAttemptsExhausted: true})
		if !decision.Terminal || decision.Outcome.Kind != TerminalParked ||
			decision.Outcome.Reason != ParkedVerificationFailed {
			t.Fatalf("exhaustion must park verification_failed, got %+v", decision.Outcome)
		}
	})
}

func TestIT016DaemonOwnsReviewArtifact(t *testing.T) {
	// IT-016: the daemon writer, not the reviewer session, creates the checksummed
	// round artifact, and every finding observation matches its snapshot and
	// fingerprints. A review bound to a superseded snapshot is rejected as stale.
	t.Parallel()
	dir := t.TempDir()
	snapshot := "snap-daemon"
	result := ReviewResult{
		ReviewID:    "rev-it016",
		Snapshot:    snapshot,
		SnapshotSeq: 4,
		Outcome:     ReviewOutcomeFindings,
		Explanation: "one actionable finding",
		Findings:    []ReviewedFinding{validReviewedFinding("pkg/a.go", "pkg.A", SeverityHigh)},
	}

	writer := NewReviewArtifactWriter(dir, WithReviewArtifactClock(fixedClock()))
	meta, err := writer.Write(context.Background(), result, snapshot, 1)
	if err != nil {
		t.Fatalf("Write() = %v", err)
	}
	if meta.Snapshot != snapshot || meta.Checksum == "" {
		t.Fatalf("metadata = %+v, want snapshot bound and checksummed", meta)
	}

	stored, err := ReadManualReviewArtifact(filepath.Join(dir, meta.RelativePath), 0)
	if err != nil {
		t.Fatalf("read written artifact = %v", err)
	}
	if err := stored.Validate(); err != nil {
		t.Fatalf("stored artifact checksum invalid: %v", err)
	}
	if stored.Snapshot != snapshot {
		t.Fatalf("artifact snapshot = %q, want %q", stored.Snapshot, snapshot)
	}

	observations, err := result.Observations(0)
	if err != nil {
		t.Fatalf("Observations() = %v", err)
	}
	if len(observations) != len(stored.Findings) {
		t.Fatalf("observations=%d artifact findings=%d, want equal", len(observations), len(stored.Findings))
	}
	for i := range observations {
		if string(observations[i].Fingerprint) != stored.Findings[i].Fingerprint {
			t.Fatalf("observation %d fingerprint mismatch with artifact", i)
		}
	}

	if _, err := writer.Write(context.Background(), result, "snap-newer", 1); !errors.Is(err, ErrObservationStale) {
		t.Fatalf("stale review write = %v, want ErrObservationStale", err)
	}
}

func TestIT033ManualReviewArtifactSeeding(t *testing.T) {
	// IT-033: seed same-snapshot, stale-snapshot, absent, and oversized manual
	// review artifacts; only matching unresolved findings enter current state and
	// old or oversized evidence remains addressable by reference.
	t.Parallel()
	dir := t.TempDir()
	current := "snap-current"
	writer := NewReviewArtifactWriter(dir, WithReviewArtifactClock(fixedClock()))

	currentMeta := writeManualArtifact(t, writer, "rev-current", current)
	staleMeta := writeManualArtifact(t, writer, "rev-stale", "snap-old")
	oversizedPath := writeOversizedArtifact(t, dir)
	absentPath := filepath.Join(dir, ReviewArtifactFileName("rev-absent"))

	manual := readSeedFindings(t, filepath.Join(dir, currentMeta.RelativePath))
	manual = append(manual, readSeedFindings(t, filepath.Join(dir, staleMeta.RelativePath))...)

	// Oversized artifacts stay addressable by reference and never enter state.
	if _, err := ReadManualReviewArtifact(oversizedPath, 1024); !errors.Is(err, ErrReviewArtifactTooLarge) {
		t.Fatalf("oversized read = %v, want ErrReviewArtifactTooLarge", err)
	}
	if _, err := os.Stat(oversizedPath); err != nil {
		t.Fatalf("oversized artifact must remain addressable: %v", err)
	}
	// Absent artifacts are skipped, not fabricated.
	if _, err := ReadManualReviewArtifact(absentPath, 0); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent read = %v, want os.ErrNotExist", err)
	}

	seed, historical := SeedManualFindings(current, manual)
	if len(seed) != 1 {
		t.Fatalf("seed len = %d, want only the same-snapshot unresolved finding", len(seed))
	}
	if seed[0].Snapshot != current {
		t.Fatalf("seed snapshot = %q, want %q", seed[0].Snapshot, current)
	}
	if len(historical) != 1 || historical[0].Snapshot != "snap-old" {
		t.Fatalf("historical = %+v, want the stale-snapshot finding preserved", historical)
	}
}

func writeManualArtifact(
	t *testing.T,
	writer *ReviewArtifactWriter,
	reviewID, snapshot string,
) ReviewArtifactMetadata {
	t.Helper()
	result := ReviewResult{
		ReviewID:    reviewID,
		Snapshot:    snapshot,
		SnapshotSeq: 1,
		Outcome:     ReviewOutcomeFindings,
		Explanation: "manual review finding",
		Findings:    []ReviewedFinding{validReviewedFinding("pkg/a.go", "pkg.A", SeverityHigh)},
	}
	meta, err := writer.Write(context.Background(), result, snapshot, 1)
	if err != nil {
		t.Fatalf("write manual artifact %s: %v", reviewID, err)
	}
	return meta
}

func writeOversizedArtifact(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, ReviewArtifactFileName("rev-oversized"))
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 2048), 0o600); err != nil {
		t.Fatalf("write oversized artifact: %v", err)
	}
	return path
}

func readSeedFindings(t *testing.T, path string) []ManualFinding {
	t.Helper()
	artifact, err := ReadManualReviewArtifact(path, 1<<20)
	if err != nil {
		t.Fatalf("read manual artifact %s: %v", path, err)
	}
	return artifact.ManualFindings()
}
