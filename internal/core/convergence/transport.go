package convergence

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

// Stable transport error codes for convergence. They are the wire contract older
// and newer clients match on; task_05 wires them into the HTTP error envelope.
// The pure domain owns the canonical sentinel->code table so the mapping has one
// source of truth.
const (
	CodeConfigInvalid        = "convergence_config_invalid"
	CodeVerificationRequired = "convergence_verification_required"
	CodeReadOnlyUnsupported  = "convergence_read_only_unsupported"
	CodeTargetIneligible     = "convergence_target_ineligible"
	CodeAlreadyActive        = "convergence_already_active"
	CodeFingerprintMismatch  = "convergence_fingerprint_mismatch"
	CodeNotParked            = "convergence_not_parked"
	CodeResumeCursorStale    = "convergence_resume_cursor_stale"
	CodeApprovalStale        = "convergence_approval_stale"
	CodeWorkspaceChanged     = "convergence_workspace_changed"
	CodeUnknownOutcome       = "convergence_unknown_outcome"
	CodeIntegrityFailed      = "convergence_integrity_failed"
)

// TransportCodes returns every stable convergence transport code in a stable
// order. Compatibility and error-contract tests enumerate it.
func TransportCodes() []string {
	return []string{
		CodeConfigInvalid,
		CodeVerificationRequired,
		CodeReadOnlyUnsupported,
		CodeTargetIneligible,
		CodeAlreadyActive,
		CodeFingerprintMismatch,
		CodeNotParked,
		CodeResumeCursorStale,
		CodeApprovalStale,
		CodeWorkspaceChanged,
		CodeUnknownOutcome,
		CodeIntegrityFailed,
	}
}

// errorCodeMapping is the canonical sentinel->code table.
var errorCodeMapping = []struct {
	sentinel error
	code     string
}{
	{ErrVerificationRequired, CodeVerificationRequired},
	{ErrReadOnlyUnsupported, CodeReadOnlyUnsupported},
	{ErrTargetIneligible, CodeTargetIneligible},
	{ErrAlreadyActive, CodeAlreadyActive},
	{ErrFingerprintMismatch, CodeFingerprintMismatch},
	{ErrNotParked, CodeNotParked},
	{ErrResumeCursorStale, CodeResumeCursorStale},
	{ErrApprovalStale, CodeApprovalStale},
	{ErrWorkspaceChanged, CodeWorkspaceChanged},
	{ErrUnknownOutcome, CodeUnknownOutcome},
	{ErrIntegrityFailed, CodeIntegrityFailed},
	{ErrConfigInvalid, CodeConfigInvalid},
	{ErrProfileNotFound, CodeConfigInvalid},
	{ErrModelSetupNotFound, CodeConfigInvalid},
	{ErrRouteInvalid, CodeConfigInvalid},
	{ErrActivationInvalid, CodeConfigInvalid},
	{ErrReplayConflict, CodeIntegrityFailed},
	{ErrTransitionInvalid, CodeIntegrityFailed},
}

// TransportCode maps a domain error to its stable transport code. The most
// specific sentinel wins; verification and target eligibility are checked before
// the generic configuration code so a wrapped error resolves precisely. It
// returns false when no convergence code applies.
func TransportCode(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	for _, mapping := range errorCodeMapping {
		if errors.Is(err, mapping.sentinel) {
			return mapping.code, true
		}
	}
	return "", false
}

var (
	// absolutePathPattern matches an absolute host path — a Windows drive path or a
	// slash-rooted Unix path — only at a token boundary (start of string or after
	// whitespace, a quote, `=`, or `(`). Anchoring to a boundary is what keeps a
	// contained relative evidence reference such as `evidence/finding.json` intact:
	// its slash is mid-token, so it is retained, while `/abs/host/path` is redacted.
	// RE2 has no lookbehind, so the boundary is a captured group re-emitted by the
	// replacement.
	absolutePathPattern = regexp.MustCompile(`(^|[\s"'=(])((?:[A-Za-z]:\\[^\s"']*)|/[^\s"':]+(?:/[^\s"':]+)*)`)
	allowedDetailKeys   = map[string]struct{}{
		"field":          {},
		"config_path":    {},
		"code":           {},
		"run_id":         {},
		"convergence_id": {},
		"segment_id":     {},
		"phase_id":       {},
		"proposal_id":    {},
		"round_id":       {},
		"fingerprint":    {},
		"snapshot":       {},
		"outcome":        {},
	}
)

// Redact removes protected detail from a transport string: absolute filesystem
// paths become a bounded marker so an unrestricted host path can never leak.
// Review prose, model output, and secrets are never placed in transport strings
// in the first place; SafeDetails enforces that structurally.
func Redact(value string) string {
	return absolutePathPattern.ReplaceAllString(value, "${1}[redacted-path]")
}

// SafeDetails returns only the allow-listed, redacted subset of transport detail
// keys. Any key that is not an authorized identifier or configuration field path
// is dropped, and every retained value is redacted. This keeps protected review
// content, model output, and authorization identities out of the transport
// envelope by construction rather than by filtering after the fact.
func SafeDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	safe := make(map[string]string, len(details))
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := allowedDetailKeys[strings.TrimSpace(key)]; !ok {
			continue
		}
		safe[key] = Redact(details[key])
	}
	if len(safe) == 0 {
		return nil
	}
	return safe
}
