package daemon

import (
	"errors"
	"fmt"
	"strings"

	"github.com/compozy/compozy/internal/api/contract"
	"github.com/compozy/compozy/internal/core/convergence"
)

// Daemon-level control sentinels for convergence approval and cancellation. Stale
// and not-parked conditions reuse the domain sentinels so they map to the stable
// transport codes; request-shape and authority failures are daemon-local.
var (
	errConvergenceApprovalInvalid = errors.New("daemon: convergence approval request is invalid")
	errConvergenceCanceled        = errors.New("daemon: convergence run is canceled")
)

// authorizeResumeTarget is the coordinator's pre-claim gate for a resume request.
// Only a parked segment is resumable; a clean, failed, or canceled terminal is
// not (ErrNotParked), and a cursor that does not match the parked segment's
// resume cursor is stale (ErrResumeCursorStale). The at-most-once claim itself and
// duplicate-request replay are enforced downstream by ConvergenceStore.ClaimResume.
func authorizeResumeTarget(snap convergence.Snapshot, expectedCursor string) error {
	terminal := snap.Segment.Terminal
	if terminal == nil || terminal.Kind != convergence.TerminalParked {
		return fmt.Errorf("%w: segment is not parked", convergence.ErrNotParked)
	}
	if strings.TrimSpace(snap.Segment.ResumeCursor) == "" {
		return fmt.Errorf("%w: segment exposes no resume cursor", convergence.ErrNotParked)
	}
	if strings.TrimSpace(expectedCursor) != strings.TrimSpace(snap.Segment.ResumeCursor) {
		return fmt.Errorf("%w: resume cursor does not match", convergence.ErrResumeCursorStale)
	}
	return nil
}

// buildResumeRequest assembles the durable single-claim resume request from a
// parked snapshot, the caller's observed cursor, and the freshly allocated run ID
// for the new linked segment. Prospective route overrides are resolved separately
// and never mutate the consumed history this request links from.
func buildResumeRequest(snap convergence.Snapshot, expectedCursor, newRunID string) convergence.ResumeRequest {
	return convergence.ResumeRequest{
		ConvergenceID:  snap.ConvergenceID,
		PreviousRunID:  snap.Segment.RunID,
		ExpectedCursor: strings.TrimSpace(expectedCursor),
		NewRunID:       strings.TrimSpace(newRunID),
	}
}

// approvalDecision is the outcome of deciding a protected-action proposal. Deciding
// never resumes the run: Replayed reports an idempotent repeat of a prior identical
// decision, and Proposal is the decided proposal the caller persists while leaving
// the segment parked for a separate, explicit resume.
type approvalDecision struct {
	Proposal convergence.ApprovalProposal
	Replayed bool
}

// decideApproval validates and applies a protected-action approval decision. It
// requires a valid approve/reject decision, a nonblank reason, and expected
// fingerprint/snapshot binding to the exact proposal the caller was shown. A
// repeat of an identical prior decision replays idempotently; a different
// decision on an already-resolved proposal, or a changed fingerprint/snapshot, is
// rejected as stale. It never resumes the run.
func decideApproval(
	proposal convergence.ApprovalProposal,
	req contract.ApprovalDecisionRequest,
) (approvalDecision, error) {
	if err := validateApprovalShape(req); err != nil {
		return approvalDecision{}, err
	}
	if err := bindApprovalToProposal(proposal, req); err != nil {
		return approvalDecision{}, err
	}
	decision := strings.TrimSpace(req.Decision)
	reason := strings.TrimSpace(req.Reason)
	if strings.TrimSpace(proposal.Decision) != "" {
		if proposal.Decision == decision && proposal.Reason == reason {
			return approvalDecision{Proposal: proposal, Replayed: true}, nil
		}
		return approvalDecision{}, fmt.Errorf("%w: proposal already decided", convergence.ErrApprovalStale)
	}
	proposal.Decision = decision
	proposal.Reason = reason
	return approvalDecision{Proposal: proposal}, nil
}

func validateApprovalShape(req contract.ApprovalDecisionRequest) error {
	if strings.TrimSpace(req.ProposalID) == "" {
		return fmt.Errorf("%w: proposal id required", errConvergenceApprovalInvalid)
	}
	switch strings.TrimSpace(req.Decision) {
	case contract.ConvergenceDecisionApprove, contract.ConvergenceDecisionReject:
	default:
		return fmt.Errorf("%w: decision must be approve or reject", errConvergenceApprovalInvalid)
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("%w: reason required", errConvergenceApprovalInvalid)
	}
	if strings.TrimSpace(req.ExpectedFingerprint) == "" {
		return fmt.Errorf("%w: expected fingerprint required", errConvergenceApprovalInvalid)
	}
	if strings.TrimSpace(req.ExpectedSnapshot) == "" {
		return fmt.Errorf("%w: expected snapshot required", errConvergenceApprovalInvalid)
	}
	return nil
}

func bindApprovalToProposal(proposal convergence.ApprovalProposal, req contract.ApprovalDecisionRequest) error {
	if fingerprint := strings.TrimSpace(
		req.ExpectedFingerprint,
	); fingerprint != strings.TrimSpace(
		proposal.Fingerprint,
	) {
		return fmt.Errorf("%w: proposal fingerprint changed", convergence.ErrApprovalStale)
	}
	if snapshot := strings.TrimSpace(req.ExpectedSnapshot); snapshot != strings.TrimSpace(proposal.Snapshot) {
		return fmt.Errorf("%w: proposal snapshot changed", convergence.ErrApprovalStale)
	}
	return nil
}

// boundaryEvidence records the expected and observed Git snapshot digests at a
// phase boundary. It is bounded metadata suitable for a parked receipt; it carries
// no finding prose or absolute paths.
type boundaryEvidence struct {
	Expected string
	Observed string
	Diverged bool
}

// classifyBoundarySnapshot decides whether a phase-boundary Git difference is an
// accepted, phase-owned change or an unexplained divergence. An owned change (the
// active phase holds a matching durable change event) is accepted; any other
// difference parks the segment as workspace_changed with expected/observed
// evidence. V1 does not attempt process-level attribution during an active phase.
func classifyBoundarySnapshot(
	expected, observed string,
	ownedChange bool,
) (*convergence.TerminalOutcome, boundaryEvidence) {
	evidence := boundaryEvidence{
		Expected: strings.TrimSpace(expected),
		Observed: strings.TrimSpace(observed),
	}
	if evidence.Expected == evidence.Observed || ownedChange {
		return nil, evidence
	}
	evidence.Diverged = true
	outcome := convergence.TerminalOutcome{
		Kind:   convergence.TerminalParked,
		Reason: convergence.ParkedWorkspaceChanged,
	}
	return &outcome, evidence
}

// admitPhaseUnderCancellation forbids starting any new phase once cancellation has
// been accepted. The active operation is signaled through existing runtime
// cancellation; this gate stops the state machine from admitting later work.
func admitPhaseUnderCancellation(cancelRequested bool) error {
	if cancelRequested {
		return fmt.Errorf("%w: no later phase admits after cancellation", errConvergenceCanceled)
	}
	return nil
}

// phaseCompletionAdmitted resolves a cancellation-versus-phase-completion race by
// durable sequence. A phase completion that committed strictly before the
// cancellation checkpoint is kept; a completion at or after the cancellation
// sequence is not admitted, so no phase begins after accepted cancellation.
func phaseCompletionAdmitted(completionSeq, cancelSeq uint64) bool {
	return completionSeq < cancelSeq
}
