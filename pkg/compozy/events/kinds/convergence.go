package kinds

// ConvergenceIdentifiers are the four canonical identifiers every convergence and
// task-continuation event carries. They flatten into the event payload object.
// request_id and actor_id are internal/confidential; resource_id and
// correlation_id locate the event within the convergence history.
type ConvergenceIdentifiers struct {
	RequestID     string `json:"request_id"`
	ActorID       string `json:"actor_id"`
	ResourceID    string `json:"resource_id"`
	CorrelationID string `json:"correlation_id"`
}

// TaskConvergenceRequestedPayload records normalized convergence activation intent
// persisted with the source task run before task execution.
type TaskConvergenceRequestedPayload struct {
	ConvergenceIdentifiers
	ActivationFingerprint string `json:"activation_fingerprint"`
	Profile               string `json:"profile"`
	ModelSetup            string `json:"model_setup"`
	Outcome               string `json:"outcome"`
}

// TaskConvergenceContinuedPayload records the deterministic continuation decision
// after a task run settles.
type TaskConvergenceContinuedPayload struct {
	ConvergenceIdentifiers
	ConvergenceRunID string `json:"convergence_run_id"`
	SourceSnapshot   string `json:"source_snapshot"`
	Outcome          string `json:"outcome"`
}

// ConvergencePreflightCompletedPayload records the frozen preflight resolution.
type ConvergencePreflightCompletedPayload struct {
	ConvergenceIdentifiers
	TargetSnapshot    string   `json:"target_snapshot"`
	ConfigFingerprint string   `json:"config_fingerprint"`
	RouteSummary      string   `json:"route_summary"`
	Warnings          []string `json:"warnings"`
	Outcome           string   `json:"outcome"`
}

// ConvergencePhaseStartedPayload records a claimed snapshot-bound phase start.
type ConvergencePhaseStartedPayload struct {
	ConvergenceIdentifiers
	Phase    string `json:"phase"`
	Round    int    `json:"round"`
	BatchID  string `json:"batch_id,omitempty"`
	Attempt  int    `json:"attempt"`
	Snapshot string `json:"snapshot"`
	Outcome  string `json:"outcome"`
}

// ConvergenceRouteSelectedPayload records a frozen route selection for one phase.
type ConvergenceRouteSelectedPayload struct {
	ConvergenceIdentifiers
	Role                string `json:"role"`
	Primary             string `json:"primary"`
	Selected            string `json:"selected"`
	ConfigurationSource string `json:"configuration_source"`
	FallbackReason      string `json:"fallback_reason,omitempty"`
	Outcome             string `json:"outcome"`
}

// ConvergenceVerificationCompletedPayload records an authoritative verification result.
type ConvergenceVerificationCompletedPayload struct {
	ConvergenceIdentifiers
	CommandFingerprint string `json:"command_fingerprint"`
	Snapshot           string `json:"snapshot"`
	ExitCode           *int   `json:"exit_code,omitempty"`
	EvidencePath       string `json:"evidence_path"`
	Outcome            string `json:"outcome"`
}

// ConvergenceReviewCompletedPayload records a validated structured review result.
type ConvergenceReviewCompletedPayload struct {
	ConvergenceIdentifiers
	Snapshot         string `json:"snapshot"`
	FindingCount     int    `json:"finding_count"`
	ArtifactPath     string `json:"artifact_path"`
	ReadOnlyEnforced bool   `json:"read_only_enforced"`
	Outcome          string `json:"outcome"`
}

// ConvergenceFindingChangedPayload records one ordered finding state transition.
type ConvergenceFindingChangedPayload struct {
	ConvergenceIdentifiers
	State             string `json:"state"`
	Severity          string `json:"severity"`
	Snapshot          string `json:"snapshot"`
	EvidenceRef       string `json:"evidence_ref"`
	DispositionReason string `json:"disposition_reason,omitempty"`
	Outcome           string `json:"outcome"`
}

// ConvergenceBatchCompletedPayload records a durable correction batch checkpoint.
type ConvergenceBatchCompletedPayload struct {
	ConvergenceIdentifiers
	FindingFingerprints []string `json:"finding_fingerprints"`
	BeforeSnapshot      string   `json:"before_snapshot"`
	AfterSnapshot       string   `json:"after_snapshot"`
	AffectedPathsRef    string   `json:"affected_paths_ref"`
	Outcome             string   `json:"outcome"`
}

// ConvergenceProgressEvaluatedPayload records one round's progress evaluation.
type ConvergenceProgressEvaluatedPayload struct {
	ConvergenceIdentifiers
	Resolved             bool   `json:"resolved"`
	SeverityDecreased    bool   `json:"severity_decreased"`
	VerificationImproved bool   `json:"verification_improved"`
	NoProgressCount      int    `json:"no_progress_count"`
	OscillationCount     int    `json:"oscillation_count"`
	Outcome              string `json:"outcome"`
}

// ConvergenceApprovalRequestedPayload records a protected-action approval proposal.
type ConvergenceApprovalRequestedPayload struct {
	ConvergenceIdentifiers
	ProposalFingerprint string `json:"proposal_fingerprint"`
	Snapshot            string `json:"snapshot"`
	Action              string `json:"action"`
	EvidenceRef         string `json:"evidence_ref"`
	Outcome             string `json:"outcome"`
}

// ConvergenceApprovalDecidedPayload records the user's decision on a proposal.
type ConvergenceApprovalDecidedPayload struct {
	ConvergenceIdentifiers
	ProposalFingerprint string `json:"proposal_fingerprint"`
	Snapshot            string `json:"snapshot"`
	Decision            string `json:"decision"`
	Reason              string `json:"reason"`
	Outcome             string `json:"outcome"`
}

// ConvergenceSegmentParkedPayload records the durable parked terminal of a segment
// before the public run.parked event is published.
type ConvergenceSegmentParkedPayload struct {
	ConvergenceIdentifiers
	Reason          string `json:"reason"`
	Snapshot        string `json:"snapshot"`
	UnresolvedCount int    `json:"unresolved_count"`
	ReceiptPath     string `json:"receipt_path"`
	ResumeAvailable bool   `json:"resume_available"`
	Outcome         string `json:"outcome"`
}

// ConvergenceSegmentCompletedPayload records the durable clean terminal of a segment.
type ConvergenceSegmentCompletedPayload struct {
	ConvergenceIdentifiers
	Snapshot       string `json:"snapshot"`
	VerificationID string `json:"verification_id"`
	ReviewID       string `json:"review_id"`
	ReceiptPath    string `json:"receipt_path"`
	Outcome        string `json:"outcome"`
}
