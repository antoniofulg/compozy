package contract

import "time"

// Convergence approval decisions. Approval never resumes the run; a separate
// resume request creates the next segment.
const (
	ConvergenceDecisionApprove = "approve"
	ConvergenceDecisionReject  = "reject"
)

// ConvergenceStartRequest is the standalone `reviews converge` activation request.
// The server derives the target snapshot and every activation and configuration
// fingerprint; the client never supplies actor identity or snapshot digests.
type ConvergenceStartRequest struct {
	Workspace        string `json:"workspace"`
	TaskGroupID      string `json:"task_group_id,omitempty"`
	Profile          string `json:"profile,omitempty"`
	ModelSetup       string `json:"model_setup,omitempty"`
	NewRun           bool   `json:"new_run,omitempty"`
	PresentationMode string `json:"presentation_mode,omitempty"`
	ClientRequestID  string `json:"client_request_id,omitempty"`
}

// ApprovalDecisionRequest carries a protected-action decision. Actor identity is
// derived from the authenticated local request, never from this body. The
// expected fingerprint and snapshot bind the decision to the exact proposal and
// workspace state it was shown against.
type ApprovalDecisionRequest struct {
	ProposalID          string `json:"proposal_id"`
	Decision            string `json:"decision"`
	Reason              string `json:"reason"`
	ExpectedFingerprint string `json:"expected_fingerprint,omitempty"`
	ExpectedSnapshot    string `json:"expected_snapshot,omitempty"`
	ClientRequestID     string `json:"client_request_id,omitempty"`
}

// ConvergencePhaseRouteOverride is one prospective resume route override. Empty
// fields inherit the frozen route for that phase role.
type ConvergencePhaseRouteOverride struct {
	IDE             string `json:"ide,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// ConvergenceResumeRequest resumes a parked segment into exactly one new linked
// segment. ExpectedCursor is the resume cursor the caller observed; a mismatch is
// rejected as stale. Overrides are prospective and never mutate consumed history.
type ConvergenceResumeRequest struct {
	ExpectedCursor     string                         `json:"expected_cursor"`
	Profile            string                         `json:"profile,omitempty"`
	ModelSetup         string                         `json:"model_setup,omitempty"`
	ReviewOverride     *ConvergencePhaseRouteOverride `json:"review_override,omitempty"`
	CorrectionOverride *ConvergencePhaseRouteOverride `json:"correction_override,omitempty"`
	PresentationMode   string                         `json:"presentation_mode,omitempty"`
	ClientRequestID    string                         `json:"client_request_id,omitempty"`
}

// ConvergenceSnapshotVersion is the schema version of the projected convergence
// snapshot. A UI or client that does not understand this version must refuse to
// infer clean or parked state rather than misread additive fields.
const ConvergenceSnapshotVersion = 1

// ConvergenceSnapshotResponse is the read model for one convergence run. It is the
// JSON-tagged transport equivalent of the internal convergence.Snapshot; it never
// exports absolute paths, finding prose, model output, or stored authorization.
type ConvergenceSnapshotResponse struct {
	Convergence ConvergenceSnapshot `json:"convergence"`
}

// ConvergenceSnapshot is the bounded, versioned projected state of one convergence
// run. Histories are bounded, but the current actionable findings, active approval,
// terminal reason, unresolved work, cursor, and child relations are always retained
// so the UI never has to reconstruct truth from model prose or full journal scans.
type ConvergenceSnapshot struct {
	Version         int                       `json:"version"`
	ConvergenceID   string                    `json:"convergence_id"`
	RequestID       string                    `json:"request_id,omitempty"`
	Segment         ConvergenceSegment        `json:"segment"`
	Target          ConvergenceTarget         `json:"target"`
	Config          ConvergenceConfigSummary  `json:"config"`
	Phase           ConvergencePhase          `json:"phase"`
	Conditions      []ConvergenceCondition    `json:"conditions,omitempty"`
	Routes          []ConvergenceRoute        `json:"routes,omitempty"`
	Rounds          []ConvergenceRound        `json:"rounds,omitempty"`
	Batches         []ConvergenceBatch        `json:"batches,omitempty"`
	Findings        []ConvergenceFinding      `json:"findings,omitempty"`
	Verification    []ConvergenceVerification `json:"verification,omitempty"`
	Approvals       []ConvergenceApproval     `json:"approvals,omitempty"`
	Terminal        *ConvergenceTerminal      `json:"terminal,omitempty"`
	Handoff         ConvergenceHandoff        `json:"handoff"`
	Relations       []ConvergenceRelation     `json:"relations,omitempty"`
	Page            ConvergencePage           `json:"page"`
	UnresolvedCount int                       `json:"unresolved_count"`
	LastSeq         uint64                    `json:"last_seq"`
}

// Convergence clean-gate condition kinds. These six gates are the deterministic
// path to a clean outcome; the UI renders each one's projected status.
const (
	ConvergenceConditionInitialVerification = "initial_verification"
	ConvergenceConditionActionableFindings  = "actionable_findings"
	ConvergenceConditionWorkspaceStable     = "workspace_stable"
	ConvergenceConditionCleanReview         = "clean_review"
	ConvergenceConditionCurrentVerification = "current_verification"
	ConvergenceConditionApprovalRequired    = "approval_required"
)

// Convergence condition statuses. Met is a satisfied gate, Blocked is an actively
// unsatisfied gate, and Pending is a gate not yet evaluated for the current round.
const (
	ConvergenceConditionMet     = "met"
	ConvergenceConditionBlocked = "blocked"
	ConvergenceConditionPending = "pending"
)

// ConvergenceCondition is one projected clean-gate condition. It is derived from
// canonical snapshot state, never from displayed counts or model prose.
type ConvergenceCondition struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ConvergenceHandoff is the preserved branch, worktree, snapshot, and resume state
// visible for an active or terminal segment. Every path is workspace-relative.
type ConvergenceHandoff struct {
	Branch          string `json:"branch,omitempty"`
	Worktree        string `json:"worktree,omitempty"`
	Snapshot        string `json:"snapshot,omitempty"`
	Dirty           bool   `json:"dirty"`
	AutoCommit      bool   `json:"auto_commit"`
	UnresolvedCount int    `json:"unresolved_count"`
	TerminalKind    string `json:"terminal_kind,omitempty"`
	TerminalReason  string `json:"terminal_reason,omitempty"`
	ResumeCursor    string `json:"resume_cursor,omitempty"`
	ResumeAvailable bool   `json:"resume_available"`
	ReceiptPath     string `json:"receipt_path,omitempty"`
}

// Convergence relation kinds locate a segment within its convergence lineage and
// any child runs.
const (
	ConvergenceRelationSource       = "source"
	ConvergenceRelationPrevious     = "previous"
	ConvergenceRelationContinuation = "continuation"
	ConvergenceRelationChild        = "child"
)

// ConvergenceRelation is one durable relation to another run. Relations are always
// retained in full; bounding never drops a child or lineage link.
type ConvergenceRelation struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
}

// ConvergenceSectionPage reports the bounding of one history section: how many
// entries exist, how many the snapshot carries, and whether the tail was truncated.
type ConvergenceSectionPage struct {
	Total     int  `json:"total"`
	Shown     int  `json:"shown"`
	Truncated bool `json:"truncated"`
}

// ConvergencePage is the bounded-history pagination state of a snapshot. Cursor is
// the canonical sequence up to which the snapshot reflects state; a paginated event
// reader continues from it without dropping ordering.
type ConvergencePage struct {
	Findings     ConvergenceSectionPage `json:"findings"`
	Rounds       ConvergenceSectionPage `json:"rounds"`
	Batches      ConvergenceSectionPage `json:"batches"`
	Verification ConvergenceSectionPage `json:"verification"`
	Cursor       string                 `json:"cursor,omitempty"`
}

// ConvergenceSegment is one active or terminal convergence run segment.
type ConvergenceSegment struct {
	RunID         string               `json:"run_id"`
	Ordinal       int                  `json:"ordinal"`
	PreviousRunID string               `json:"previous_run_id,omitempty"`
	SourceRunID   string               `json:"source_run_id,omitempty"`
	State         string               `json:"state"`
	ResumeCursor  string               `json:"resume_cursor,omitempty"`
	ResumeClaimed bool                 `json:"resume_claimed"`
	Terminal      *ConvergenceTerminal `json:"terminal,omitempty"`
}

// ConvergenceTarget is the frozen recognized-work binding. Every path is
// workspace-relative; absolute host paths never enter the snapshot.
type ConvergenceTarget struct {
	WorkspaceID    string `json:"workspace_id"`
	ExecutionScope string `json:"execution_scope,omitempty"`
	TaskGroupID    string `json:"task_group_id,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Worktree       string `json:"worktree,omitempty"`
	Snapshot       string `json:"snapshot,omitempty"`
}

// ConvergenceConfigSummary is the bounded public view of the frozen configuration.
type ConvergenceConfigSummary struct {
	Profile    string            `json:"profile"`
	ModelSetup string            `json:"model_setup"`
	AutoCommit bool              `json:"auto_commit"`
	Limits     ConvergenceLimits `json:"limits"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// ConvergenceLimits is the frozen limit profile.
type ConvergenceLimits struct {
	MaxReviewRounds         int    `json:"max_review_rounds"`
	MaxFindingAttempts      int    `json:"max_finding_attempts"`
	MaxVerificationAttempts int    `json:"max_verification_attempts"`
	NoProgressRounds        int    `json:"no_progress_rounds"`
	OscillationCycles       int    `json:"oscillation_cycles"`
	ReviewAdmissionTimeout  string `json:"review_admission_timeout"`
}

// ConvergencePhase is the current phase projection of a segment.
type ConvergencePhase struct {
	PhaseID  string `json:"phase_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Round    int    `json:"round"`
	BatchID  string `json:"batch_id,omitempty"`
	Attempt  int    `json:"attempt"`
	Snapshot string `json:"snapshot,omitempty"`
	State    string `json:"state,omitempty"`
}

// ConvergenceRoute records the frozen and selected route for one phase role.
type ConvergenceRoute struct {
	PhaseID             string `json:"phase_id"`
	Role                string `json:"role"`
	Primary             string `json:"primary"`
	Selected            string `json:"selected"`
	ConfigurationSource string `json:"configuration_source"`
	FallbackReason      string `json:"fallback_reason,omitempty"`
}

// ConvergenceRound is one review round's admission, progress, and terminal.
type ConvergenceRound struct {
	RoundID    string               `json:"round_id"`
	Number     int                  `json:"number"`
	AdmittedAt time.Time            `json:"admitted_at"`
	Progress   ConvergenceProgress  `json:"progress"`
	Terminal   *ConvergenceTerminal `json:"terminal,omitempty"`
}

// ConvergenceProgress carries a round's measured convergence counters.
type ConvergenceProgress struct {
	Resolved             bool `json:"resolved"`
	SeverityDecreased    bool `json:"severity_decreased"`
	VerificationImproved bool `json:"verification_improved"`
	NoProgressCount      int  `json:"no_progress_count"`
	OscillationCount     int  `json:"oscillation_count"`
}

// ConvergenceBatch is one durable correction checkpoint.
type ConvergenceBatch struct {
	BatchID             string   `json:"batch_id"`
	PhaseID             string   `json:"phase_id"`
	FindingFingerprints []string `json:"finding_fingerprints,omitempty"`
	BeforeSnapshot      string   `json:"before_snapshot,omitempty"`
	AfterSnapshot       string   `json:"after_snapshot,omitempty"`
	Status              string   `json:"status"`
	AffectedPathsRef    string   `json:"affected_paths_ref,omitempty"`
}

// ConvergenceFinding is the current projected state of one semantic finding. The
// evidence reference is run-relative and never contains finding prose.
type ConvergenceFinding struct {
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Severity    string `json:"severity"`
	SnapshotSeq uint64 `json:"snapshot_seq"`
	Attempts    int    `json:"attempts"`
	FirstSeq    uint64 `json:"first_seq"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// ConvergenceVerification is one authoritative verification outcome. Complete raw
// output is referenced by run-relative evidence path.
type ConvergenceVerification struct {
	VerificationID     string `json:"verification_id"`
	PhaseID            string `json:"phase_id"`
	CommandFingerprint string `json:"command_fingerprint"`
	Snapshot           string `json:"snapshot,omitempty"`
	ExitCode           *int   `json:"exit_code,omitempty"`
	Passed             bool   `json:"passed"`
	Attempt            int    `json:"attempt"`
	EvidencePath       string `json:"evidence_path,omitempty"`
}

// ConvergenceApproval is one protected-action proposal and its optional decision.
type ConvergenceApproval struct {
	ProposalID  string `json:"proposal_id"`
	Fingerprint string `json:"fingerprint"`
	Action      string `json:"action"`
	Snapshot    string `json:"snapshot,omitempty"`
	Decision    string `json:"decision,omitempty"`
	Reason      string `json:"reason,omitempty"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// ConvergenceTerminal is the public projection of a convergence terminal outcome.
type ConvergenceTerminal struct {
	Kind      string `json:"kind"`
	Reason    string `json:"reason,omitempty"`
	Status    string `json:"status"`
	EventKind string `json:"event_kind"`
}
