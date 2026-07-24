package convergence

// FindingState is the current lifecycle state of a finding, projected from
// ordered observations and user decisions.
type FindingState string

const (
	// FindingActionable means the finding is open and blocks clean.
	FindingActionable FindingState = "actionable"
	// FindingResolved means a fresh review no longer reports the finding.
	FindingResolved FindingState = "resolved"
	// FindingInvalid means a current review closed the finding as not a real issue.
	FindingInvalid FindingState = "invalid"
	// FindingDuplicate means a current review closed the finding as a duplicate.
	FindingDuplicate FindingState = "duplicate"
	// FindingWaived means an authorized user waived the valid finding.
	FindingWaived FindingState = "waived"
)

// IsOpen reports whether the finding still counts as accepted actionable work.
func (s FindingState) IsOpen() bool { return s == FindingActionable }

// ActorKind classifies who is attempting an action.
type ActorKind string

const (
	// ActorReview is a read-only review session.
	ActorReview ActorKind = "review"
	// ActorUser is a local workspace user.
	ActorUser ActorKind = "user"
	// ActorModel is a model session other than an authoritative review.
	ActorModel ActorKind = "model"
)

// Actor identifies who requested a disposition and the authority they carry.
type Actor struct {
	// Kind is the actor classification.
	Kind ActorKind
	// ID is the actor identifier for auditing.
	ID string
	// CurrentReview is true when a review actor evaluated the current snapshot.
	CurrentReview bool
	// RunAuthority is true when a user actor holds run authority.
	RunAuthority bool
}

// ObservationOutcome is what a review reported about a finding for one snapshot.
type ObservationOutcome string

const (
	// ObservationActionable means the review still reports the finding.
	ObservationActionable ObservationOutcome = "actionable"
	// ObservationResolved means the review no longer reports the finding.
	ObservationResolved ObservationOutcome = "resolved"
)

// ObservationEvent is one snapshot-bound review observation of a finding. Line,
// column, prose, and reviewer IDs are evidence, never identity, and are omitted
// from the projection contract.
type ObservationEvent struct {
	// ObservationID makes the observation idempotent under replay.
	ObservationID string
	// Fingerprint is the finding's semantic identity.
	Fingerprint FindingFingerprint
	// Sequence is the monotonic projection order.
	Sequence uint64
	// SnapshotSeq is the snapshot recency ordinal used to reject stale updates.
	SnapshotSeq uint64
	// Severity is the observed severity.
	Severity Severity
	// Outcome is what the review reported.
	Outcome ObservationOutcome
	// ReviewID identifies the source review.
	ReviewID string
}

// DispositionType is the kind of user or review decision closing a finding.
type DispositionType string

const (
	// DispositionInvalid closes a finding as not a real issue.
	DispositionInvalid DispositionType = "invalid"
	// DispositionDuplicate closes a finding as a duplicate of another.
	DispositionDuplicate DispositionType = "duplicate"
	// DispositionWaived waives a valid finding.
	DispositionWaived DispositionType = "waived"
)

// DispositionEvent is one decision closing a finding. Invalid and duplicate come
// from a current review with evidence; waived comes from an authorized user with
// a reason.
type DispositionEvent struct {
	// DecisionID makes the decision idempotent under replay.
	DecisionID string
	// Fingerprint is the target finding's semantic identity.
	Fingerprint FindingFingerprint
	// Type is the disposition kind.
	Type DispositionType
	// Actor is who requested the disposition.
	Actor Actor
	// Evidence is required for invalid and duplicate dispositions.
	Evidence string
	// Reason is required for waivers.
	Reason string
	// SnapshotSeq is the snapshot recency ordinal used to reject stale decisions.
	SnapshotSeq uint64
	// RelatedFingerprint optionally names the duplicate's counterpart.
	RelatedFingerprint FindingFingerprint
}

// finalState maps a disposition type to the finding state it produces.
func (t DispositionType) finalState() (FindingState, bool) {
	switch t {
	case DispositionInvalid:
		return FindingInvalid, true
	case DispositionDuplicate:
		return FindingDuplicate, true
	case DispositionWaived:
		return FindingWaived, true
	default:
		return "", false
	}
}
