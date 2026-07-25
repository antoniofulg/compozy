package convergence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// ReceiptSchemaVersion versions the receipt document shape.
	ReceiptSchemaVersion = "convergence-receipt/v1"
	// ReceiptFileName is the atomic receipt projection filename in a run directory.
	ReceiptFileName = "convergence-receipt.json"
)

// ErrReceiptCorrupt marks a receipt whose bytes are unreadable or whose stored
// checksum does not match its content.
var ErrReceiptCorrupt = errors.New("convergence: receipt corrupt")

// ErrReceiptDrift marks a receipt whose canonical binding (source sequence or
// content checksum) does not match the current canonical snapshot, so it must be
// rebuilt from canonical state.
var ErrReceiptDrift = errors.New("convergence: receipt drift")

// Receipt is the deterministic, human-readable, rebuildable projection of one
// convergence run. It is never a second source of truth: it binds to a canonical
// source sequence and content checksum and is rebuilt from run.db whenever it
// drifts. It excludes credentials, environment values, unredacted secrets, full
// transcripts, and absolute paths outside the workspace.
type Receipt struct {
	SchemaVersion        string                   `json:"schema_version"`
	FingerprintAlgorithm string                   `json:"fingerprint_algorithm"`
	RunID                string                   `json:"run_id"`
	ConvergenceID        string                   `json:"convergence_id"`
	RequestID            string                   `json:"request_id"`
	SegmentOrdinal       int                      `json:"segment_ordinal"`
	SourceRunID          string                   `json:"source_run_id,omitempty"`
	PreviousRunID        string                   `json:"previous_run_id,omitempty"`
	Target               ReceiptTarget            `json:"target"`
	Config               ReceiptConfig            `json:"config"`
	ConfiguredRoutes     []ReceiptConfiguredRoute `json:"configured_routes"`
	SelectedRoutes       []ReceiptSelectedRoute   `json:"selected_routes"`
	Rounds               []ReceiptRound           `json:"rounds"`
	Batches              []ReceiptBatch           `json:"batches"`
	Findings             []ReceiptFinding         `json:"findings"`
	Observations         []ReceiptObservation     `json:"observations"`
	Dispositions         []ReceiptDisposition     `json:"dispositions"`
	Verification         []ReceiptVerification    `json:"verification"`
	Approvals            []ReceiptApproval        `json:"approvals"`
	Overrides            []ReceiptOverride        `json:"overrides"`
	UnresolvedWork       []ReceiptFinding         `json:"unresolved_work"`
	Progress             ReceiptProgress          `json:"progress"`
	Commit               ReceiptCommit            `json:"commit"`
	Terminal             ReceiptTerminal          `json:"terminal"`
	SourceSeq            uint64                   `json:"source_seq"`
	GeneratedAt          time.Time                `json:"generated_at"`
	Checksum             string                   `json:"checksum"`
}

// ReceiptTarget is the workspace-relative target binding.
type ReceiptTarget struct {
	WorkspaceID    string `json:"workspace_id"`
	ExecutionScope string `json:"execution_scope"`
	TaskGroupID    string `json:"task_group_id,omitempty"`
	Branch         string `json:"branch"`
	Worktree       string `json:"worktree"`
	Snapshot       string `json:"snapshot"`
}

// ReceiptConfig is the frozen configuration projection with per-field sources.
type ReceiptConfig struct {
	Profile            string              `json:"profile"`
	ModelSetup         string              `json:"model_setup"`
	Verification       []string            `json:"verification"`
	VerificationSource string              `json:"verification_source"`
	MaxReviewRounds    int                 `json:"max_review_rounds"`
	MaxFindingAttempts int                 `json:"max_finding_attempts"`
	MaxVerifyAttempts  int                 `json:"max_verification_attempts"`
	NoProgressRounds   int                 `json:"no_progress_rounds"`
	OscillationCycles  int                 `json:"oscillation_cycles"`
	AdmissionTimeoutMs int64               `json:"review_admission_timeout_ms"`
	BaseRouteIDE       string              `json:"base_route_ide"`
	BaseRouteModel     string              `json:"base_route_model"`
	BaseRouteEffort    string              `json:"base_route_reasoning_effort"`
	LimitSources       ReceiptLimitSources `json:"limit_sources"`
	Warnings           []string            `json:"warnings,omitempty"`
}

// ReceiptLimitSources explains the provenance of every frozen limit.
type ReceiptLimitSources struct {
	MaxReviewRounds         string `json:"max_review_rounds"`
	MaxFindingAttempts      string `json:"max_finding_attempts"`
	MaxVerificationAttempts string `json:"max_verification_attempts"`
	NoProgressRounds        string `json:"no_progress_rounds"`
	ReviewAdmissionTimeout  string `json:"review_admission_timeout"`
	OscillationCycles       string `json:"oscillation_cycles"`
}

// ReceiptConfiguredRoute is one frozen primary/fallback route with provenance.
type ReceiptConfiguredRoute struct {
	Role            string              `json:"role"`
	Severity        string              `json:"severity,omitempty"`
	Primary         ReceiptRoute        `json:"primary"`
	Sources         ReceiptRouteSources `json:"sources"`
	Fallback        *ReceiptRoute       `json:"fallback,omitempty"`
	FallbackEnabled bool                `json:"fallback_enabled"`
}

// ReceiptRoute is a wire-stable runtime route.
type ReceiptRoute struct {
	IDE             string `json:"ide"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
}

// ReceiptRouteSources explains each selected route field.
type ReceiptRouteSources struct {
	IDE             string `json:"ide"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
}

// ReceiptSelectedRoute records the route actually selected for one phase.
type ReceiptSelectedRoute struct {
	PhaseID             string `json:"phase_id"`
	Role                string `json:"role"`
	Primary             string `json:"primary"`
	Selected            string `json:"selected"`
	ConfigurationSource string `json:"configuration_source"`
	FallbackReason      string `json:"fallback_reason,omitempty"`
	FallbackUsed        bool   `json:"fallback_used"`
}

// ReceiptRound is one round's admission, progress, and terminal projection.
type ReceiptRound struct {
	Number               int    `json:"number"`
	AdmittedAt           string `json:"admitted_at,omitempty"`
	Resolved             bool   `json:"resolved"`
	SeverityDecreased    bool   `json:"severity_decreased"`
	VerificationImproved bool   `json:"verification_improved"`
	NoProgressCount      int    `json:"no_progress_count"`
	OscillationCount     int    `json:"oscillation_count"`
	TerminalReason       string `json:"terminal_reason,omitempty"`
}

// ReceiptFinding is one finding's current projected state and bounded evidence.
type ReceiptFinding struct {
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Severity    string `json:"severity"`
	Attempts    int    `json:"attempts"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// ReceiptBatch is one correction checkpoint and its bounded path evidence.
type ReceiptBatch struct {
	BatchID             string   `json:"batch_id"`
	PhaseID             string   `json:"phase_id"`
	FindingFingerprints []string `json:"finding_fingerprints"`
	BeforeSnapshot      string   `json:"before_snapshot"`
	AfterSnapshot       string   `json:"after_snapshot"`
	Status              string   `json:"status"`
	AffectedPathsRef    string   `json:"affected_paths_ref,omitempty"`
}

// ReceiptObservation is one immutable finding observation.
type ReceiptObservation struct {
	ObservationID string `json:"observation_id"`
	Fingerprint   string `json:"fingerprint"`
	Snapshot      string `json:"snapshot"`
	SnapshotSeq   uint64 `json:"snapshot_seq"`
	Severity      string `json:"severity"`
	Outcome       string `json:"outcome"`
	ReviewID      string `json:"review_id"`
}

// ReceiptDisposition is one immutable finding disposition.
type ReceiptDisposition struct {
	DecisionID         string `json:"decision_id"`
	Fingerprint        string `json:"fingerprint"`
	Disposition        string `json:"disposition"`
	ActorKind          string `json:"actor_kind,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Snapshot           string `json:"snapshot"`
	SnapshotSeq        uint64 `json:"snapshot_seq"`
	RelatedFingerprint string `json:"related_fingerprint,omitempty"`
}

// ReceiptVerification is one verification result's bounded evidence.
type ReceiptVerification struct {
	VerificationID     string `json:"verification_id"`
	CommandFingerprint string `json:"command_fingerprint"`
	Snapshot           string `json:"snapshot"`
	ExitCode           *int   `json:"exit_code,omitempty"`
	Passed             bool   `json:"passed"`
	Attempt            int    `json:"attempt"`
	EvidencePath       string `json:"evidence_path,omitempty"`
}

// ReceiptApproval is one proposal and its optional decision.
type ReceiptApproval struct {
	ProposalID  string `json:"proposal_id"`
	Fingerprint string `json:"fingerprint"`
	Action      string `json:"action"`
	Decision    string `json:"decision,omitempty"`
	Reason      string `json:"reason,omitempty"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// ReceiptOverride records an effective non-base route source.
type ReceiptOverride struct {
	PhaseID string `json:"phase_id"`
	Role    string `json:"role"`
	Source  string `json:"source"`
}

// ReceiptCommit makes the non-functional auto-commit contract explicit.
type ReceiptCommit struct {
	AutoCommitEnabled bool   `json:"auto_commit_enabled"`
	State             string `json:"state"`
}

// ReceiptProgress is consumed and remaining limits plus unresolved work.
type ReceiptProgress struct {
	RoundsConsumed  int    `json:"rounds_consumed"`
	RoundsRemaining int    `json:"rounds_remaining"`
	NoProgressCount int    `json:"no_progress_count"`
	Oscillation     int    `json:"oscillation_count"`
	UnresolvedCount int    `json:"unresolved_count"`
	ResumeCursor    string `json:"resume_cursor,omitempty"`
}

// ReceiptTerminal is the terminal outcome projection.
type ReceiptTerminal struct {
	Kind            string `json:"kind"`
	Reason          string `json:"reason,omitempty"`
	ResumeAvailable bool   `json:"resume_available"`
}

// ReceiptMetadata is the durable index a global projection stores about a receipt.
type ReceiptMetadata struct {
	// RelativePath is the receipt path relative to the run directory.
	RelativePath string
	SourceSeq    uint64
	Checksum     string
}

// BuildReceipt projects a canonical snapshot into a deterministic receipt. The
// generatedAt timestamp is injected so byte output is reproducible under a fixed
// clock; the content checksum deliberately excludes generatedAt so rebuilding
// from the same canonical events always yields the same checksum. Absolute paths
// are redacted defensively so no unrestricted host path leaks into the receipt.
func BuildReceipt(snap Snapshot, generatedAt time.Time) Receipt {
	receipt := Receipt{
		SchemaVersion:        ReceiptSchemaVersion,
		FingerprintAlgorithm: FingerprintAlgorithm,
		RunID:                snap.Segment.RunID,
		ConvergenceID:        snap.ConvergenceID,
		RequestID:            snap.RequestID,
		SegmentOrdinal:       snap.Segment.Ordinal,
		SourceRunID:          snap.Segment.SourceRunID,
		PreviousRunID:        snap.Segment.PreviousRunID,
		Target:               buildReceiptTarget(snap.Target),
		Config:               buildReceiptConfig(snap.Config),
		ConfiguredRoutes:     buildReceiptConfiguredRoutes(snap.Config),
		SelectedRoutes:       buildReceiptSelectedRoutes(snap.Routes),
		Rounds:               buildReceiptRounds(snap.Rounds),
		Batches:              buildReceiptBatches(snap.Batches),
		Findings:             buildReceiptFindings(snap.Findings),
		Observations:         buildReceiptObservations(snap.Observations),
		Dispositions:         buildReceiptDispositions(snap.Dispositions),
		Verification:         buildReceiptVerification(snap.Verification),
		Approvals:            buildReceiptApprovals(snap.Approvals),
		Overrides:            buildReceiptOverrides(snap.Routes),
		UnresolvedWork:       buildReceiptFindings(unresolvedFindings(snap.Findings)),
		Progress:             buildReceiptProgress(snap),
		Commit:               buildReceiptCommit(snap.Config),
		Terminal:             buildReceiptTerminal(snap),
		SourceSeq:            snap.LastSeq,
		GeneratedAt:          generatedAt.UTC(),
	}
	receipt.Checksum = receipt.contentChecksum()
	return receipt
}

// Marshal returns the deterministic, human-readable receipt bytes with a trailing
// newline. Output is byte-stable for a fixed snapshot and clock.
func (r Receipt) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal receipt: %w", err)
	}
	return append(data, '\n'), nil
}

// contentChecksum is the lowercase hex SHA-256 over the canonical receipt content
// with the checksum and generated_at fields cleared. Excluding generated_at binds
// the checksum to canonical state alone, so a rebuild reproduces it regardless of
// when it runs.
func (r Receipt) contentChecksum() string {
	canonical := r
	canonical.Checksum = ""
	canonical.GeneratedAt = time.Time{}
	data, err := json.Marshal(canonical)
	if err != nil {
		// The receipt is composed of JSON-safe value types; marshal cannot fail.
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Validate recomputes the content checksum and rejects a corrupt receipt whose
// stored checksum does not match its content.
func (r Receipt) Validate() error {
	if r.SchemaVersion != ReceiptSchemaVersion {
		return fmt.Errorf("%w: unexpected schema %q", ErrReceiptCorrupt, r.SchemaVersion)
	}
	if r.Checksum == "" {
		return fmt.Errorf("%w: missing checksum", ErrReceiptCorrupt)
	}
	if r.contentChecksum() != r.Checksum {
		return fmt.Errorf("%w: checksum mismatch", ErrReceiptCorrupt)
	}
	return nil
}

// ParseReceipt decodes and validates receipt bytes. Malformed JSON or a checksum
// mismatch reports ErrReceiptCorrupt.
func ParseReceipt(data []byte) (Receipt, error) {
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrReceiptCorrupt, err)
	}
	if err := receipt.Validate(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func buildReceiptTarget(target TargetBinding) ReceiptTarget {
	return ReceiptTarget{
		WorkspaceID:    target.WorkspaceID,
		ExecutionScope: target.ExecutionScope,
		TaskGroupID:    target.TaskGroupID,
		Branch:         Redact(target.Branch),
		Worktree:       Redact(target.Worktree),
		Snapshot:       target.Snapshot,
	}
}

func buildReceiptConfig(cfg FrozenConfiguration) ReceiptConfig {
	return ReceiptConfig{
		Profile:            cfg.ProfileName,
		ModelSetup:         cfg.ModelSetupName,
		Verification:       redactSlice(cfg.Verification),
		VerificationSource: string(cfg.VerificationSource),
		MaxReviewRounds:    cfg.Limits.MaxReviewRounds,
		MaxFindingAttempts: cfg.Limits.MaxFindingAttempts,
		MaxVerifyAttempts:  cfg.Limits.MaxVerificationAttempts,
		NoProgressRounds:   cfg.Limits.NoProgressRounds,
		OscillationCycles:  cfg.Limits.OscillationCycles,
		AdmissionTimeoutMs: cfg.Limits.ReviewAdmissionTimeout.Milliseconds(),
		BaseRouteIDE:       cfg.BaseRoute.IDE,
		BaseRouteModel:     cfg.BaseRoute.Model,
		BaseRouteEffort:    cfg.BaseRoute.ReasoningEffort,
		LimitSources: ReceiptLimitSources{
			MaxReviewRounds:         string(cfg.LimitSources.MaxReviewRounds),
			MaxFindingAttempts:      string(cfg.LimitSources.MaxFindingAttempts),
			MaxVerificationAttempts: string(cfg.LimitSources.MaxVerificationAttempts),
			NoProgressRounds:        string(cfg.LimitSources.NoProgressRounds),
			ReviewAdmissionTimeout:  string(cfg.LimitSources.ReviewAdmissionTimeout),
			OscillationCycles:       string(cfg.LimitSources.OscillationCycles),
		},
		Warnings: append([]string(nil), cfg.Warnings...),
	}
}

func buildReceiptConfiguredRoutes(cfg FrozenConfiguration) []ReceiptConfiguredRoute {
	out := make([]ReceiptConfiguredRoute, 0, 5)
	if cfg.Review.Role != "" {
		out = append(out, receiptConfiguredRoute(cfg.Review, ""))
	}
	for _, severity := range []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow} {
		if route, ok := cfg.Correction[severity]; ok {
			out = append(out, receiptConfiguredRoute(route, string(severity)))
		}
	}
	return out
}

func receiptConfiguredRoute(route ResolvedRoute, severity string) ReceiptConfiguredRoute {
	item := ReceiptConfiguredRoute{
		Role:     string(route.Role),
		Severity: severity,
		Primary:  receiptRoute(route.Primary),
		Sources: ReceiptRouteSources{
			IDE:             string(route.Sources.IDE),
			Model:           string(route.Sources.Model),
			ReasoningEffort: string(route.Sources.ReasoningEffort),
		},
		FallbackEnabled: route.HasFallback,
	}
	if route.Fallback != nil {
		fallback := receiptRoute(*route.Fallback)
		item.Fallback = &fallback
	}
	return item
}

func receiptRoute(route Route) ReceiptRoute {
	return ReceiptRoute(route)
}

func buildReceiptSelectedRoutes(routes []RouteSelection) []ReceiptSelectedRoute {
	out := make([]ReceiptSelectedRoute, 0, len(routes))
	for _, route := range routes {
		out = append(out, ReceiptSelectedRoute{
			PhaseID:             route.PhaseID,
			Role:                route.Role,
			Primary:             route.Primary,
			Selected:            route.Selected,
			ConfigurationSource: route.ConfigurationSource,
			FallbackReason:      Redact(route.FallbackReason),
			FallbackUsed:        route.FallbackReason != "" || route.Selected != route.Primary,
		})
	}
	return out
}

func buildReceiptRounds(rounds []RoundState) []ReceiptRound {
	out := make([]ReceiptRound, 0, len(rounds))
	for i := range rounds {
		round := rounds[i]
		item := ReceiptRound{
			Number:               round.Number,
			Resolved:             round.Progress.Resolved,
			SeverityDecreased:    round.Progress.SeverityDecreased,
			VerificationImproved: round.Progress.VerificationImproved,
			NoProgressCount:      round.Progress.NoProgressCount,
			OscillationCount:     round.Progress.OscillationCount,
		}
		if !round.AdmittedAt.IsZero() {
			item.AdmittedAt = round.AdmittedAt.UTC().Format(time.RFC3339Nano)
		}
		if round.Terminal != nil {
			item.TerminalReason = string(round.Terminal.Reason)
		}
		out = append(out, item)
	}
	return out
}

func buildReceiptFindings(findings []Finding) []ReceiptFinding {
	out := make([]ReceiptFinding, 0, len(findings))
	for i := range findings {
		finding := findings[i]
		out = append(out, ReceiptFinding{
			Fingerprint: string(finding.Fingerprint),
			State:       string(finding.State),
			Severity:    string(finding.Severity),
			Attempts:    finding.Attempts,
			EvidenceRef: Redact(finding.EvidenceRef),
		})
	}
	return out
}

func buildReceiptBatches(batches []BatchState) []ReceiptBatch {
	out := make([]ReceiptBatch, 0, len(batches))
	for _, batch := range batches {
		out = append(out, ReceiptBatch{
			BatchID:             batch.BatchID,
			PhaseID:             batch.PhaseID,
			FindingFingerprints: append([]string(nil), batch.FindingFingerprints...),
			BeforeSnapshot:      batch.BeforeSnapshot,
			AfterSnapshot:       batch.AfterSnapshot,
			Status:              batch.Status,
			AffectedPathsRef:    Redact(batch.AffectedPathsRef),
		})
	}
	return out
}

func buildReceiptObservations(observations []FindingObservation) []ReceiptObservation {
	out := make([]ReceiptObservation, 0, len(observations))
	for _, observation := range observations {
		out = append(out, ReceiptObservation(observation))
	}
	return out
}

func buildReceiptDispositions(dispositions []FindingDisposition) []ReceiptDisposition {
	out := make([]ReceiptDisposition, 0, len(dispositions))
	for _, disposition := range dispositions {
		out = append(out, ReceiptDisposition{
			DecisionID:         disposition.DecisionID,
			Fingerprint:        disposition.Fingerprint,
			Disposition:        disposition.Disposition,
			ActorKind:          disposition.ActorKind,
			Reason:             Redact(disposition.Reason),
			Snapshot:           disposition.Snapshot,
			SnapshotSeq:        disposition.SnapshotSeq,
			RelatedFingerprint: disposition.RelatedFingerprint,
		})
	}
	return out
}

func buildReceiptVerification(results []VerificationResult) []ReceiptVerification {
	out := make([]ReceiptVerification, 0, len(results))
	for i := range results {
		result := results[i]
		out = append(out, ReceiptVerification{
			VerificationID:     result.VerificationID,
			CommandFingerprint: result.CommandFingerprint,
			Snapshot:           result.Snapshot,
			ExitCode:           result.ExitCode,
			Passed:             result.Passed,
			Attempt:            result.Attempt,
			EvidencePath:       Redact(result.EvidencePath),
		})
	}
	return out
}

func buildReceiptApprovals(proposals []ApprovalProposal) []ReceiptApproval {
	out := make([]ReceiptApproval, 0, len(proposals))
	for i := range proposals {
		proposal := proposals[i]
		out = append(out, ReceiptApproval{
			ProposalID:  proposal.ProposalID,
			Fingerprint: proposal.Fingerprint,
			Action:      proposal.Action,
			Decision:    proposal.Decision,
			Reason:      Redact(proposal.Reason),
			EvidenceRef: Redact(proposal.EvidenceRef),
		})
	}
	return out
}

func buildReceiptOverrides(routes []RouteSelection) []ReceiptOverride {
	out := make([]ReceiptOverride, 0)
	for _, route := range routes {
		source := route.ConfigurationSource
		if source != string(SourceResumeOverride) && source != string(SourceSeverityOverride) {
			continue
		}
		out = append(out, ReceiptOverride{
			PhaseID: route.PhaseID,
			Role:    route.Role,
			Source:  source,
		})
	}
	return out
}

func unresolvedFindings(findings []Finding) []Finding {
	out := make([]Finding, 0)
	for _, finding := range findings {
		if finding.State.IsOpen() {
			out = append(out, finding)
		}
	}
	return out
}

func buildReceiptCommit(cfg FrozenConfiguration) ReceiptCommit {
	state := "managed"
	if !cfg.AutoCommit {
		state = "uncommitted"
	}
	return ReceiptCommit{AutoCommitEnabled: cfg.AutoCommit, State: state}
}

func buildReceiptProgress(snap Snapshot) ReceiptProgress {
	consumed := len(snap.Rounds)
	remaining := snap.Config.Limits.MaxReviewRounds - consumed
	if remaining < 0 {
		remaining = 0
	}
	progress := ReceiptProgress{
		RoundsConsumed:  consumed,
		RoundsRemaining: remaining,
		UnresolvedCount: snap.UnresolvedCount(),
		ResumeCursor:    snap.Segment.ResumeCursor,
	}
	if consumed > 0 {
		last := snap.Rounds[consumed-1]
		progress.NoProgressCount = last.Progress.NoProgressCount
		progress.Oscillation = last.Progress.OscillationCount
	}
	return progress
}

func buildReceiptTerminal(snap Snapshot) ReceiptTerminal {
	if snap.Terminal == nil {
		return ReceiptTerminal{Kind: "active"}
	}
	terminal := ReceiptTerminal{Kind: string(snap.Terminal.Kind)}
	if snap.Terminal.Kind == TerminalParked {
		terminal.Reason = string(snap.Terminal.Reason)
		terminal.ResumeAvailable = true
	}
	return terminal
}

func redactSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = Redact(value)
	}
	return out
}

// ReceiptWriter builds, validates, and atomically replaces the receipt projection
// for one run directory. It never rolls back a canonical event; a receipt failure
// marks the projection incomplete for observers and is retried by Rebuild before
// the next transition. It implements the durable receipt contract from ADR-005.
type ReceiptWriter struct {
	fs    ReceiptFS
	dir   string
	clock func() time.Time
}

// ReceiptWriterOption configures a ReceiptWriter.
type ReceiptWriterOption func(*ReceiptWriter)

// WithReceiptFS injects a filesystem seam, chiefly for crash-injection tests.
func WithReceiptFS(fs ReceiptFS) ReceiptWriterOption {
	return func(w *ReceiptWriter) {
		if fs != nil {
			w.fs = fs
		}
	}
}

// WithReceiptClock injects the clock used for the generated_at timestamp.
func WithReceiptClock(clock func() time.Time) ReceiptWriterOption {
	return func(w *ReceiptWriter) {
		if clock != nil {
			w.clock = clock
		}
	}
}

// NewReceiptWriter constructs a receipt writer bound to one run directory.
func NewReceiptWriter(runDir string, opts ...ReceiptWriterOption) *ReceiptWriter {
	writer := &ReceiptWriter{
		fs:    OSReceiptFS{},
		dir:   runDir,
		clock: func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(writer)
	}
	return writer
}

// Rebuild projects the snapshot into a receipt and atomically replaces the
// on-disk receipt using temp-file, sync, rename, and directory-sync. It is
// idempotent: rebuilding from identical canonical state yields identical bytes and
// checksum. It never repeats a model, process, Git, or artifact side effect.
func (w *ReceiptWriter) Rebuild(ctx context.Context, snap Snapshot) (ReceiptMetadata, error) {
	if err := ctx.Err(); err != nil {
		return ReceiptMetadata{}, err
	}
	receipt := BuildReceipt(snap, w.clock())
	data, err := receipt.Marshal()
	if err != nil {
		return ReceiptMetadata{}, err
	}
	if err := w.writeAtomic(data); err != nil {
		return ReceiptMetadata{}, err
	}
	return ReceiptMetadata{
		RelativePath: ReceiptFileName,
		SourceSeq:    receipt.SourceSeq,
		Checksum:     receipt.Checksum,
	}, nil
}

// Validate reads the on-disk receipt and confirms it is a complete, uncorrupted
// projection of the canonical snapshot. A missing, unreadable, checksum-mismatched,
// or content-drifted receipt reports an error so the caller rebuilds from
// canonical state.
func (w *ReceiptWriter) Validate(ctx context.Context, snap Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := w.fs.ReadFile(ReceiptPath(w.dir))
	if err != nil {
		return fmt.Errorf("%w: read receipt: %v", ErrReceiptDrift, err)
	}
	stored, err := ParseReceipt(data)
	if err != nil {
		return err
	}
	expected := BuildReceipt(snap, stored.GeneratedAt)
	if stored.SourceSeq != expected.SourceSeq || stored.Checksum != expected.Checksum {
		return fmt.Errorf("%w: receipt does not match canonical seq %d", ErrReceiptDrift, snap.LastSeq)
	}
	return nil
}

func (w *ReceiptWriter) writeAtomic(data []byte) error {
	tempPath, err := w.fs.WriteTemp(w.dir, ".convergence-receipt-*.json", data)
	if err != nil {
		return fmt.Errorf("write receipt projection: %w", err)
	}
	if err := w.fs.Rename(tempPath, ReceiptPath(w.dir)); err != nil {
		replaceErr := fmt.Errorf("replace receipt projection: %w", err)
		if removeErr := w.fs.Remove(tempPath); removeErr != nil {
			return errors.Join(replaceErr, fmt.Errorf("remove receipt temp file: %w", removeErr))
		}
		return replaceErr
	}
	if err := w.fs.SyncDir(w.dir); err != nil {
		return fmt.Errorf("sync receipt directory: %w", err)
	}
	return nil
}
