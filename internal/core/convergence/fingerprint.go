package convergence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// FingerprintAlgorithm is the versioned name of the finding identity algorithm.
// It is persisted alongside the digest so identity can migrate deterministically.
const FingerprintAlgorithm = "semantic-v1"

// FindingFingerprint is a lowercase hexadecimal SHA-256 digest identifying a
// finding across snapshots. It is deterministic and independent of line, column,
// severity, state, snapshot, evidence, prose, and reviewer IDs.
type FindingFingerprint string

// AnchorKind classifies how a finding's stable anchor is derived.
type AnchorKind string

const (
	// AnchorSymbol anchors on a language symbol identity such as package.Symbol.
	AnchorSymbol AnchorKind = "symbol"
	// AnchorConfig anchors on a configuration key path.
	AnchorConfig AnchorKind = "config"
	// AnchorContent anchors on a hash of the smallest stable normalized code unit.
	AnchorContent AnchorKind = "content"
)

// Anchor is a finding's stable anchor input. Symbol and configuration anchors use
// their language identity and preserve case. A content anchor hashes ContentUnit
// together with the required Unit kind.
type Anchor struct {
	Kind        AnchorKind
	Value       string
	Unit        string
	ContentUnit string
}

// FindingIdentity is the reviewer-supplied identity input for one finding. Only
// these fields feed the fingerprint; every other reported value is observation
// data.
type FindingIdentity struct {
	File     string
	Category string
	Anchor   Anchor
	Claim    string
}

// canonicalIdentity is serialized with lexicographically ordered keys and UTF-8
// encoding. Field order here is the canonical key order.
type canonicalIdentity struct {
	Anchor   string `json:"anchor"`
	Category string `json:"category"`
	Claim    string `json:"claim"`
	File     string `json:"file"`
}

var categoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
var windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)
var whitespaceRun = regexp.MustCompile(`\s+`)

// Canonicalize validates the identity input and returns its canonical UTF-8 JSON
// bytes with lexicographically ordered keys. It fails closed on any invalid,
// missing, or ambiguous input rather than guessing an identity.
func (id FindingIdentity) Canonicalize() ([]byte, error) {
	file, err := normalizeFindingPath(id.File)
	if err != nil {
		return nil, err
	}
	category, err := normalizeCategory(id.Category)
	if err != nil {
		return nil, err
	}
	anchor, err := id.Anchor.canonical()
	if err != nil {
		return nil, err
	}
	claim, err := normalizeClaim(id.Claim)
	if err != nil {
		return nil, err
	}
	return encodeCanonical(canonicalIdentity{Anchor: anchor, Category: category, Claim: claim, File: file})
}

// Fingerprint computes the lowercase hexadecimal SHA-256 semantic-v1 fingerprint
// for the identity, or an error wrapping ErrFindingIdentityInvalid.
func (id FindingIdentity) Fingerprint() (FindingFingerprint, error) {
	canonical, err := id.Canonicalize()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return FindingFingerprint(hex.EncodeToString(digest[:])), nil
}

func encodeCanonical(value canonicalIdentity) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("%w: canonical encode: %v", ErrFindingIdentityInvalid, err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func normalizeFindingPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: file path is required", ErrFindingIdentityInvalid)
	}
	if !utf8.ValidString(trimmed) {
		return "", fmt.Errorf("%w: file path is not valid UTF-8", ErrFindingIdentityInvalid)
	}
	slashed := strings.ReplaceAll(trimmed, "\\", "/")
	if path.IsAbs(slashed) || windowsVolumePattern.MatchString(slashed) {
		return "", fmt.Errorf("%w: file path must be repository-relative: %q", ErrFindingIdentityInvalid, raw)
	}
	cleaned := path.Clean(slashed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: file path escapes the repository: %q", ErrFindingIdentityInvalid, raw)
	}
	return cleaned, nil
}

func normalizeCategory(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: category is required", ErrFindingIdentityInvalid)
	}
	if !utf8.ValidString(trimmed) {
		return "", fmt.Errorf("%w: category is not valid UTF-8", ErrFindingIdentityInvalid)
	}
	if trimmed != strings.ToLower(trimmed) || !categoryPattern.MatchString(trimmed) {
		return "", fmt.Errorf("%w: category must be a fixed lowercase slug: %q", ErrFindingIdentityInvalid, raw)
	}
	return trimmed, nil
}

func normalizeClaim(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%w: claim is not valid UTF-8", ErrFindingIdentityInvalid)
	}
	normalized := norm.NFC.String(raw)
	collapsed := strings.TrimSpace(whitespaceRun.ReplaceAllString(normalized, " "))
	if collapsed == "" {
		return "", fmt.Errorf("%w: claim is required", ErrFindingIdentityInvalid)
	}
	return collapsed, nil
}
