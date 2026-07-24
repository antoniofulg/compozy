package contract

import "github.com/compozy/compozy/internal/core/convergence"

// deriveConvergenceConditions projects the six deterministic clean-gate conditions
// from canonical snapshot state. It never infers a gate from displayed counts or
// model prose. A clean terminal marks every gate met; otherwise each gate reports
// met, blocked, or pending from the projected findings, verification, and approvals.
func deriveConvergenceConditions(snap convergence.Snapshot) []ConvergenceCondition {
	clean := snap.Terminal != nil && snap.Terminal.Kind == convergence.TerminalClean
	return []ConvergenceCondition{
		{Kind: ConvergenceConditionInitialVerification, Status: initialVerificationStatus(snap, clean)},
		{Kind: ConvergenceConditionActionableFindings, Status: actionableFindingsStatus(snap, clean)},
		{Kind: ConvergenceConditionWorkspaceStable, Status: workspaceStableStatus(snap)},
		{Kind: ConvergenceConditionCleanReview, Status: cleanReviewStatus(clean)},
		{Kind: ConvergenceConditionCurrentVerification, Status: currentVerificationStatus(snap, clean)},
		{Kind: ConvergenceConditionApprovalRequired, Status: approvalRequiredStatus(snap, clean)},
	}
}

func initialVerificationStatus(snap convergence.Snapshot, clean bool) string {
	if clean {
		return ConvergenceConditionMet
	}
	if len(snap.Verification) == 0 {
		return ConvergenceConditionPending
	}
	for i := range snap.Verification {
		if snap.Verification[i].Passed {
			return ConvergenceConditionMet
		}
	}
	return ConvergenceConditionBlocked
}

func actionableFindingsStatus(snap convergence.Snapshot, clean bool) string {
	if clean {
		return ConvergenceConditionMet
	}
	if snap.UnresolvedCount() > 0 {
		return ConvergenceConditionBlocked
	}
	if convergenceReviewObserved(snap) {
		return ConvergenceConditionMet
	}
	return ConvergenceConditionPending
}

func workspaceStableStatus(snap convergence.Snapshot) string {
	if t := snap.Terminal; t != nil &&
		t.Kind == convergence.TerminalParked &&
		t.Reason == convergence.ParkedWorkspaceChanged {
		return ConvergenceConditionBlocked
	}
	return ConvergenceConditionMet
}

func cleanReviewStatus(clean bool) string {
	if clean {
		return ConvergenceConditionMet
	}
	return ConvergenceConditionPending
}

func currentVerificationStatus(snap convergence.Snapshot, clean bool) string {
	if clean {
		return ConvergenceConditionMet
	}
	current, ok := snap.CurrentVerification()
	if !ok {
		return ConvergenceConditionPending
	}
	if !current.Passed {
		return ConvergenceConditionBlocked
	}
	if currentSnapshotDigest(snap) == "" || current.Snapshot == currentSnapshotDigest(snap) {
		return ConvergenceConditionMet
	}
	return ConvergenceConditionPending
}

func approvalRequiredStatus(snap convergence.Snapshot, clean bool) string {
	if convergencePendingApproval(snap) {
		return ConvergenceConditionBlocked
	}
	if clean {
		return ConvergenceConditionMet
	}
	return ConvergenceConditionPending
}

// deriveConvergenceHandoff projects the preserved branch, worktree, snapshot, and
// resume state for an active or terminal segment. Resume is offered only for a
// parked segment whose cursor is present and unclaimed.
func deriveConvergenceHandoff(snap convergence.Snapshot, opts ConvergenceProjectionOptions) ConvergenceHandoff {
	handoff := ConvergenceHandoff{
		Branch:          snap.Target.Branch,
		Worktree:        snap.Target.Worktree,
		Snapshot:        currentSnapshotDigest(snap),
		AutoCommit:      snap.Config.AutoCommit,
		UnresolvedCount: snap.UnresolvedCount(),
		ReceiptPath:     opts.ReceiptPath,
	}
	if opts.WorktreeDirty != nil {
		handoff.Dirty = *opts.WorktreeDirty
	} else {
		handoff.Dirty = !snap.Config.AutoCommit
	}
	if t := snap.Terminal; t != nil {
		handoff.TerminalKind = string(t.Kind)
		handoff.TerminalReason = string(t.Reason)
		handoff.ResumeAvailable = t.Kind == convergence.TerminalParked &&
			snap.Segment.ResumeCursor != "" &&
			!snap.Segment.ResumeClaimed
	}
	if handoff.ResumeAvailable {
		handoff.ResumeCursor = snap.Segment.ResumeCursor
	}
	return handoff
}

// deriveConvergenceRelations returns the segment lineage plus any daemon-supplied
// continuation or child relations. Relations are always retained in full.
func deriveConvergenceRelations(snap convergence.Snapshot, opts ConvergenceProjectionOptions) []ConvergenceRelation {
	relations := make([]ConvergenceRelation, 0, len(opts.Children)+2)
	if id := snap.Segment.SourceRunID; id != "" {
		relations = append(relations, ConvergenceRelation{Kind: ConvergenceRelationSource, RunID: id})
	}
	if id := snap.Segment.PreviousRunID; id != "" {
		relations = append(relations, ConvergenceRelation{Kind: ConvergenceRelationPrevious, RunID: id})
	}
	relations = append(relations, opts.Children...)
	if len(relations) == 0 {
		return nil
	}
	return relations
}

// currentSnapshotDigest reports the snapshot the current phase is bound to, falling
// back to the frozen target snapshot.
func currentSnapshotDigest(snap convergence.Snapshot) string {
	if snap.Phase.Snapshot != "" {
		return snap.Phase.Snapshot
	}
	return snap.Target.Snapshot
}

// convergenceReviewObserved reports whether a review has produced any observation or
// finding for this run.
func convergenceReviewObserved(snap convergence.Snapshot) bool {
	return len(snap.Observations) > 0 || len(snap.Findings) > 0
}

// convergencePendingApproval reports whether any approval proposal is still awaiting
// a user decision.
func convergencePendingApproval(snap convergence.Snapshot) bool {
	for i := range snap.Approvals {
		if snap.Approvals[i].Decision == "" {
			return true
		}
	}
	return false
}
