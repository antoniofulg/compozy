package contract

import (
	"sort"
	"strconv"

	"github.com/compozy/compozy/internal/core/convergence"
)

// Default bounding limits for a projected convergence snapshot. Histories beyond
// these limits are truncated in the read model, but the current actionable
// findings, active approval, terminal reason, unresolved work, cursor, and child
// relations are always retained regardless of these limits.
const (
	defaultConvergenceMaxFindings     = 200
	defaultConvergenceMaxRounds       = 50
	defaultConvergenceMaxBatches      = 100
	defaultConvergenceMaxVerification = 50
)

// ConvergenceProjectionOptions tunes the bounded projection. Zero limits fall back
// to the package defaults. Children, ReceiptPath, and WorktreeDirty are supplied by
// the daemon from the global index and Git state; the pure snapshot cannot know
// them on its own.
type ConvergenceProjectionOptions struct {
	MaxFindings     int
	MaxRounds       int
	MaxBatches      int
	MaxVerification int
	// Children are continuation and Task Group child relations from the global
	// index. They are always retained in full.
	Children []ConvergenceRelation
	// ReceiptPath is the run-relative receipt path, when a receipt exists.
	ReceiptPath string
	// WorktreeDirty overrides the derived dirty flag with observed Git state.
	WorktreeDirty *bool
}

// DefaultConvergenceProjectionOptions returns the standard bounding limits.
func DefaultConvergenceProjectionOptions() ConvergenceProjectionOptions {
	return ConvergenceProjectionOptions{
		MaxFindings:     defaultConvergenceMaxFindings,
		MaxRounds:       defaultConvergenceMaxRounds,
		MaxBatches:      defaultConvergenceMaxBatches,
		MaxVerification: defaultConvergenceMaxVerification,
	}
}

// NewConvergenceSnapshotResponse wraps a bounded projection in its response
// envelope.
func NewConvergenceSnapshotResponse(
	snap convergence.Snapshot,
	opts ConvergenceProjectionOptions,
) ConvergenceSnapshotResponse {
	return ConvergenceSnapshotResponse{Convergence: ProjectConvergenceSnapshot(snap, opts)}
}

// ProjectConvergenceSnapshot maps the canonical domain snapshot to the bounded,
// versioned read model. It bounds history sections while always retaining current
// actionable findings, the active approval, the terminal reason, unresolved work,
// the cursor, and child relations. It never emits absolute paths, finding prose,
// model output, or stored authorization.
func ProjectConvergenceSnapshot(
	snap convergence.Snapshot,
	opts ConvergenceProjectionOptions,
) ConvergenceSnapshot {
	findings, findingsPage := boundConvergenceFindings(snap.Findings, opts.MaxFindings)
	rounds, roundsPage := boundConvergenceTail(snap.Rounds, opts.MaxRounds, defaultConvergenceMaxRounds)
	batches, batchesPage := boundConvergenceTail(snap.Batches, opts.MaxBatches, defaultConvergenceMaxBatches)
	verifications, verificationPage := boundConvergenceTail(
		snap.Verification,
		opts.MaxVerification,
		defaultConvergenceMaxVerification,
	)
	return ConvergenceSnapshot{
		Version:         ConvergenceSnapshotVersion,
		ConvergenceID:   snap.ConvergenceID,
		RequestID:       snap.RequestID,
		Segment:         convergenceSegmentFromDomain(snap.Segment),
		Target:          convergenceTargetFromDomain(snap.Target),
		Config:          convergenceConfigFromDomain(snap.Config),
		Phase:           convergencePhaseFromDomain(snap.Phase),
		Conditions:      deriveConvergenceConditions(snap),
		Routes:          convergenceRoutesFromDomain(snap.Routes),
		Rounds:          convergenceRoundsFromDomain(rounds),
		Batches:         convergenceBatchesFromDomain(batches),
		Findings:        convergenceFindingsFromDomain(findings),
		Verification:    convergenceVerificationsFromDomain(verifications),
		Approvals:       convergenceApprovalsFromDomain(snap.Approvals),
		Terminal:        convergenceTerminalFromDomain(snap.Terminal),
		Handoff:         deriveConvergenceHandoff(snap, opts),
		Relations:       deriveConvergenceRelations(snap, opts),
		Page:            convergencePage(snap, findingsPage, roundsPage, batchesPage, verificationPage),
		UnresolvedCount: snap.UnresolvedCount(),
		LastSeq:         snap.LastSeq,
	}
}

func convergencePage(
	snap convergence.Snapshot,
	findings, rounds, batches, verification ConvergenceSectionPage,
) ConvergencePage {
	cursor := ""
	if snap.LastSeq > 0 {
		cursor = strconv.FormatUint(snap.LastSeq, 10)
	}
	return ConvergencePage{
		Findings:     findings,
		Rounds:       rounds,
		Batches:      batches,
		Verification: verification,
		Cursor:       cursor,
	}
}

// boundConvergenceFindings always keeps every open (actionable) finding and bounds
// only the closed-finding history to the remaining budget, preferring the most
// recent by first sequence. The result is re-sorted by first sequence for stable
// display.
func boundConvergenceFindings(
	findings []convergence.Finding,
	limit int,
) ([]convergence.Finding, ConvergenceSectionPage) {
	total := len(findings)
	if limit <= 0 {
		limit = defaultConvergenceMaxFindings
	}
	open := make([]convergence.Finding, 0, len(findings))
	closed := make([]convergence.Finding, 0, len(findings))
	for i := range findings {
		if findings[i].State.IsOpen() {
			open = append(open, findings[i])
			continue
		}
		closed = append(closed, findings[i])
	}
	budget := limit - len(open)
	if budget < 0 {
		budget = 0
	}
	keptClosed := closed
	if len(closed) > budget {
		keptClosed = closed[len(closed)-budget:]
	}
	kept := make([]convergence.Finding, 0, len(open)+len(keptClosed))
	kept = append(kept, open...)
	kept = append(kept, keptClosed...)
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].FirstSeq < kept[j].FirstSeq })
	return kept, ConvergenceSectionPage{Total: total, Shown: len(kept), Truncated: total > len(kept)}
}

// boundConvergenceTail keeps the most recent limit entries of an ordered history.
func boundConvergenceTail[T any](items []T, limit, fallback int) ([]T, ConvergenceSectionPage) {
	total := len(items)
	if limit <= 0 {
		limit = fallback
	}
	kept := items
	if total > limit {
		kept = items[total-limit:]
	}
	return kept, ConvergenceSectionPage{Total: total, Shown: len(kept), Truncated: total > len(kept)}
}
