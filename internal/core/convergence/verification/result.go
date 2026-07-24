package verification

import "time"

// Completion is the terminal execution state of a verification command. Only
// Completed carries an exit code; every other state means the command's exit is
// unknown and can never be read as a pass.
type Completion string

const (
	// CompletionCompleted means the process ran to termination and reported an
	// exit code.
	CompletionCompleted Completion = "completed"
	// CompletionCanceled means the run context was canceled before the process
	// reported an exit.
	CompletionCanceled Completion = "canceled"
	// CompletionTimedOut means the command exceeded its absolute wall-clock cap.
	CompletionTimedOut Completion = "timed_out"
	// CompletionIncomplete means the command produced no trustworthy exit code for
	// another reason, such as a failure to start or a lost terminal response.
	CompletionIncomplete Completion = "incomplete"
)

// Result is the durable evidence of one authoritative verification run. It never
// serializes credentials, environment values, or full raw output inline; the
// complete stdout/stderr are referenced by contained relative path and checksum,
// and the inline summaries are bounded and redacted.
type Result struct {
	// VerificationID and PhaseID bind the evidence to its attempt and phase.
	VerificationID string
	PhaseID        string
	// Attempt is the 1-based attempt number for the owning verification failure.
	Attempt int
	// CommandFingerprint is the stable SHA-256 identity of the executed argv.
	CommandFingerprint string
	// Executable is the resolved command executable.
	Executable string
	// RedactedArgv is the argv with secret-bearing values redacted.
	RedactedArgv []string
	// StartSnapshot and EndSnapshot are the Git snapshot digests captured
	// immediately before and after the command. A difference means the worktree
	// changed during verification and the result cannot authorize a phase.
	StartSnapshot string
	EndSnapshot   string
	// StartedAt and EndedAt are UTC timestamps around the command.
	StartedAt time.Time
	EndedAt   time.Time
	// Completion classifies how the command terminated.
	Completion Completion
	// ExitCode is set only when Completion is Completed.
	ExitCode *int
	// StdoutSummary and StderrSummary are bounded, redacted tails of the output.
	StdoutSummary string
	StderrSummary string
	// StdoutPath and StderrPath reference the complete raw output by contained
	// relative path; StdoutChecksum and StderrChecksum are SHA-256 over the raw
	// bytes.
	StdoutPath     string
	StdoutChecksum string
	StderrPath     string
	StderrChecksum string
	// OutputTruncated reports whether either inline summary omitted earlier bytes.
	OutputTruncated bool
	// FailureFingerprint is the stable failure identity for a nonzero result; it
	// is empty for a passing result.
	FailureFingerprint string
}

// Passed reports whether the result is a completed, exit-zero verification. It
// makes no snapshot claim; use Authorize to bind a pass to an unchanged
// snapshot before authorizing another phase.
func (r Result) Passed() bool {
	return r.Completion == CompletionCompleted && r.ExitCode != nil && *r.ExitCode == 0
}
