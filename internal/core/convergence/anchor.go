package convergence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// canonical reduces the anchor to its canonical identity string. Symbol and
// configuration anchors preserve their language identity. A content anchor hashes
// the required unit kind with the normalized code unit so distinct units do not
// collide. An anchor that cannot be reliably established fails closed.
func (a Anchor) canonical() (string, error) {
	switch a.Kind {
	case AnchorSymbol, AnchorConfig:
		return a.canonicalIdentityValue()
	case AnchorContent:
		return a.canonicalContent()
	default:
		return "", fmt.Errorf(
			"%w: anchor kind must be symbol, config, or content: %q",
			ErrFindingIdentityInvalid,
			a.Kind,
		)
	}
}

func (a Anchor) canonicalIdentityValue() (string, error) {
	trimmed := strings.TrimSpace(a.Value)
	if trimmed == "" {
		return "", fmt.Errorf("%w: %s anchor value is required", ErrFindingIdentityInvalid, a.Kind)
	}
	if !utf8.ValidString(trimmed) {
		return "", fmt.Errorf("%w: anchor value is not valid UTF-8", ErrFindingIdentityInvalid)
	}
	return norm.NFC.String(trimmed), nil
}

func (a Anchor) canonicalContent() (string, error) {
	unit := strings.TrimSpace(a.Unit)
	if unit == "" {
		return "", fmt.Errorf("%w: content anchor requires a unit kind", ErrFindingIdentityInvalid)
	}
	if !utf8.ValidString(a.ContentUnit) || !utf8.ValidString(unit) {
		return "", fmt.Errorf("%w: content anchor is not valid UTF-8", ErrFindingIdentityInvalid)
	}
	body := norm.NFC.String(strings.TrimSpace(a.ContentUnit))
	if body == "" {
		return "", fmt.Errorf("%w: content anchor unit is empty", ErrFindingIdentityInvalid)
	}
	digest := sha256.Sum256([]byte(strings.ToLower(unit) + "\x00" + body))
	return hex.EncodeToString(digest[:]), nil
}
