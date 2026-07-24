package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	commandFingerprintSchema = "convergence-verification-command-v1"
	failureFingerprintSchema = "convergence-verification-failure-v1"
)

var whitespaceRun = regexp.MustCompile(`\s+`)

// CommandFingerprint is the lowercase hexadecimal SHA-256 over the canonical
// verification argv. Two runs of the same argv share a fingerprint; any
// difference in the argv, including argument order, produces a distinct one.
func CommandFingerprint(command []string) string {
	h := sha256.New()
	h.Write([]byte(commandFingerprintSchema))
	for _, arg := range command {
		h.Write([]byte{0})
		h.Write([]byte(arg))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// FailureFingerprint is the lowercase hexadecimal SHA-256 over the command
// fingerprint, the failing gate identifier, and the normalized failure
// signature. It identifies a verification failure across attempts without
// requiring a source file, so correction attempts can be counted against a
// stable identity.
func FailureFingerprint(commandFingerprint, gateID, signature string) string {
	h := sha256.New()
	h.Write([]byte(failureFingerprintSchema))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(commandFingerprint)))
	h.Write([]byte{0})
	h.Write([]byte(strings.ToLower(strings.TrimSpace(gateID))))
	h.Write([]byte{0})
	h.Write([]byte(normalizeSignature(signature)))
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeSignature canonicalizes a failure signature: NFC, trimmed ends, and
// collapsed internal whitespace. It preserves case and punctuation so distinct
// failures keep distinct identities.
func normalizeSignature(raw string) string {
	normalized := norm.NFC.String(raw)
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(normalized, " "))
}
