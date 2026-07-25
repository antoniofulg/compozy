package convergence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ReviewArtifactSchemaVersion versions the review artifact document shape.
	ReviewArtifactSchemaVersion = "convergence-review/v1"
)

// ErrReviewArtifactCorrupt marks a review artifact whose bytes are unreadable or
// whose stored checksum does not match its content.
var ErrReviewArtifactCorrupt = errors.New("convergence: review artifact corrupt")

// ReviewArtifact is the daemon-authored, checksummed projection of one review's
// structured result, bound to the daemon's captured snapshot. It is the canonical
// review evidence; the reviewer session never writes it. Finding prose is
// retained here because the artifact is the daemon's own evidence file, not an
// event payload; identity fingerprints let observations bind back to it.
type ReviewArtifact struct {
	SchemaVersion        string                  `json:"schema_version"`
	FingerprintAlgorithm string                  `json:"fingerprint_algorithm"`
	ReviewID             string                  `json:"review_id"`
	RoundNumber          int                     `json:"round_number"`
	Snapshot             string                  `json:"snapshot"`
	Outcome              string                  `json:"outcome"`
	Explanation          string                  `json:"explanation"`
	Findings             []ReviewArtifactFinding `json:"findings"`
	GeneratedAt          time.Time               `json:"generated_at"`
	Checksum             string                  `json:"checksum"`
}

// ReviewArtifactFinding is one finding's daemon-bound identity and observation
// evidence within the artifact.
type ReviewArtifactFinding struct {
	Fingerprint        string `json:"fingerprint"`
	Severity           string `json:"severity"`
	Outcome            string `json:"outcome"`
	Line               int    `json:"line,omitempty"`
	Column             int    `json:"column,omitempty"`
	Evidence           string `json:"evidence"`
	Disposition        string `json:"disposition,omitempty"`
	RelatedFingerprint string `json:"related_fingerprint,omitempty"`
}

// ReviewArtifactMetadata is the durable index a caller records about a written
// review artifact.
type ReviewArtifactMetadata struct {
	// RelativePath is the artifact path relative to the round artifact directory.
	RelativePath string
	Snapshot     string
	Checksum     string
}

// BuildReviewArtifact validates the structured review result, binds it to the
// daemon's expected snapshot, and projects it into a checksummed artifact. It
// rejects a result bound to a superseded snapshot as stale so a review of an old
// snapshot can never author current evidence. The generatedAt timestamp is
// excluded from the checksum so a rebuild from identical canonical state yields
// the identical checksum.
func BuildReviewArtifact(
	result ReviewResult,
	expectedSnapshot string,
	roundNumber int,
	generatedAt time.Time,
) (ReviewArtifact, error) {
	if err := result.Validate(); err != nil {
		return ReviewArtifact{}, err
	}
	expected := strings.TrimSpace(expectedSnapshot)
	if expected == "" {
		return ReviewArtifact{}, fmt.Errorf("%w: expected snapshot is required", ErrReviewInvalid)
	}
	if strings.TrimSpace(result.Snapshot) != expected {
		return ReviewArtifact{}, fmt.Errorf(
			"%w: review snapshot %q does not match current %q",
			ErrObservationStale, result.Snapshot, expected,
		)
	}
	findings, err := buildReviewArtifactFindings(result)
	if err != nil {
		return ReviewArtifact{}, err
	}
	artifact := ReviewArtifact{
		SchemaVersion:        ReviewArtifactSchemaVersion,
		FingerprintAlgorithm: FingerprintAlgorithm,
		ReviewID:             result.ReviewID,
		RoundNumber:          roundNumber,
		Snapshot:             expected,
		Outcome:              string(result.Outcome),
		Explanation:          result.Explanation,
		Findings:             findings,
		GeneratedAt:          generatedAt.UTC(),
	}
	artifact.Checksum = artifact.contentChecksum()
	return artifact, nil
}

func buildReviewArtifactFindings(result ReviewResult) ([]ReviewArtifactFinding, error) {
	out := make([]ReviewArtifactFinding, 0, len(result.Findings))
	for i := range result.Findings {
		fingerprint, err := result.Findings[i].Identity.Fingerprint()
		if err != nil {
			return nil, err
		}
		finding := ReviewArtifactFinding{
			Fingerprint: string(fingerprint),
			Severity:    string(result.Findings[i].Severity),
			Outcome:     string(result.Findings[i].Outcome),
			Line:        result.Findings[i].Line,
			Column:      result.Findings[i].Column,
			Evidence:    result.Findings[i].Evidence,
		}
		if disposition := result.Findings[i].Disposition; disposition != nil {
			finding.Disposition = string(disposition.Type)
			finding.RelatedFingerprint = string(disposition.RelatedFingerprint)
		}
		out = append(out, finding)
	}
	return out, nil
}

// Marshal returns deterministic, human-readable artifact bytes with a trailing
// newline. Output is byte-stable for a fixed result and clock.
func (a ReviewArtifact) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal review artifact: %w", err)
	}
	return append(data, '\n'), nil
}

// contentChecksum is the lowercase hex SHA-256 over the artifact content with the
// checksum and generated_at fields cleared, binding the checksum to the review
// result alone.
func (a ReviewArtifact) contentChecksum() string {
	canonical := a
	canonical.Checksum = ""
	canonical.GeneratedAt = time.Time{}
	data, err := json.Marshal(canonical)
	if err != nil {
		// The artifact is composed of JSON-safe value types; marshal cannot fail.
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Validate recomputes the content checksum and rejects a corrupt artifact.
func (a ReviewArtifact) Validate() error {
	if a.SchemaVersion != ReviewArtifactSchemaVersion {
		return fmt.Errorf("%w: unexpected schema %q", ErrReviewArtifactCorrupt, a.SchemaVersion)
	}
	if a.Checksum == "" {
		return fmt.Errorf("%w: missing checksum", ErrReviewArtifactCorrupt)
	}
	if a.contentChecksum() != a.Checksum {
		return fmt.Errorf("%w: checksum mismatch", ErrReviewArtifactCorrupt)
	}
	return nil
}

// ErrReviewArtifactTooLarge marks a manual review artifact that exceeds the read
// cap and must remain historical evidence addressable only by reference rather
// than entering current finding state.
var ErrReviewArtifactTooLarge = errors.New("convergence: review artifact too large")

// ManualFindings maps the artifact's findings to manual-finding seeds bound to the
// artifact snapshot. A finding the review reported resolved is marked resolved so
// only unresolved same-snapshot findings can seed a standalone run.
func (a ReviewArtifact) ManualFindings() []ManualFinding {
	out := make([]ManualFinding, 0, len(a.Findings))
	for i := range a.Findings {
		out = append(out, ManualFinding{
			Fingerprint: FindingFingerprint(a.Findings[i].Fingerprint),
			Snapshot:    a.Snapshot,
			Resolved:    a.Findings[i].Outcome == string(ObservationResolved),
		})
	}
	return out
}

// ReadManualReviewArtifact reads and validates a manual review artifact bounded by
// maxBytes. An artifact larger than the cap is left unparsed and reported with
// ErrReviewArtifactTooLarge so it stays historical evidence addressable by path; a
// missing file returns an os.ErrNotExist-wrapped error. A non-positive maxBytes
// disables the size cap.
func ReadManualReviewArtifact(path string, maxBytes int64) (ReviewArtifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ReviewArtifact{}, fmt.Errorf("stat review artifact: %w", err)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return ReviewArtifact{}, fmt.Errorf(
			"%w: %d bytes exceeds %d", ErrReviewArtifactTooLarge, info.Size(), maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReviewArtifact{}, fmt.Errorf("read review artifact: %w", err)
	}
	return ParseReviewArtifact(data)
}

// ParseReviewArtifact decodes and validates review artifact bytes.
func ParseReviewArtifact(data []byte) (ReviewArtifact, error) {
	var artifact ReviewArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return ReviewArtifact{}, fmt.Errorf("%w: %v", ErrReviewArtifactCorrupt, err)
	}
	if err := artifact.Validate(); err != nil {
		return ReviewArtifact{}, err
	}
	return artifact, nil
}

// ReviewArtifactWriter builds and atomically writes the canonical review artifact
// for one round directory. It is the daemon's sole review-artifact authority: the
// reviewer session returns a structured ReviewResult and never touches this
// writer. It reuses the crash-safe temp-file, sync, rename, directory-sync
// sequence so a reader sees either the prior or the new complete artifact.
type ReviewArtifactWriter struct {
	fs    ReceiptFS
	dir   string
	clock func() time.Time
}

// ReviewArtifactWriterOption configures a ReviewArtifactWriter.
type ReviewArtifactWriterOption func(*ReviewArtifactWriter)

// WithReviewArtifactFS injects a filesystem seam for crash-injection tests.
func WithReviewArtifactFS(fs ReceiptFS) ReviewArtifactWriterOption {
	return func(w *ReviewArtifactWriter) {
		if fs != nil {
			w.fs = fs
		}
	}
}

// WithReviewArtifactClock injects the clock used for the generated_at timestamp.
func WithReviewArtifactClock(clock func() time.Time) ReviewArtifactWriterOption {
	return func(w *ReviewArtifactWriter) {
		if clock != nil {
			w.clock = clock
		}
	}
}

// NewReviewArtifactWriter constructs a writer bound to one round artifact
// directory.
func NewReviewArtifactWriter(dir string, opts ...ReviewArtifactWriterOption) *ReviewArtifactWriter {
	writer := &ReviewArtifactWriter{
		fs:    OSReceiptFS{},
		dir:   dir,
		clock: func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(writer)
	}
	return writer
}

// ReviewArtifactFileName returns the artifact filename for a review.
func ReviewArtifactFileName(reviewID string) string {
	return "review-" + reviewID + ".json"
}

// Write validates the review result against the expected snapshot, builds the
// checksummed artifact, and atomically replaces it on disk. It returns the
// durable metadata a caller indexes. It never repeats a model or process side
// effect; rewriting from an identical result yields identical bytes.
func (w *ReviewArtifactWriter) Write(
	ctx context.Context,
	result ReviewResult,
	expectedSnapshot string,
	roundNumber int,
) (ReviewArtifactMetadata, error) {
	if err := ctx.Err(); err != nil {
		return ReviewArtifactMetadata{}, err
	}
	artifact, err := BuildReviewArtifact(result, expectedSnapshot, roundNumber, w.clock())
	if err != nil {
		return ReviewArtifactMetadata{}, err
	}
	data, err := artifact.Marshal()
	if err != nil {
		return ReviewArtifactMetadata{}, err
	}
	name := ReviewArtifactFileName(result.ReviewID)
	if err := w.writeAtomic(name, data); err != nil {
		return ReviewArtifactMetadata{}, err
	}
	return ReviewArtifactMetadata{
		RelativePath: name,
		Snapshot:     artifact.Snapshot,
		Checksum:     artifact.Checksum,
	}, nil
}

func (w *ReviewArtifactWriter) writeAtomic(name string, data []byte) error {
	dest := filepath.Join(w.dir, name)
	tempPath, err := w.fs.WriteTemp(w.dir, ".convergence-review-*.json", data)
	if err != nil {
		return fmt.Errorf("write review artifact: %w", err)
	}
	if err := w.fs.Rename(tempPath, dest); err != nil {
		replaceErr := fmt.Errorf("replace review artifact: %w", err)
		if removeErr := w.fs.Remove(tempPath); removeErr != nil {
			return errors.Join(replaceErr, fmt.Errorf("remove review artifact temp file: %w", removeErr))
		}
		return replaceErr
	}
	if err := w.fs.SyncDir(w.dir); err != nil {
		return fmt.Errorf("sync review artifact directory: %w", err)
	}
	return nil
}
