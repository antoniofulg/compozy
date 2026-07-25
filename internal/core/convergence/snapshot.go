package convergence

import "time"

// Snapshot is the canonical projected state of one convergence run, reconstructed
// from the ordered convergence journal and run.db. It is the single input the
// receipt and every read model project from; it introduces no second authority.
type Snapshot struct {
	ConvergenceID string
	RequestID     string
	Segment       Segment
	Target        TargetBinding
	Config        FrozenConfiguration
	Phase         PhaseState
	Routes        []RouteSelection
	Rounds        []RoundState
	Batches       []BatchState
	Findings      []Finding
	Observations  []FindingObservation
	Dispositions  []FindingDisposition
	Verification  []VerificationResult
	Approvals     []ApprovalProposal
	Terminal      *TerminalOutcome
	// LastSeq is the highest canonical sequence the snapshot reflects. It binds a
	// receipt or rebuild to the exact canonical position it was projected from.
	LastSeq uint64
}

// RouteSelection records the frozen and selected route for one phase role.
type RouteSelection struct {
	PhaseID             string
	Role                string
	Primary             string
	Selected            string
	ConfigurationSource string
	FallbackReason      string
}

// BatchState is one durable correction checkpoint.
type BatchState struct {
	BatchID             string
	PhaseID             string
	FindingFingerprints []string
	BeforeSnapshot      string
	AfterSnapshot       string
	Status              string
	AffectedPathsRef    string
}

// FindingObservation is one immutable snapshot-bound finding observation.
type FindingObservation struct {
	ObservationID string
	Fingerprint   string
	Snapshot      string
	SnapshotSeq   uint64
	Severity      string
	Outcome       string
	ReviewID      string
}

// FindingDisposition is one immutable invalid, duplicate, or waiver decision.
type FindingDisposition struct {
	DecisionID         string
	Fingerprint        string
	Disposition        string
	ActorKind          string
	Reason             string
	Snapshot           string
	SnapshotSeq        uint64
	RelatedFingerprint string
}

// Segment is one active or terminal convergence run segment. Only one segment for
// a convergence identity may be active; a parked or clean segment is terminal and
// immutable, and a legal resume links a new segment through PreviousRunID.
type Segment struct {
	RunID         string
	Ordinal       int
	PreviousRunID string
	SourceRunID   string
	State         SegmentState
	// ResumeCursor is the opaque cursor a legal resume must present. It is claimed
	// at most once; ResumeClaimed reports whether it has already been consumed.
	ResumeCursor  string
	ResumeClaimed bool
	Terminal      *TerminalOutcome
}

// TargetBinding is the frozen recognized-work target a convergence run operates on.
// Every path is workspace-relative; absolute host paths never enter the snapshot.
type TargetBinding struct {
	WorkspaceID    string
	ExecutionScope string
	TaskGroupID    string
	Branch         string
	Worktree       string
	// Snapshot is the frozen Git snapshot digest bound at preflight.
	Snapshot string
}

// PhaseState is the current phase projection of a segment.
type PhaseState struct {
	PhaseID  string
	Kind     PhaseKind
	Round    int
	BatchID  string
	Attempt  int
	Snapshot string
	State    SegmentState
}

// ProgressState carries a round's measured convergence progress counters.
type ProgressState struct {
	Resolved             bool
	SeverityDecreased    bool
	VerificationImproved bool
	NoProgressCount      int
	OscillationCount     int
}

// RoundState is one review round's admission, progress, and terminal evaluation.
type RoundState struct {
	RoundID    string
	Number     int
	AdmittedAt time.Time
	Progress   ProgressState
	Terminal   *TerminalOutcome
}

// Finding is the current projected state and counters of one semantic finding.
type Finding struct {
	Fingerprint FindingFingerprint
	State       FindingState
	Severity    Severity
	SnapshotSeq uint64
	Attempts    int
	FirstSeq    uint64
	// EvidenceRef is a run-relative evidence path; it never contains finding prose.
	EvidenceRef string
}

// VerificationResult is one authoritative verification outcome and its evidence.
type VerificationResult struct {
	VerificationID     string
	PhaseID            string
	CommandFingerprint string
	Snapshot           string
	ExitCode           *int
	Passed             bool
	Attempt            int
	// EvidencePath references complete raw output by run-relative path.
	EvidencePath string
}

// ApprovalProposal is one protected-action proposal and its optional user decision.
type ApprovalProposal struct {
	ProposalID  string
	Fingerprint string
	Action      string
	Snapshot    string
	// Decision is empty while pending, then approve or reject.
	Decision    string
	Reason      string
	EvidenceRef string
}

// UnresolvedCount reports how many findings still count as accepted actionable work.
func (s Snapshot) UnresolvedCount() int {
	count := 0
	for i := range s.Findings {
		if s.Findings[i].State.IsOpen() {
			count++
		}
	}
	return count
}

// CurrentVerification returns the most recent verification result, if any.
func (s Snapshot) CurrentVerification() (VerificationResult, bool) {
	if len(s.Verification) == 0 {
		return VerificationResult{}, false
	}
	return s.Verification[len(s.Verification)-1], true
}

// ResumeRequest carries the durable inputs a store needs to claim a parked
// segment's resume cursor exactly once and link a new active segment to the same
// convergence identity. It is a pure value with no transport dependency; the
// coordinator (a later task) fills it and the store enforces the single-claim
// invariant. Overrides that only affect routing are resolved outside the store.
type ResumeRequest struct {
	// ConvergenceID is the logical convergence history the new segment continues.
	ConvergenceID string
	// PreviousRunID is the terminal parked segment being resumed.
	PreviousRunID string
	// ExpectedCursor is the resume cursor the caller observed; a mismatch is stale.
	ExpectedCursor string
	// NewRunID is the run ID allocated for the new active segment.
	NewRunID string
}
