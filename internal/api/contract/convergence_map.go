package contract

import "github.com/compozy/compozy/internal/core/convergence"

func convergenceSegmentFromDomain(seg convergence.Segment) ConvergenceSegment {
	return ConvergenceSegment{
		RunID:         seg.RunID,
		Ordinal:       seg.Ordinal,
		PreviousRunID: seg.PreviousRunID,
		SourceRunID:   seg.SourceRunID,
		State:         string(seg.State),
		ResumeCursor:  seg.ResumeCursor,
		ResumeClaimed: seg.ResumeClaimed,
		Terminal:      convergenceTerminalFromDomain(seg.Terminal),
	}
}

func convergenceTargetFromDomain(target convergence.TargetBinding) ConvergenceTarget {
	return ConvergenceTarget{
		WorkspaceID:    target.WorkspaceID,
		ExecutionScope: target.ExecutionScope,
		TaskGroupID:    target.TaskGroupID,
		Branch:         target.Branch,
		Worktree:       target.Worktree,
		Snapshot:       target.Snapshot,
	}
}

func convergenceConfigFromDomain(cfg convergence.FrozenConfiguration) ConvergenceConfigSummary {
	return ConvergenceConfigSummary{
		Profile:    cfg.ProfileName,
		ModelSetup: cfg.ModelSetupName,
		AutoCommit: cfg.AutoCommit,
		Limits: ConvergenceLimits{
			MaxReviewRounds:         cfg.Limits.MaxReviewRounds,
			MaxFindingAttempts:      cfg.Limits.MaxFindingAttempts,
			MaxVerificationAttempts: cfg.Limits.MaxVerificationAttempts,
			NoProgressRounds:        cfg.Limits.NoProgressRounds,
			OscillationCycles:       cfg.Limits.OscillationCycles,
			ReviewAdmissionTimeout:  cfg.Limits.ReviewAdmissionTimeout.String(),
		},
		Warnings: append([]string(nil), cfg.Warnings...),
	}
}

func convergencePhaseFromDomain(phase convergence.PhaseState) ConvergencePhase {
	return ConvergencePhase{
		PhaseID:  phase.PhaseID,
		Kind:     string(phase.Kind),
		Round:    phase.Round,
		BatchID:  phase.BatchID,
		Attempt:  phase.Attempt,
		Snapshot: phase.Snapshot,
		State:    string(phase.State),
	}
}

func convergenceRoutesFromDomain(routes []convergence.RouteSelection) []ConvergenceRoute {
	if len(routes) == 0 {
		return nil
	}
	out := make([]ConvergenceRoute, len(routes))
	for i := range routes {
		out[i] = ConvergenceRoute{
			PhaseID:             routes[i].PhaseID,
			Role:                routes[i].Role,
			Primary:             routes[i].Primary,
			Selected:            routes[i].Selected,
			ConfigurationSource: routes[i].ConfigurationSource,
			FallbackReason:      routes[i].FallbackReason,
		}
	}
	return out
}

func convergenceRoundsFromDomain(rounds []convergence.RoundState) []ConvergenceRound {
	if len(rounds) == 0 {
		return nil
	}
	out := make([]ConvergenceRound, len(rounds))
	for i := range rounds {
		out[i] = ConvergenceRound{
			RoundID:    rounds[i].RoundID,
			Number:     rounds[i].Number,
			AdmittedAt: rounds[i].AdmittedAt,
			Progress: ConvergenceProgress{
				Resolved:             rounds[i].Progress.Resolved,
				SeverityDecreased:    rounds[i].Progress.SeverityDecreased,
				VerificationImproved: rounds[i].Progress.VerificationImproved,
				NoProgressCount:      rounds[i].Progress.NoProgressCount,
				OscillationCount:     rounds[i].Progress.OscillationCount,
			},
			Terminal: convergenceTerminalFromDomain(rounds[i].Terminal),
		}
	}
	return out
}

func convergenceBatchesFromDomain(batches []convergence.BatchState) []ConvergenceBatch {
	if len(batches) == 0 {
		return nil
	}
	out := make([]ConvergenceBatch, len(batches))
	for i := range batches {
		out[i] = ConvergenceBatch{
			BatchID:             batches[i].BatchID,
			PhaseID:             batches[i].PhaseID,
			FindingFingerprints: append([]string(nil), batches[i].FindingFingerprints...),
			BeforeSnapshot:      batches[i].BeforeSnapshot,
			AfterSnapshot:       batches[i].AfterSnapshot,
			Status:              batches[i].Status,
			AffectedPathsRef:    batches[i].AffectedPathsRef,
		}
	}
	return out
}

func convergenceFindingsFromDomain(findings []convergence.Finding) []ConvergenceFinding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]ConvergenceFinding, len(findings))
	for i := range findings {
		out[i] = ConvergenceFinding{
			Fingerprint: string(findings[i].Fingerprint),
			State:       string(findings[i].State),
			Severity:    string(findings[i].Severity),
			SnapshotSeq: findings[i].SnapshotSeq,
			Attempts:    findings[i].Attempts,
			FirstSeq:    findings[i].FirstSeq,
			EvidenceRef: findings[i].EvidenceRef,
		}
	}
	return out
}

func convergenceVerificationsFromDomain(results []convergence.VerificationResult) []ConvergenceVerification {
	if len(results) == 0 {
		return nil
	}
	out := make([]ConvergenceVerification, len(results))
	for i := range results {
		var exit *int
		if results[i].ExitCode != nil {
			code := *results[i].ExitCode
			exit = &code
		}
		out[i] = ConvergenceVerification{
			VerificationID:     results[i].VerificationID,
			PhaseID:            results[i].PhaseID,
			CommandFingerprint: results[i].CommandFingerprint,
			Snapshot:           results[i].Snapshot,
			ExitCode:           exit,
			Passed:             results[i].Passed,
			Attempt:            results[i].Attempt,
			EvidencePath:       results[i].EvidencePath,
		}
	}
	return out
}

func convergenceApprovalsFromDomain(approvals []convergence.ApprovalProposal) []ConvergenceApproval {
	if len(approvals) == 0 {
		return nil
	}
	out := make([]ConvergenceApproval, len(approvals))
	for i := range approvals {
		out[i] = ConvergenceApproval{
			ProposalID:  approvals[i].ProposalID,
			Fingerprint: approvals[i].Fingerprint,
			Action:      approvals[i].Action,
			Snapshot:    approvals[i].Snapshot,
			Decision:    approvals[i].Decision,
			Reason:      approvals[i].Reason,
			EvidenceRef: approvals[i].EvidenceRef,
		}
	}
	return out
}

// convergenceTerminalFromDomain maps a domain terminal outcome to its public
// projection, resolving the public run status and event kind. A malformed outcome
// keeps its coarse kind rather than guessing a status.
func convergenceTerminalFromDomain(outcome *convergence.TerminalOutcome) *ConvergenceTerminal {
	if outcome == nil {
		return nil
	}
	terminal := &ConvergenceTerminal{
		Kind:   string(outcome.Kind),
		Reason: string(outcome.Reason),
	}
	if public, err := outcome.Public(); err == nil {
		terminal.Status = public.Status
		terminal.EventKind = public.EventKind
	}
	return terminal
}
