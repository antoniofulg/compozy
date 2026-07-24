package verification

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors for verification request rejections. Callers match them with
// errors.Is. A later transport slice maps them to stable error codes; this
// package stays free of transport concerns.
var (
	// ErrRequestInvalid marks a verification request missing required identity or
	// execution-scope fields.
	ErrRequestInvalid = errors.New("verification: request invalid")
	// ErrCommandInvalid marks a verification command that is missing, empty, or
	// would require shell inference.
	ErrCommandInvalid = errors.New("verification: command invalid")
)

// Request is one authoritative verification invocation. Command is the frozen
// `[convergence.verification].command` argv; the runner never derives it from a
// shell string, a heuristic over repository files, or the conflict resolver's
// validation command.
type Request struct {
	// VerificationID identifies this verification attempt's evidence.
	VerificationID string
	// PhaseID is the owning phase the result is bound to.
	PhaseID string
	// Attempt is the 1-based attempt number for the owning verification failure.
	Attempt int
	// WorktreeRoot is the target worktree directory the command runs in.
	WorktreeRoot string
	// Command is the direct argv executed without a shell.
	Command []string
	// ExpectedSnapshot is the frozen Git snapshot digest the result must remain
	// bound to; a change during execution makes the result unable to authorize.
	ExpectedSnapshot string
	// Env is the inherited safe environment. Nil inherits the process environment.
	Env []string
	// EvidenceDir is the run-relative directory raw stdout/stderr files are
	// written into.
	EvidenceDir string
	// SummaryByteLimit bounds the redacted stdout/stderr summaries. Zero selects
	// DefaultSummaryByteLimit.
	SummaryByteLimit int
	// Timeout is the absolute wall-clock cap for the command. Zero disables the
	// cap and relies on context cancellation alone.
	Timeout time.Duration
}

// DefaultSummaryByteLimit bounds redacted stdout/stderr summaries when a request
// does not specify one.
const DefaultSummaryByteLimit = 16 * 1024

// Validate rejects a request that is missing identity, an execution scope, or a
// runnable command before any process starts. It enforces a nonblank executable,
// no NUL bytes, and no whitespace-bearing executable that would imply a shell
// string rather than a direct argv.
func (r Request) Validate() error {
	if strings.TrimSpace(r.VerificationID) == "" {
		return fmt.Errorf("%w: verification id is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.PhaseID) == "" {
		return fmt.Errorf("%w: phase id is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.WorktreeRoot) == "" {
		return fmt.Errorf("%w: worktree root is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.EvidenceDir) == "" {
		return fmt.Errorf("%w: evidence directory is required", ErrRequestInvalid)
	}
	return ValidateCommand(r.Command)
}

// ValidateCommand enforces the direct-argv verification command contract: a
// nonblank executable, no NUL bytes, and no shell inference. It is exported so
// configuration and preflight can reject a malformed command with the same rule
// the runner applies.
func ValidateCommand(command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("%w: command must be a nonempty argv array", ErrCommandInvalid)
	}
	executable := strings.TrimSpace(command[0])
	if executable == "" {
		return fmt.Errorf("%w: command executable must not be blank", ErrCommandInvalid)
	}
	if strings.ContainsAny(executable, " \t\n") {
		return fmt.Errorf(
			"%w: executable %q contains whitespace; provide a direct argv, not a shell string",
			ErrCommandInvalid,
			executable,
		)
	}
	for idx, arg := range command {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("%w: command[%d] must not contain NUL bytes", ErrCommandInvalid, idx)
		}
	}
	return nil
}
