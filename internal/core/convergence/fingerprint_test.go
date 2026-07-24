package convergence

import (
	"errors"
	"regexp"
	"testing"
)

var lowercaseHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func baseIdentity() FindingIdentity {
	return FindingIdentity{
		File:     "internal/payments/service.go",
		Category: "correctness",
		Anchor:   Anchor{Kind: AnchorSymbol, Value: "payments.Service.Charge"},
		Claim:    "charge does not roll back on partial failure",
	}
}

func mustFingerprint(t *testing.T, id FindingIdentity) FindingFingerprint {
	t.Helper()
	fingerprint, err := id.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	return fingerprint
}

func TestSemanticV1FingerprintStability(t *testing.T) {
	t.Parallel()
	t.Run("Should produce a lowercase hex SHA-256 digest", func(t *testing.T) {
		t.Parallel()
		fingerprint := mustFingerprint(t, baseIdentity())
		if !lowercaseHex64.MatchString(string(fingerprint)) {
			t.Fatalf("fingerprint is not lowercase hex SHA-256: %q", fingerprint)
		}
	})
	t.Run("Should be identical for the same identity", func(t *testing.T) {
		t.Parallel()
		first := mustFingerprint(t, baseIdentity())
		second := mustFingerprint(t, baseIdentity())
		if first != second {
			t.Fatal("expected identical fingerprints for identical identity")
		}
	})
	t.Run("Should collapse claim whitespace", func(t *testing.T) {
		t.Parallel()
		tidy := baseIdentity()
		tidy.Claim = "charge does not roll back on partial failure"
		messy := baseIdentity()
		messy.Claim = "  charge   does not\troll  back on partial failure  "
		if mustFingerprint(t, tidy) != mustFingerprint(t, messy) {
			t.Fatal("expected whitespace-insensitive claim normalization")
		}
	})
	t.Run("Should unify composed and decomposed NFC forms", func(t *testing.T) {
		t.Parallel()
		composed := baseIdentity()
		composed.Claim = "café charge fails" // precomposed e-acute
		decomposed := baseIdentity()
		decomposed.Claim = "café charge fails" // e + combining acute
		if mustFingerprint(t, composed) != mustFingerprint(t, decomposed) {
			t.Fatal("expected NFC to unify composed and decomposed claims")
		}
	})
}

func TestSemanticV1FingerprintDistinctInputs(t *testing.T) {
	t.Parallel()
	baseFingerprint := mustFingerprint(t, baseIdentity())
	cases := map[string]func(FindingIdentity) FindingIdentity{
		"file":     func(id FindingIdentity) FindingIdentity { id.File = "internal/payments/store.go"; return id },
		"category": func(id FindingIdentity) FindingIdentity { id.Category = "security"; return id },
		"anchor":   func(id FindingIdentity) FindingIdentity { id.Anchor.Value = "payments.Service.Refund"; return id },
		"claim":    func(id FindingIdentity) FindingIdentity { id.Claim = "refund is not idempotent"; return id },
	}
	for name, mutate := range cases {
		t.Run("Should differ when "+name+" differs", func(t *testing.T) {
			t.Parallel()
			if mustFingerprint(t, mutate(baseIdentity())) == baseFingerprint {
				t.Fatalf("expected distinct fingerprint when %s differs", name)
			}
		})
	}
}

func TestFindingFingerprinterInterface(t *testing.T) {
	t.Parallel()
	t.Run("Should compute the same fingerprint through the interface", func(t *testing.T) {
		t.Parallel()
		var fingerprinter FindingFingerprinter = SemanticFingerprinter{}
		viaInterface, err := fingerprinter.Fingerprint(baseIdentity())
		if err != nil {
			t.Fatalf("fingerprint via interface: %v", err)
		}
		if viaInterface != mustFingerprint(t, baseIdentity()) {
			t.Fatal("interface fingerprint must match the direct method")
		}
	})
}

func TestSemanticV1ContentAnchor(t *testing.T) {
	t.Parallel()
	t.Run("Should hash the normalized content unit with its kind", func(t *testing.T) {
		t.Parallel()
		id := baseIdentity()
		id.Anchor = Anchor{Kind: AnchorContent, Unit: "func", ContentUnit: "func Charge() {}"}
		fingerprint := mustFingerprint(t, id)
		if !lowercaseHex64.MatchString(string(fingerprint)) {
			t.Fatalf("content anchor fingerprint invalid: %q", fingerprint)
		}
	})
}

func TestSemanticV1FingerprintRejections(t *testing.T) {
	t.Parallel()
	cases := map[string]FindingIdentity{
		"absolute path":      mutateIdentity(func(id *FindingIdentity) { id.File = "/etc/passwd" }),
		"traversing path":    mutateIdentity(func(id *FindingIdentity) { id.File = "../secrets.txt" }),
		"missing category":   mutateIdentity(func(id *FindingIdentity) { id.Category = "" }),
		"uppercase category": mutateIdentity(func(id *FindingIdentity) { id.Category = "Correctness" }),
		"missing claim":      mutateIdentity(func(id *FindingIdentity) { id.Claim = "  " }),
		"unreliable anchor": mutateIdentity(
			func(id *FindingIdentity) { id.Anchor = Anchor{Kind: AnchorSymbol, Value: "  "} }),
		"unknown anchor kind": mutateIdentity(
			func(id *FindingIdentity) { id.Anchor = Anchor{Kind: "guess", Value: "x"} }),
		"content anchor without unit": mutateIdentity(
			func(id *FindingIdentity) { id.Anchor = Anchor{Kind: AnchorContent, ContentUnit: "body"} }),
		"invalid utf8 claim": mutateIdentity(
			func(id *FindingIdentity) { id.Claim = string([]byte{0xff, 0xfe}) }),
		"invalid utf8 path": mutateIdentity(
			func(id *FindingIdentity) { id.File = "internal/" + string([]byte{0xff}) + ".go" }),
	}
	for name, id := range cases {
		t.Run("Should reject "+name, func(t *testing.T) {
			t.Parallel()
			if _, err := id.Fingerprint(); !errors.Is(err, ErrFindingIdentityInvalid) {
				t.Fatalf("expected ErrFindingIdentityInvalid for %s, got %v", name, err)
			}
		})
	}
}

func mutateIdentity(mutate func(*FindingIdentity)) FindingIdentity {
	id := baseIdentity()
	mutate(&id)
	return id
}
