package convergencestore

import (
	"strings"

	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/store/rundb"
)

func segmentFromRow(row rundb.ConvergenceSegmentRow) convergence.Segment {
	return convergence.Segment{
		RunID:         row.RunID,
		Ordinal:       row.Ordinal,
		PreviousRunID: row.PreviousRunID,
		SourceRunID:   row.SourceRunID,
		State:         convergence.SegmentState(row.State),
		ResumeCursor:  row.ResumeCursor,
		ResumeClaimed: row.ResumeClaimed,
		Terminal:      terminalFromRow(row.TerminalKind, row.TerminalReason),
	}
}

func terminalFromRow(kind, reason string) *convergence.TerminalOutcome {
	if strings.TrimSpace(kind) == "" {
		return nil
	}
	outcome := convergence.TerminalOutcome{Kind: convergence.TerminalKind(kind)}
	if outcome.Kind == convergence.TerminalParked {
		outcome.Reason = convergence.ParkedReason(reason)
	}
	return &outcome
}

func phaseFromRow(row rundb.ConvergencePhaseRow) convergence.PhaseState {
	return convergence.PhaseState{
		PhaseID:  row.PhaseID,
		Kind:     convergence.PhaseKind(row.Phase),
		Round:    row.Round,
		BatchID:  row.BatchID,
		Attempt:  row.Attempt,
		Snapshot: row.Snapshot,
		State:    convergence.SegmentState(row.State),
	}
}

func roundsFromRows(rows []rundb.ConvergenceRoundRow) []convergence.RoundState {
	result := make([]convergence.RoundState, 0, len(rows))
	for _, row := range rows {
		round := convergence.RoundState{
			RoundID: row.RoundID,
			Number:  row.Number,
			Progress: convergence.ProgressState{
				Resolved:             row.Resolved,
				SeverityDecreased:    row.SeverityDecreased,
				VerificationImproved: row.VerificationImproved,
				NoProgressCount:      row.NoProgressCount,
				OscillationCount:     row.OscillationCount,
			},
		}
		round.AdmittedAt = row.AdmittedAt
		if strings.TrimSpace(row.TerminalReason) != "" {
			round.Terminal = &convergence.TerminalOutcome{
				Kind:   convergence.TerminalParked,
				Reason: convergence.ParkedReason(row.TerminalReason),
			}
		}
		result = append(result, round)
	}
	return result
}

func routesFromRows(rows []rundb.ConvergenceRouteRow) []convergence.RouteSelection {
	result := make([]convergence.RouteSelection, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.RouteSelection{
			PhaseID:             row.PhaseID,
			Role:                row.Role,
			Primary:             row.Primary,
			Selected:            row.Selected,
			ConfigurationSource: row.ConfigurationSource,
			FallbackReason:      row.FallbackReason,
		})
	}
	return result
}

func batchesFromRows(rows []rundb.ConvergenceBatchRow) []convergence.BatchState {
	result := make([]convergence.BatchState, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.BatchState{
			BatchID:             row.BatchID,
			PhaseID:             row.PhaseID,
			FindingFingerprints: append([]string(nil), row.FindingFingerprints...),
			BeforeSnapshot:      row.BeforeSnapshot,
			AfterSnapshot:       row.AfterSnapshot,
			Status:              row.Status,
			AffectedPathsRef:    row.AffectedRef,
		})
	}
	return result
}

func observationsFromRows(rows []rundb.ConvergenceObservationRow) []convergence.FindingObservation {
	result := make([]convergence.FindingObservation, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.FindingObservation{
			ObservationID: row.ObservationID,
			Fingerprint:   row.Fingerprint,
			Snapshot:      row.Snapshot,
			SnapshotSeq:   row.SnapshotSeq,
			Severity:      row.Severity,
			Outcome:       row.Outcome,
			ReviewID:      row.ReviewID,
		})
	}
	return result
}

func dispositionsFromRows(rows []rundb.ConvergenceDispositionRow) []convergence.FindingDisposition {
	result := make([]convergence.FindingDisposition, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.FindingDisposition{
			DecisionID:         row.DecisionID,
			Fingerprint:        row.Fingerprint,
			Disposition:        row.Disposition,
			ActorKind:          row.ActorKind,
			Reason:             row.Reason,
			Snapshot:           row.Snapshot,
			SnapshotSeq:        row.SnapshotSeq,
			RelatedFingerprint: row.RelatedFingerprint,
		})
	}
	return result
}

func findingsFromRows(rows []rundb.ConvergenceFindingRow) []convergence.Finding {
	result := make([]convergence.Finding, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.Finding{
			Fingerprint: convergence.FindingFingerprint(row.Fingerprint),
			State:       convergence.FindingState(row.State),
			Severity:    convergence.Severity(row.Severity),
			SnapshotSeq: row.SnapshotSeq,
			Attempts:    row.Attempts,
			FirstSeq:    row.FirstSeq,
			EvidenceRef: row.EvidenceRef,
		})
	}
	return result
}

func verificationsFromRows(rows []rundb.ConvergenceVerificationRow) []convergence.VerificationResult {
	result := make([]convergence.VerificationResult, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.VerificationResult{
			VerificationID:     row.VerificationID,
			PhaseID:            row.PhaseID,
			CommandFingerprint: row.CommandFingerprint,
			Snapshot:           row.Snapshot,
			ExitCode:           row.ExitCode,
			Passed:             row.Passed,
			Attempt:            row.Attempt,
			EvidencePath:       row.EvidencePath,
		})
	}
	return result
}

func approvalsFromRows(rows []rundb.ConvergenceApprovalRow) []convergence.ApprovalProposal {
	result := make([]convergence.ApprovalProposal, 0, len(rows))
	for _, row := range rows {
		result = append(result, convergence.ApprovalProposal{
			ProposalID:  row.ProposalID,
			Fingerprint: row.Fingerprint,
			Action:      row.Action,
			Snapshot:    row.Snapshot,
			Decision:    row.Decision,
			Reason:      row.Reason,
			EvidenceRef: row.EvidenceRef,
		})
	}
	return result
}
