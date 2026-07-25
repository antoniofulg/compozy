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
		return classifyReadOnlyDiagnostic(base, args)
	}
	return ReadOnlyDecision{Allowed: false, Reason: "command " + base + " is not an approved read-only diagnostic"}
}

// TerminalEnvironment permits only presentation-related environment overrides.
// Executable, Git, configuration, pager, path, and temporary-directory controls
// are denied so an allowed argv cannot regain mutation or code-execution
// capability through its environment.
func (ReadOnlyGuard) TerminalEnvironment(names []string) ReadOnlyDecision {
	for _, rawName := range names {
		name := strings.ToUpper(strings.TrimSpace(rawName))
		if name == "" {
			continue
		}
		if _, ok := readOnlyTerminalEnvironmentVariables[name]; !ok {
			return ReadOnlyDecision{
				Allowed: false,
				Reason:  "terminal environment override is not approved for read-only diagnostics",
			}
		}
	}
	return ReadOnlyDecision{Allowed: true, Reason: "presentation-only terminal environment"}
}

// classifyReadOnlyGit allows only unambiguously read-only Git subcommands. Global
// options that carry a separate value are skipped so the true subcommand is
// found; command-execution and file-output flags are denied even when the
// subcommand itself is read-only.
func classifyReadOnlyGit(args []string) ReadOnlyDecision {
	sub, subIndex := gitSubcommand(args)
	if sub == "" {
		return ReadOnlyDecision{Allowed: false, Reason: "git command has no read-only subcommand"}
	}
	if hasUnsafeGitGlobalOption(args[:subIndex]) {
		return ReadOnlyDecision{
			Allowed: false,
			Reason:  "git global option may execute code or change diagnostic behavior",
		}
	}
	if _, ok := readOnlyGitSubcommands[sub]; !ok {
		return ReadOnlyDecision{Allowed: false, Reason: "git " + sub + " may mutate repository state"}
	}
	if hasGitWriteCapableOption(args[subIndex+1:]) {
		return ReadOnlyDecision{Allowed: false, Reason: "git " + sub + " option may write files or execute code"}
	}
	return ReadOnlyDecision{Allowed: true, Reason: "non-mutating git " + sub}
}

// gitSubcommand returns the first non-option token, skipping the value of the
// global options that take one. It returns the normalized subcommand and its
// argv index, or an empty subcommand and -1 when none is present.
func gitSubcommand(args []string) (string, int) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return strings.ToLower(strings.TrimSpace(arg)), i
		}
		if _, takesValue := gitGlobalValueOptions[arg]; takesValue {
			i++ // skip the option's value
		}
	}
	return "", -1
}

func hasUnsafeGitGlobalOption(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "-c", strings.HasPrefix(arg, "-c") && len(arg) > 2:
			return true
		case arg == "-C", strings.HasPrefix(arg, "-C") && len(arg) > 2:
			return true
		case optionMatches(arg, "--config-env"), optionMatches(arg, "--exec-path"):
			return true
		case optionMatches(arg, "--git-dir"), optionMatches(arg, "--work-tree"):
			return true
		case arg == "-p", arg == "--paginate":
			return true
		}
	}
	return false
}

func hasGitWriteCapableOption(args []string) bool {
	for _, arg := range args {
		if optionMatches(arg, "--output") ||
			optionMatches(arg, "--ext-diff") ||
			optionMatches(arg, "--textconv") ||
			optionMatches(arg, "--open-files-in-pager") ||
			shortOptionMatches(arg, "-O") {
			return true
		}
	}
	return false
}

func classifyReadOnlyDiagnostic(base string, args []string) ReadOnlyDecision {
	unsafe := false
	switch base {
	case "sort":
		unsafe = hasShortOrLongOption(args, "-o", "--output") ||
			hasShortOrLongOption(args, "-T", "--temporary-directory") ||
			hasLongOption(args, "--compress-program")
	case "tree":
		unsafe = hasShortOrLongOption(args, "-o", "--output")
	case "date":
		unsafe = hasShortOrLongOption(args, "-s", "--set")
	case "hostname":
		unsafe = hasShortOrLongOption(args, "-F", "--file") ||
			hasShortOption(args, "-b") ||
			hasPositionalArgument(args)
	case "rg":
		unsafe = hasLongOption(args, "--pre") ||
			hasLongOption(args, "--hostname-bin") ||
			hasShortOrLongOption(args, "-z", "--search-zip")
	}
	if unsafe {
		return ReadOnlyDecision{
			Allowed: false,
			Reason:  "diagnostic " + base + " option may mutate state or execute code",
		}
	}
	return ReadOnlyDecision{Allowed: true, Reason: "non-mutating diagnostic " + base}
}

func hasShortOrLongOption(args []string, short, long string) bool {
	return hasShortOption(args, short) || hasLongOption(args, long)
}

func hasShortOption(args []string, option string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if shortOptionMatches(arg, option) {
			return true
		}
	}
	return false
}

func shortOptionMatches(arg, option string) bool {
	if len(option) != 2 || option[0] != '-' || len(arg) < 2 ||
		arg[0] != '-' || strings.HasPrefix(arg, "--") {
		return false
	}
	return strings.ContainsRune(arg[1:], rune(option[1]))
}

func hasLongOption(args []string, option string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if optionMatches(arg, option) {
			return true
		}
	}
	return false
}

func optionMatches(arg, option string) bool {
	return arg == option || strings.HasPrefix(arg, option+"=")
}

func hasPositionalArgument(args []string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) != "" && !strings.HasPrefix(arg, "-") {
			return true
		}
	}
	return false
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
	"shortlog": {}, "grep": {}, "rev-list": {}, "for-each-ref": {}, "name-rev": {},
	"merge-base": {}, "show-ref": {}, "whatchanged": {}, "count-objects": {}, "cherry": {},
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

// readOnlyTerminalEnvironmentVariables is deliberately narrow. These variables
// influence locale, terminal dimensions, or color presentation only; they do
// not select executables, configuration files, pagers, temporary directories,
// or command hooks.
var readOnlyTerminalEnvironmentVariables = map[string]struct{}{
	"LANG": {}, "LANGUAGE": {},
	"LC_ALL": {}, "LC_COLLATE": {}, "LC_CTYPE": {}, "LC_MESSAGES": {},
	"LC_MONETARY": {}, "LC_NUMERIC": {}, "LC_TIME": {},
	"TERM": {}, "COLORTERM": {}, "COLUMNS": {}, "LINES": {},
	"NO_COLOR": {}, "CLICOLOR": {}, "CLICOLOR_FORCE": {},
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
