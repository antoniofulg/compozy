package convergence

import (
	"fmt"
	"sort"
	"strings"
)

// BatchFinding is one accepted actionable finding considered for correction
// planning. The caller supplies only findings that are currently actionable; a
// resolved, invalid, duplicate, or waived finding is removed before planning.
type BatchFinding struct {
	// Fingerprint is the finding's semantic identity.
	Fingerprint FindingFingerprint
	// File is the normalized repo-relative path the finding belongs to.
	File string
	// Severity is the finding's current observed severity.
	Severity Severity
	// Sequence is the finding's first-seen projection order, used for deterministic
	// batch ordering.
	Sequence uint64
}

// CorrectionSession is one bounded correction session within a logical batch.
// Large same-file groups split into ordered sessions that still execute
// sequentially; the group remains one logical batch for ordering and
// verification.
type CorrectionSession struct {
	// Index is the 0-based session order within the batch.
	Index int
	// FindingFingerprints are the findings this session corrects, in batch order.
	FindingFingerprints []FindingFingerprint
}

// CorrectionBatch is all actionable findings for one normalized file, grouped as
// one logical unit. Batches are ordered deterministically and execute
// sequentially; RouteSeverity is the highest current severity in the batch and
// selects the configured by_severity correction route.
type CorrectionBatch struct {
	// Order is the 0-based deterministic batch order.
	Order int
	// File is the normalized repo-relative path shared by every finding.
	File string
	// FindingFingerprints are every finding in the batch, in deterministic order.
	FindingFingerprints []FindingFingerprint
	// RouteSeverity is the highest severity in the batch.
	RouteSeverity Severity
	// Sessions are the ordered bounded sessions the batch executes through.
	Sessions []CorrectionSession
}

// PlanCorrectionBatches groups every actionable finding for the same normalized
// file into one logical batch, orders batches by first-finding sequence then
// path, orders findings within a batch by sequence then fingerprint, selects the
// highest severity as the batch route, and splits large groups into bounded
// sequential sessions. maxSessionSize bounds a single session; a value of zero or
// less places the whole group in one session. It never merges findings from
// different files and never reorders nondeterministically.
func PlanCorrectionBatches(findings []BatchFinding, maxSessionSize int) ([]CorrectionBatch, error) {
	groups, order, err := groupByFile(findings)
	if err != nil {
		return nil, err
	}
	sortBatchOrder(order, groups)
	batches := make([]CorrectionBatch, 0, len(order))
	for i, file := range order {
		group := groups[file]
		sortWithinGroup(group)
		batch := CorrectionBatch{
			Order:               i,
			File:                file,
			FindingFingerprints: fingerprintsOf(group),
			RouteSeverity:       highestGroupSeverity(group),
			Sessions:            splitSessions(fingerprintsOf(group), maxSessionSize),
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

// groupByFile buckets findings by normalized file and records first-appearance
// order. It rejects a finding whose path cannot be normalized rather than
// grouping it under an ambiguous key.
func groupByFile(findings []BatchFinding) (map[string][]BatchFinding, []string, error) {
	groups := make(map[string][]BatchFinding)
	order := make([]string, 0)
	for i := range findings {
		file, err := normalizeFindingPath(findings[i].File)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := groups[file]; !ok {
			order = append(order, file)
		}
		normalized := findings[i]
		normalized.File = file
		groups[file] = append(groups[file], normalized)
	}
	return groups, order, nil
}

// sortBatchOrder orders files by the lowest finding sequence in each group, then
// by path, so batch order is deterministic and independent of input order.
func sortBatchOrder(order []string, groups map[string][]BatchFinding) {
	sort.SliceStable(order, func(i, j int) bool {
		left, right := order[i], order[j]
		li, ri := minSequence(groups[left]), minSequence(groups[right])
		if li != ri {
			return li < ri
		}
		return left < right
	})
}

// sortWithinGroup orders findings by sequence then fingerprint for a stable batch
// interior.
func sortWithinGroup(group []BatchFinding) {
	sort.SliceStable(group, func(i, j int) bool {
		if group[i].Sequence != group[j].Sequence {
			return group[i].Sequence < group[j].Sequence
		}
		return group[i].Fingerprint < group[j].Fingerprint
	})
}

func minSequence(group []BatchFinding) uint64 {
	best := ^uint64(0)
	for i := range group {
		if group[i].Sequence < best {
			best = group[i].Sequence
		}
	}
	return best
}

func fingerprintsOf(group []BatchFinding) []FindingFingerprint {
	out := make([]FindingFingerprint, len(group))
	for i := range group {
		out[i] = group[i].Fingerprint
	}
	return out
}

func highestGroupSeverity(group []BatchFinding) Severity {
	severities := make([]Severity, len(group))
	for i := range group {
		severities[i] = group[i].Severity
	}
	highest, _ := HighestSeverity(severities)
	return highest
}

// splitSessions divides a batch's findings into bounded ordered sessions. A
// non-positive bound yields a single session containing the whole batch.
func splitSessions(fingerprints []FindingFingerprint, maxSessionSize int) []CorrectionSession {
	if maxSessionSize <= 0 || len(fingerprints) <= maxSessionSize {
		return []CorrectionSession{{Index: 0, FindingFingerprints: fingerprints}}
	}
	sessions := make([]CorrectionSession, 0, (len(fingerprints)+maxSessionSize-1)/maxSessionSize)
	for start := 0; start < len(fingerprints); start += maxSessionSize {
		end := start + maxSessionSize
		if end > len(fingerprints) {
			end = len(fingerprints)
		}
		sessions = append(sessions, CorrectionSession{
			Index:               len(sessions),
			FindingFingerprints: fingerprints[start:end],
		})
	}
	return sessions
}

// CorrectionOutcome is the durable outcome of one correction batch. Only changed
// and no_change are trustworthy terminal states; canceled, incomplete, and
// unknown never authorize a phase and must pass through recovery.
type CorrectionOutcome string

const (
	// CorrectionChanged means the batch produced a durable owned change.
	CorrectionChanged CorrectionOutcome = "changed"
	// CorrectionNoChange means the batch completed without any project change.
	CorrectionNoChange CorrectionOutcome = "no_change"
	// CorrectionCanceled means the batch was canceled before a durable result.
	CorrectionCanceled CorrectionOutcome = "canceled"
	// CorrectionIncomplete means the batch produced no trustworthy terminal result.
	CorrectionIncomplete CorrectionOutcome = "incomplete"
	// CorrectionUnknown means recovery could not determine the batch outcome.
	CorrectionUnknown CorrectionOutcome = "unknown"
	// CorrectionReplayed means a durable prior result was replayed.
	CorrectionReplayed CorrectionOutcome = "replayed"
)

// IsValid reports whether o is a recognized correction outcome.
func (o CorrectionOutcome) IsValid() bool {
	switch o {
	case CorrectionChanged, CorrectionNoChange, CorrectionCanceled,
		CorrectionIncomplete, CorrectionUnknown, CorrectionReplayed:
		return true
	default:
		return false
	}
}

// CorrectionResult is the durable evidence of one correction batch: the before
// and after Git snapshots and the affected files. A changed outcome must move the
// snapshot and name affected paths; a no_change outcome must leave the snapshot
// and name none. The result records evidence only; it never re-applies an edit.
type CorrectionResult struct {
	BatchID             string
	PhaseID             string
	FindingFingerprints []FindingFingerprint
	BeforeSnapshot      string
	AfterSnapshot       string
	AffectedPaths       []string
	Outcome             CorrectionOutcome
}

// Changed reports whether the correction moved the owned Git snapshot.
func (r CorrectionResult) Changed() bool {
	return r.BeforeSnapshot != "" && r.AfterSnapshot != "" && r.BeforeSnapshot != r.AfterSnapshot
}

// Validate enforces the correction-result evidence contract: identity fields, a
// before snapshot, and outcome-consistent after-snapshot and affected-path
// evidence. It rejects a changed outcome that did not move the snapshot and a
// no_change outcome that named affected paths.
func (r CorrectionResult) Validate() error {
	if strings.TrimSpace(r.BatchID) == "" {
		return fmt.Errorf("%w: batch id is required", ErrCorrectionInvalid)
	}
	if strings.TrimSpace(r.PhaseID) == "" {
		return fmt.Errorf("%w: phase id is required", ErrCorrectionInvalid)
	}
	if !r.Outcome.IsValid() {
		return fmt.Errorf("%w: unknown correction outcome %q", ErrCorrectionInvalid, r.Outcome)
	}
	if strings.TrimSpace(r.BeforeSnapshot) == "" {
		return fmt.Errorf("%w: before snapshot is required", ErrCorrectionInvalid)
	}
	return r.validateOutcomeEvidence()
}

func (r CorrectionResult) validateOutcomeEvidence() error {
	switch r.Outcome {
	case CorrectionChanged:
		if !r.Changed() {
			return fmt.Errorf("%w: changed outcome must move the snapshot", ErrCorrectionInvalid)
		}
		if len(r.AffectedPaths) == 0 {
			return fmt.Errorf("%w: changed outcome must name affected paths", ErrCorrectionInvalid)
		}
	case CorrectionNoChange:
		if r.AfterSnapshot != "" && r.AfterSnapshot != r.BeforeSnapshot {
			return fmt.Errorf("%w: no_change outcome must not move the snapshot", ErrCorrectionInvalid)
		}
		if len(r.AffectedPaths) != 0 {
			return fmt.Errorf("%w: no_change outcome must not name affected paths", ErrCorrectionInvalid)
		}
	}
	return nil
}

// ChangeKind classifies a change a fixer proposes. Additive and repairing test
// changes are allowed within scope; every other listed kind weakens a protected
// test or verification gate and must be approved before it is applied.
type ChangeKind string

const (
	// ChangeAddTest adds new test coverage. Allowed without approval.
	ChangeAddTest ChangeKind = "add_test"
	// ChangeRepairTest repairs an existing test to reflect accepted behavior.
	// Allowed without approval.
	ChangeRepairTest ChangeKind = "repair_test"
	// ChangeRemoveTest deletes an existing test. Protected.
	ChangeRemoveTest ChangeKind = "remove_test"
	// ChangeSkipTest skips or disables an existing test. Protected.
	ChangeSkipTest ChangeKind = "skip_test"
	// ChangeWeakenAssertion weakens an existing assertion. Protected.
	ChangeWeakenAssertion ChangeKind = "weaken_assertion"
	// ChangeMutateVerification changes an authoritative verification command.
	// Protected.
	ChangeMutateVerification ChangeKind = "mutate_verification_command"
	// ChangeBypassGate bypasses an authoritative quality gate. Protected.
	ChangeBypassGate ChangeKind = "bypass_gate"
)

// Protected reports whether a change kind weakens a protected test or gate and
// therefore requires user approval before it may be applied.
func (k ChangeKind) Protected() bool {
	switch k {
	case ChangeRemoveTest, ChangeSkipTest, ChangeWeakenAssertion,
		ChangeMutateVerification, ChangeBypassGate:
		return true
	default:
		return false
	}
}

// ProposedChange is one change a fixer proposes to make. Its Kind classifies the
// intent so the daemon can allow additive or repairing test work and require
// approval before any weakening is applied.
type ProposedChange struct {
	Kind   ChangeKind
	Path   string
	Detail string
}

// ApprovalRequirement is a protected change that the daemon must not apply until
// the user approves it. Action mirrors the change kind for the approval proposal
// record.
type ApprovalRequirement struct {
	Action ChangeKind
	Path   string
	Detail string
	Reason string
}

// ProtectedChangeReport separates protected changes that require approval from
// changes the fixer may apply directly.
type ProtectedChangeReport struct {
	// Protected are changes that must be approved before they may be applied.
	Protected []ApprovalRequirement
	// Allowed are additive or repairing changes the fixer may apply directly.
	Allowed []ProposedChange
}

// RequiresApproval reports whether any proposed change weakens a protected test
// or gate and must be parked for user approval.
func (r ProtectedChangeReport) RequiresApproval() bool {
	return len(r.Protected) > 0
}

// ClassifyProtectedChanges partitions proposed changes into those that require
// user approval and those a fixer may apply directly. It detects proposed test
// removal, skip, assertion weakening, verification-command mutation, and gate
// bypass before they are applied; additive and repairing test changes remain
// allowed. An unrecognized change kind is treated as protected so an unknown
// mutation can never bypass approval.
func ClassifyProtectedChanges(changes []ProposedChange) ProtectedChangeReport {
	report := ProtectedChangeReport{}
	for _, change := range changes {
		if change.Kind == ChangeAddTest || change.Kind == ChangeRepairTest {
			report.Allowed = append(report.Allowed, change)
			continue
		}
		report.Protected = append(report.Protected, ApprovalRequirement{
			Action: change.Kind,
			Path:   change.Path,
			Detail: change.Detail,
			Reason: protectedChangeReason(change.Kind),
		})
	}
	return report
}

func protectedChangeReason(kind ChangeKind) string {
	switch kind {
	case ChangeRemoveTest:
		return "removing an existing test requires user approval"
	case ChangeSkipTest:
		return "skipping an existing test requires user approval"
	case ChangeWeakenAssertion:
		return "weakening an assertion requires user approval"
	case ChangeMutateVerification:
		return "changing an authoritative verification command requires user approval"
	case ChangeBypassGate:
		return "bypassing an authoritative gate requires user approval"
	default:
		return "an unrecognized change requires user approval before it is applied"
	}
}
