package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/compozy/compozy/internal/core/model"
)

// ErrReadOnlyReviewUnsupported marks a runtime that cannot serve as an
// enforceable read-only reviewer. Convergence preflight maps it to a stable
// transport code and refuses the route before any model work begins.
var ErrReadOnlyReviewUnsupported = errors.New("agent runtime cannot enforce read-only review")

// ReadOnlyViolationError is a structured denial produced by the read-only
// runtime boundary. It carries the operation class and a redacted detail so the
// daemon can record a denial event without leaking secrets, absolute paths, or
// review content. It is intentionally deny-by-default: any operation not
// positively recognized as contained-read or approved-diagnostic is a violation.
type ReadOnlyViolationError struct {
	// Operation is the boundary that denied the request (write_file, terminal,
	// permission, path).
	Operation string
	// Detail is a short, redacted explanation safe for structured logs and events.
	Detail string
}

// Error implements the error interface.
func (e *ReadOnlyViolationError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Detail) == "" {
		return fmt.Sprintf("read-only review boundary denied %s", e.Operation)
	}
	return fmt.Sprintf("read-only review boundary denied %s: %s", e.Operation, e.Detail)
}

// IsReadOnlyViolation reports whether err is a read-only boundary denial.
func IsReadOnlyViolation(err error) bool {
	var violation *ReadOnlyViolationError
	return errors.As(err, &violation)
}

// isReadOnlyAccessMode reports whether the resolved access mode selects the
// enforceable read-only review capability.
func isReadOnlyAccessMode(accessMode string) bool {
	return strings.TrimSpace(accessMode) == model.AccessModeReadOnly
}

// ReadOnlyDecision is the outcome of a read-only capability check.
type ReadOnlyDecision struct {
	// Allowed reports whether the operation may proceed under read-only review.
	Allowed bool
	// Reason is a short, redacted justification. For denials it feeds the
	// structured ReadOnlyViolationError; for approvals it explains the allowance.
	Reason string
}

// ReadOnlyGuard classifies session operations for the read-only review
// capability. It is deny-by-default: only a contained read or an explicitly
// recognized non-mutating diagnostic is allowed. Every other operation — project
// write, rename, delete, Git mutation, mutating terminal, network side effect,
// path expansion, or authority escalation — is denied. The guard is pure: it
// performs no I/O and holds no state, so the runtime boundary and unit tests
// evaluate identical policy.
type ReadOnlyGuard struct{}

// FileRead permits a contained repository read. Path containment against the
// session's allowed roots is enforced separately by the caller; the read itself
// never mutates project state.
func (ReadOnlyGuard) FileRead() ReadOnlyDecision {
	return ReadOnlyDecision{Allowed: true, Reason: "contained repository read"}
}

// FileWrite denies every project file creation, write, rename, or deletion.
// The reviewer session has no artifact write exception; the daemon is the only
// writer of review artifacts.
func (ReadOnlyGuard) FileWrite() ReadOnlyDecision {
	return ReadOnlyDecision{Allowed: false, Reason: "project file mutation is denied"}
}

// Permission denies authority escalation. A read-only reviewer never receives an
// expanded permission, a new writable path, or an approval that would let it
// mutate project, Git, network, or environment state.
func (ReadOnlyGuard) Permission() ReadOnlyDecision {
	return ReadOnlyDecision{Allowed: false, Reason: "permission escalation is denied"}
}

// Terminal classifies a normalized terminal command over its argv. It permits
// only a conservative allowlist of non-mutating diagnostics and denies
// everything else, including Git mutation, file mutation, network side effects,
// shells, interpreters, and privilege escalation. Because the runner executes
// argv directly with no shell, redirection and pipe tokens arrive as literal
// arguments and cannot smuggle a side effect past this policy.
func (ReadOnlyGuard) Terminal(command string, args []string) ReadOnlyDecision {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ReadOnlyDecision{Allowed: false, Reason: "empty terminal command is denied"}
	}
	if base == "git" {
		return classifyReadOnlyGit(args)
	}
	if _, ok := readOnlyDiagnosticExecutables[base]; ok {
		return ReadOnlyDecision{Allowed: true, Reason: "non-mutating diagnostic " + base}
	}
	return ReadOnlyDecision{Allowed: false, Reason: "command " + base + " is not an approved read-only diagnostic"}
}

// classifyReadOnlyGit allows only unambiguously read-only Git subcommands. Global
// options that carry a separate value are skipped so the true subcommand is
// found; any command that cannot be resolved to a read subcommand is denied.
func classifyReadOnlyGit(args []string) ReadOnlyDecision {
	sub := gitSubcommand(args)
	if sub == "" {
		return ReadOnlyDecision{Allowed: false, Reason: "git command has no read-only subcommand"}
	}
	if _, ok := readOnlyGitSubcommands[sub]; ok {
		return ReadOnlyDecision{Allowed: true, Reason: "non-mutating git " + sub}
	}
	return ReadOnlyDecision{Allowed: false, Reason: "git " + sub + " may mutate repository state"}
}

// gitSubcommand returns the first non-option token, skipping the value of the
// global options that take one. It returns "" when no subcommand is present.
func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return strings.ToLower(strings.TrimSpace(arg))
		}
		if _, takesValue := gitGlobalValueOptions[arg]; takesValue {
			i++ // skip the option's value
		}
	}
	return ""
}

// gitGlobalValueOptions are the git global options that consume a following
// value argument; their values must be skipped when locating the subcommand.
var gitGlobalValueOptions = map[string]struct{}{
	"-C": {}, "-c": {}, "--git-dir": {}, "--work-tree": {},
	"--namespace": {}, "--super-prefix": {}, "--exec-path": {},
}

// readOnlyGitSubcommands is the closed allowlist of Git subcommands that never
// mutate repository, index, working tree, or remote state.
var readOnlyGitSubcommands = map[string]struct{}{
	"status": {}, "diff": {}, "log": {}, "show": {}, "rev-parse": {},
	"ls-files": {}, "ls-tree": {}, "cat-file": {}, "describe": {}, "blame": {},
	"shortlog": {}, "grep": {}, "reflog": {}, "rev-list": {}, "for-each-ref": {},
	"name-rev": {}, "merge-base": {}, "show-ref": {}, "symbolic-ref": {},
	"whatchanged": {}, "count-objects": {}, "cherry": {},
}

// readOnlyDiagnosticExecutables is the closed allowlist of non-Git executables
// permitted under read-only review. Every entry inspects state without mutating
// the project, touching the network, spawning arbitrary code, or escalating
// authority. Shells, interpreters, editors, package managers, and network tools
// are intentionally absent and therefore denied by default.
var readOnlyDiagnosticExecutables = map[string]struct{}{
	"ls": {}, "cat": {}, "head": {}, "tail": {}, "wc": {}, "stat": {},
	"echo": {}, "pwd": {}, "true": {}, "printf": {}, "grep": {}, "egrep": {},
	"fgrep": {}, "rg": {}, "sort": {}, "uniq": {}, "cut": {}, "tr": {}, "nl": {},
	"diff": {}, "cmp": {}, "basename": {}, "dirname": {}, "realpath": {},
	"readlink": {}, "date": {}, "uname": {}, "file": {}, "tree": {}, "comm": {},
	"tac": {}, "fold": {}, "od": {}, "hexdump": {}, "cksum": {}, "sha256sum": {},
	"md5sum": {}, "du": {}, "df": {}, "whoami": {}, "hostname": {}, "id": {},
	"groups": {}, "seq": {}, "column": {},
}

// ReadOnlyReviewSupported reports whether the runtime declares that it can serve
// as an enforceable read-only reviewer. A runtime qualifies only when every
// project mutation routes through the Compozy ACP boundary or a bootstrap-level
// read-only sandbox, so the deny-by-default guard can contain it.
func ReadOnlyReviewSupported(ide string) (bool, error) {
	spec, err := lookupAgentSpec(ide)
	if err != nil {
		return false, err
	}
	return spec.SupportsReadOnlyReview, nil
}

// EnsureReadOnlyReviewer validates that a selected reviewer runtime can enforce
// read-only authority. It is called in preflight for both the primary and the
// fallback route before any model work; an unsupported route fails here.
func EnsureReadOnlyReviewer(ide string) error {
	supported, err := ReadOnlyReviewSupported(ide)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrReadOnlyReviewUnsupported, err)
	}
	if !supported {
		return fmt.Errorf("%w: %q", ErrReadOnlyReviewUnsupported, strings.TrimSpace(ide))
	}
	return nil
}
