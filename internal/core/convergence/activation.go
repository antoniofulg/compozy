package convergence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ActivationRequest carries the identity inputs of a convergence activation. Its
// fingerprint enforces one active or replayable result per logical convergence
// history. A legal new run after a terminal result must supply an explicit nonce
// so it produces a distinct fingerprint.
type ActivationRequest struct {
	// WorkspaceID identifies the authorized workspace.
	WorkspaceID string
	// ExecutionScopeID identifies the recognized execution scope.
	ExecutionScopeID string
	// TargetSnapshot is the frozen Git snapshot digest of the target.
	TargetSnapshot string
	// NormalizedIntent is the caller-normalized convergence intent.
	NormalizedIntent string
	// SourceRunID is the task or Task Group run that requested convergence. It is
	// empty for standalone activation.
	SourceRunID string
	// Nonce is set only for a legal new run and makes its fingerprint distinct.
	Nonce string
}

// Fingerprint returns the lowercase hexadecimal SHA-256 activation fingerprint.
// Identical requests produce identical fingerprints so duplicates return the same
// run; a request with a distinct nonce produces a distinct fingerprint.
func (r ActivationRequest) Fingerprint() (string, error) {
	fields := []struct {
		name  string
		value string
	}{
		{"workspace", strings.TrimSpace(r.WorkspaceID)},
		{"execution_scope", strings.TrimSpace(r.ExecutionScopeID)},
		{"target_snapshot", strings.TrimSpace(r.TargetSnapshot)},
		{"intent", strings.TrimSpace(r.NormalizedIntent)},
	}
	for _, field := range fields {
		if field.value == "" {
			return "", fmt.Errorf("%w: %s is required", ErrActivationInvalid, field.name)
		}
	}
	canonical := strings.Join([]string{
		fields[0].value,
		fields[1].value,
		fields[2].value,
		fields[3].value,
		strings.TrimSpace(r.SourceRunID),
		strings.TrimSpace(r.Nonce),
	}, "|")
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:]), nil
}

// RequireNonceForNewRun enforces that a new run started after a terminal result
// carries an explicit nonce. Without it, activation must return the existing
// terminal run rather than allocating a new one.
func RequireNonceForNewRun(priorTerminal bool, nonce string) error {
	if priorTerminal && strings.TrimSpace(nonce) == "" {
		return fmt.Errorf("%w: a new run after a terminal result requires an explicit nonce", ErrActivationInvalid)
	}
	return nil
}
