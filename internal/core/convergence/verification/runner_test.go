package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testSnapshot = "snap-fixed"

func fixedSnapshotter(digest string) Snapshotter {
	return func(context.Context, string) (string, error) { return digest, nil }
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return path
}

func newTestRunner() *Runner {
	return NewRunner(WithSnapshotter(fixedSnapshotter(testSnapshot)))
}

func baseRequest(t *testing.T, command []string) Request {
	t.Helper()
	return Request{
		VerificationID:   "ver-1",
		PhaseID:          "phase-1",
		Attempt:          1,
		WorktreeRoot:     t.TempDir(),
		Command:          command,
		ExpectedSnapshot: testSnapshot,
		EvidenceDir:      filepath.Join(t.TempDir(), "evidence"),
		SummaryByteLimit: 256,
	}
}

func TestRunnerVerification(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("verification runner fixtures use POSIX shell scripts")
	}

	t.Run("Should record a passing exit-zero result with evidence", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := writeScript(t, dir, "pass.sh", "#!/bin/sh\necho 'verify ok'\nexit 0\n")
		req := baseRequest(t, []string{"/bin/sh", script})
		result, err := newTestRunner().Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if result.Completion != CompletionCompleted || !result.Passed() {
			t.Fatalf("result = %+v, want completed pass", result)
		}
		if auth := Authorize(result, testSnapshot); !auth.Authorized {
			t.Fatalf("Authorize = %v (%s), want authorized", auth.Authorized, auth.Reason)
		}
		if result.StartSnapshot != testSnapshot || result.EndSnapshot != testSnapshot {
			t.Fatalf("snapshots = %q/%q, want bound to %q", result.StartSnapshot, result.EndSnapshot, testSnapshot)
		}
		if result.CommandFingerprint == "" {
			t.Fatal("missing command fingerprint")
		}
		assertEvidenceChecksum(t, req.EvidenceDir, result.StdoutPath, result.StdoutChecksum)
		if !strings.Contains(result.StdoutSummary, "verify ok") {
			t.Fatalf("stdout summary = %q, want to contain verify ok", result.StdoutSummary)
		}
	})

	t.Run("Should record a failing result with a failure fingerprint", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := writeScript(t, dir, "fail.sh", "#!/bin/sh\necho boom 1>&2\nexit 3\n")
		req := baseRequest(t, []string{"/bin/sh", script})
		result, err := newTestRunner().Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if result.Completion != CompletionCompleted || result.Passed() {
			t.Fatalf("result = %+v, want completed failure", result)
		}
		if result.ExitCode == nil || *result.ExitCode != 3 {
			t.Fatalf("exit code = %v, want 3", result.ExitCode)
		}
		if result.FailureFingerprint == "" {
			t.Fatal("missing failure fingerprint for a failing result")
		}
		if Authorize(result, testSnapshot).Authorized {
			t.Fatal("failing result authorized a phase")
		}
		if !strings.Contains(result.StderrSummary, "boom") {
			t.Fatalf("stderr summary = %q, want to contain boom", result.StderrSummary)
		}
	})

	t.Run("Should bound the summary and checksum full oversized output", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := writeScript(t, dir, "big.sh", "#!/bin/sh\nhead -c 1048576 /dev/zero | tr '\\0' 'A'\nexit 0\n")
		req := baseRequest(t, []string{"/bin/sh", script})
		result, err := newTestRunner().Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if !result.OutputTruncated {
			t.Fatal("oversized output not marked truncated")
		}
		if len(result.StdoutSummary) > req.SummaryByteLimit {
			t.Fatalf("summary len = %d, want <= %d", len(result.StdoutSummary), req.SummaryByteLimit)
		}
		raw, err := os.ReadFile(filepath.Join(req.EvidenceDir, result.StdoutPath))
		if err != nil {
			t.Fatalf("read raw stdout: %v", err)
		}
		if len(raw) != 1048576 {
			t.Fatalf("raw stdout size = %d, want 1048576 (complete evidence retained)", len(raw))
		}
		if checksum := sha256Hex(raw); checksum != result.StdoutChecksum {
			t.Fatalf("stdout checksum mismatch: file=%s result=%s", checksum, result.StdoutChecksum)
		}
	})

	t.Run("Should classify a timed-out command", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := writeScript(t, dir, "sleep.sh", "#!/bin/sh\nsleep 30\n")
		req := baseRequest(t, []string{"/bin/sh", script})
		req.Timeout = 300 * time.Millisecond
		result, err := newTestRunner().Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if result.Completion != CompletionTimedOut {
			t.Fatalf("completion = %s, want timed_out", result.Completion)
		}
		if result.ExitCode != nil {
			t.Fatalf("timed-out result carried an exit code %v", result.ExitCode)
		}
		if Authorize(result, testSnapshot).Authorized {
			t.Fatal("timed-out result authorized a phase")
		}
	})

	t.Run("Should classify a canceled command", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		marker := filepath.Join(dir, "ready")
		script := writeScript(t, dir, "cancel.sh", "#!/bin/sh\necho ready > \"$1\"\nsleep 30\n")
		req := baseRequest(t, []string{"/bin/sh", script, marker})
		ctx, cancel := context.WithCancel(context.Background())
		type outcome struct {
			result Result
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := newTestRunner().Run(ctx, req)
			done <- outcome{result: result, err: err}
		}()
		waitForFile(t, marker)
		cancel()
		got := <-done
		if got.err != nil {
			t.Fatalf("Run() = %v", got.err)
		}
		if got.result.Completion != CompletionCanceled {
			t.Fatalf("completion = %s, want canceled", got.result.Completion)
		}
		if Authorize(got.result, testSnapshot).Authorized {
			t.Fatal("canceled result authorized a phase")
		}
	})

	t.Run("Should replay a completed result after transport loss", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := writeScript(t, dir, "pass.sh", "#!/bin/sh\nexit 0\n")
		req := baseRequest(t, []string{"/bin/sh", script})
		result, err := newTestRunner().Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run() = %v", err)
		}
		if !CanReplay(result, testSnapshot) {
			t.Fatal("trusted completion could not replay on the unchanged snapshot")
		}
		if CanReplay(result, "snap-different") {
			t.Fatal("completed result replayed on a changed snapshot")
		}
	})
}

func TestRunnerRejectsInvalidCommandWithoutProcess(t *testing.T) {
	t.Parallel()
	req := baseRequest(t, []string{"make verify"}) // shell string, not a direct argv
	if _, err := newTestRunner().Run(context.Background(), req); err == nil {
		t.Fatal("Run() accepted a shell-string command")
	}
	if _, err := os.Stat(req.EvidenceDir); !os.IsNotExist(err) {
		t.Fatalf("invalid command created evidence dir (stat err=%v)", err)
	}
}

func assertEvidenceChecksum(t *testing.T, dir, rel, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read evidence %s: %v", rel, err)
	}
	if got := sha256Hex(raw); got != want {
		t.Fatalf("evidence checksum mismatch: file=%s result=%s", got, want)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("readiness marker %s never appeared", path)
		}
		<-ticker.C
	}
}
