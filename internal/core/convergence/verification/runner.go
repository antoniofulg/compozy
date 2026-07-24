package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/compozy/compozy/internal/core/subprocess"
	"github.com/compozy/compozy/internal/core/worktree"
)

// errVerificationTimeout marks a command terminated for exceeding its absolute
// wall-clock cap rather than being canceled by the caller.
var errVerificationTimeout = errors.New("verification command exceeded wall-clock cap")

// Snapshotter captures the Git snapshot digest of a worktree root. It lets the
// runner bind evidence to the pre- and post-command snapshot while staying
// decoupled from any concrete snapshot implementation for deterministic tests.
type Snapshotter func(ctx context.Context, root string) (string, error)

// Runner executes the authoritative verification command as a direct argv with
// context-owned process cancellation and captures complete evidence. It holds no
// per-run state, so one Runner serves every phase.
type Runner struct {
	snapshot Snapshotter
	clock    func() time.Time
}

// Option configures a Runner.
type Option func(*Runner)

// WithSnapshotter overrides how the runner captures start/end snapshots.
func WithSnapshotter(s Snapshotter) Option {
	return func(r *Runner) {
		if s != nil {
			r.snapshot = s
		}
	}
}

// WithClock overrides the runner clock for deterministic timestamps in tests.
func WithClock(clock func() time.Time) Option {
	return func(r *Runner) {
		if clock != nil {
			r.clock = clock
		}
	}
}

// NewRunner constructs a Runner. By default it captures snapshots through the
// worktree package and stamps timestamps with the system UTC clock.
func NewRunner(opts ...Option) *Runner {
	r := &Runner{
		snapshot: defaultSnapshotter,
		clock:    func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run validates the request, captures the start snapshot, executes the command
// directly in the worktree root with owned cancellation, captures the end
// snapshot, and returns complete evidence. It never runs a shell and never
// reads any command other than req.Command. A validation failure returns before
// any process starts.
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	startSnapshot, err := r.snapshot(ctx, req.WorktreeRoot)
	if err != nil {
		return Result{}, fmt.Errorf("verification: capture start snapshot: %w", err)
	}
	result := Result{
		VerificationID:     req.VerificationID,
		PhaseID:            req.PhaseID,
		Attempt:            req.Attempt,
		CommandFingerprint: CommandFingerprint(req.Command),
		Executable:         req.Command[0],
		RedactedArgv:       redactArgv(req.Command),
		StartSnapshot:      startSnapshot,
		StartedAt:          r.clock(),
	}
	if err := r.execute(ctx, req, &result); err != nil {
		return Result{}, err
	}
	result.EndedAt = r.clock()
	result.EndSnapshot = r.captureEndSnapshot(ctx, req.WorktreeRoot)
	if !result.Passed() && result.Completion == CompletionCompleted {
		result.FailureFingerprint = r.failureFingerprint(result)
	}
	return result, nil
}

// execute runs the command, streams evidence, and classifies completion.
func (r *Runner) execute(ctx context.Context, req Request, result *Result) error {
	if err := os.MkdirAll(req.EvidenceDir, 0o750); err != nil {
		return fmt.Errorf("verification: create evidence directory: %w", err)
	}
	limit := req.SummaryByteLimit
	if limit <= 0 {
		limit = DefaultSummaryByteLimit
	}
	stdoutName := req.VerificationID + ".stdout.log"
	stderrName := req.VerificationID + ".stderr.log"
	stdout, err := newEvidenceWriter(filepath.Join(req.EvidenceDir, stdoutName), limit)
	if err != nil {
		return fmt.Errorf("verification: open stdout evidence: %w", err)
	}
	defer stdout.close()
	stderr, err := newEvidenceWriter(filepath.Join(req.EvidenceDir, stderrName), limit)
	if err != nil {
		return fmt.Errorf("verification: open stderr evidence: %w", err)
	}
	defer stderr.close()

	runCtx, cancel := r.runContext(ctx, req.Timeout)
	defer cancel()
	// #nosec G204 -- the configured verification argv is executed directly by design.
	cmd := exec.CommandContext(runCtx, req.Command[0], req.Command[1:]...)
	cmd.Dir = req.WorktreeRoot
	if req.Env != nil {
		cmd.Env = req.Env
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// On cancellation (caller cancel or cap expiry) reap the whole process group
	// so no verification subprocess outlives the owned context.
	cmd.Cancel = func() error { return subprocess.ForceTerminateCommandProcessTree(cmd) }
	if err := subprocess.ConfigureCommandProcessGroup(cmd); err != nil {
		return fmt.Errorf("verification: configure process group: %w", err)
	}
	if err := cmd.Start(); err != nil {
		result.Completion = CompletionIncomplete
		return nil
	}
	waitErr := cmd.Wait()
	classifyCompletion(runCtx, ctx, cmd, waitErr, result)

	result.StdoutPath = stdoutName
	result.StdoutChecksum = stdout.checksum()
	result.StderrPath = stderrName
	result.StderrChecksum = stderr.checksum()
	stdoutSummary, stdoutTruncated := stdout.summary()
	stderrSummary, stderrTruncated := stderr.summary()
	result.StdoutSummary = redactSummary(stdoutSummary)
	result.StderrSummary = redactSummary(stderrSummary)
	result.OutputTruncated = stdoutTruncated || stderrTruncated
	return nil
}

// runContext derives the owned run context, applying an absolute wall-clock cap
// when the request sets one.
func (r *Runner) runContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeoutCause(ctx, timeout, errVerificationTimeout)
	}
	return context.WithCancel(ctx)
}

// classifyCompletion records the terminal state and exit code. A timed-out or
// canceled command has no trustworthy exit and is never recorded as completed.
func classifyCompletion(runCtx, parent context.Context, cmd *exec.Cmd, waitErr error, result *Result) {
	switch {
	case errors.Is(context.Cause(runCtx), errVerificationTimeout):
		result.Completion = CompletionTimedOut
	case parent.Err() != nil:
		result.Completion = CompletionCanceled
	case cmd.ProcessState != nil && cmd.ProcessState.ExitCode() >= 0:
		code := cmd.ProcessState.ExitCode()
		result.Completion = CompletionCompleted
		result.ExitCode = &code
	case waitErr != nil:
		result.Completion = CompletionIncomplete
	default:
		result.Completion = CompletionIncomplete
	}
}

// captureEndSnapshot captures the post-command snapshot. A capture failure yields
// an empty end snapshot, which makes the result unable to authorize a phase
// rather than falsely binding it to the start snapshot.
func (r *Runner) captureEndSnapshot(ctx context.Context, root string) string {
	snapshot, err := r.snapshot(ctx, root)
	if err != nil {
		return ""
	}
	return snapshot
}

// failureFingerprint derives a stable identity for a completed nonzero result.
func (r *Runner) failureFingerprint(result Result) string {
	gate := fmt.Sprintf("%s:exit-%d", result.Executable, exitCodeOrNeg(result.ExitCode))
	signature := result.StderrSummary
	if signature == "" {
		signature = result.StdoutSummary
	}
	return FailureFingerprint(result.CommandFingerprint, gate, signature)
}

func exitCodeOrNeg(code *int) int {
	if code == nil {
		return -1
	}
	return *code
}

// defaultSnapshotter captures a supported Git snapshot digest for root.
func defaultSnapshotter(ctx context.Context, root string) (string, error) {
	snapshot, err := worktree.Capture(ctx, root)
	if err != nil {
		return "", err
	}
	if !snapshot.IsSupported() {
		return "", fmt.Errorf("verification: unsupported worktree snapshot (%s)", snapshot.UnsupportedReason())
	}
	return snapshot.Digest(), nil
}

// evidenceWriter streams command output to a raw file while maintaining a running
// checksum and a bounded tail for the redacted summary.
type evidenceWriter struct {
	file      *os.File
	hasher    hash.Hash
	tail      []byte
	limit     int
	truncated bool
}

func newEvidenceWriter(path string, limit int) (*evidenceWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &evidenceWriter{file: file, hasher: sha256.New(), limit: limit}, nil
}

// Write persists p, updates the checksum, and retains the last limit bytes.
// os/exec drives Write from a single per-stream goroutine and cmd.Wait joins it,
// so the runner reads checksum/summary only after Wait with no data race.
func (w *evidenceWriter) Write(p []byte) (int, error) {
	if _, err := w.file.Write(p); err != nil {
		return 0, err
	}
	w.hasher.Write(p)
	w.tail = append(w.tail, p...)
	if w.limit > 0 && len(w.tail) > w.limit {
		w.tail = append([]byte(nil), w.tail[len(w.tail)-w.limit:]...)
		w.truncated = true
	}
	return len(p), nil
}

func (w *evidenceWriter) checksum() string {
	return hex.EncodeToString(w.hasher.Sum(nil))
}

func (w *evidenceWriter) summary() (string, bool) {
	return string(trimUTF8Prefix(w.tail)), w.truncated
}

func (w *evidenceWriter) close() error {
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

// trimUTF8Prefix drops any leading continuation bytes so a byte-bounded tail
// starts on a valid rune boundary.
func trimUTF8Prefix(data []byte) []byte {
	start := 0
	for start < len(data) && !utf8.RuneStart(data[start]) {
		start++
	}
	return data[start:]
}
