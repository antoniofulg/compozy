package convergence

// FindingFingerprinter computes the semantic identity of a finding. It is the
// small policy interface later slices depend on so finding identity stays
// deterministic and daemon-owned rather than derived from chat history.
type FindingFingerprinter interface {
	Fingerprint(FindingIdentity) (FindingFingerprint, error)
}

// SemanticFingerprinter is the semantic-v1 implementation of FindingFingerprinter.
type SemanticFingerprinter struct{}

// Fingerprint computes the semantic-v1 fingerprint for one identity.
func (SemanticFingerprinter) Fingerprint(id FindingIdentity) (FindingFingerprint, error) {
	return id.Fingerprint()
}

var _ FindingFingerprinter = SemanticFingerprinter{}
