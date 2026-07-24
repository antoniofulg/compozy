package ui

import (
	"context"
	"strings"

	"github.com/compozy/compozy/internal/api/contract"

	tea "charm.land/bubbletea/v2"
)

// convergenceAction is a user interaction distinct from every other. Approval and
// resume are deliberately separate: an approval decision never resumes the run.
type convergenceAction int

const (
	convergenceActionNone convergenceAction = iota
	convergenceActionApprove
	convergenceActionReject
	convergenceActionResume
)

// convergencePrompt is an in-progress approve, reject, or resume interaction. The
// approve and reject prompts require a non-empty reason before they submit.
type convergencePrompt struct {
	active bool
	action convergenceAction
	reason string
}

func convergenceActionLabel(action convergenceAction) string {
	switch action {
	case convergenceActionApprove:
		return "approve"
	case convergenceActionReject:
		return "reject"
	case convergenceActionResume:
		return "resume"
	default:
		return "action"
	}
}

func (m *convergenceModel) handleKey(v tea.KeyPressMsg) tea.Cmd {
	if m.quitDialog.Active {
		return m.handleQuitDialogKey(v)
	}
	if m.prompt.active {
		return m.handlePromptKey(v)
	}
	switch v.Code {
	case tea.KeyUp:
		m.scrollBy(-1)
		return nil
	case tea.KeyDown:
		m.scrollBy(1)
		return nil
	case tea.KeyHome:
		m.scroll = 0
		return nil
	case tea.KeyEnd:
		m.scroll = m.maxScroll()
		return nil
	}
	switch strings.ToLower(v.String()) {
	case keyCtrlC, "q":
		return m.handleQuitKey()
	case "k":
		m.scrollBy(-1)
	case "j":
		m.scrollBy(1)
	case keyPageUp:
		m.scrollBy(-convergenceScrollPage)
	case keyPageDown:
		m.scrollBy(convergenceScrollPage)
	case "a":
		m.beginApproval(convergenceActionApprove)
	case "r":
		m.beginApproval(convergenceActionReject)
	case "s":
		m.beginResume()
	default:
	}
	return nil
}

func (m *convergenceModel) beginApproval(action convergenceAction) {
	if !m.view.approveEnabled {
		m.lastError = "no approval is pending"
		return
	}
	m.lastError = ""
	m.prompt = convergencePrompt{active: true, action: action}
}

func (m *convergenceModel) beginResume() {
	if !m.view.resumeEnabled {
		m.lastError = "resume is not available for this segment"
		return
	}
	m.lastError = ""
	m.prompt = convergencePrompt{active: true, action: convergenceActionResume}
}

func (m *convergenceModel) handlePromptKey(v tea.KeyPressMsg) tea.Cmd {
	switch v.String() {
	case keyEscape:
		m.prompt = convergencePrompt{}
		return nil
	case keyEnter:
		return m.submitPrompt()
	case "backspace":
		if m.prompt.action != convergenceActionResume {
			m.prompt.reason = trimLastRune(m.prompt.reason)
		}
		return nil
	default:
		if m.prompt.action != convergenceActionResume && v.Text != "" {
			m.prompt.reason += v.Text
		}
		return nil
	}
}

func (m *convergenceModel) submitPrompt() tea.Cmd {
	action := m.prompt.action
	reason := strings.TrimSpace(m.prompt.reason)
	switch action {
	case convergenceActionApprove, convergenceActionReject:
		if reason == "" {
			// A reason is mandatory for both approve and reject; keep the prompt open.
			m.lastError = "a reason is required to decide this proposal"
			return nil
		}
		req, ok := m.buildApprovalRequest(action, reason)
		m.prompt = convergencePrompt{}
		if !ok {
			m.lastError = "approval is no longer available"
			return nil
		}
		return m.approveCmd(action, req)
	case convergenceActionResume:
		req, ok := m.buildResumeRequest()
		m.prompt = convergencePrompt{}
		if !ok {
			m.lastError = "resume is no longer available"
			return nil
		}
		return m.resumeCmd(req)
	default:
		m.prompt = convergencePrompt{}
		return nil
	}
}

func (m *convergenceModel) buildApprovalRequest(
	action convergenceAction,
	reason string,
) (contract.ApprovalDecisionRequest, bool) {
	proposal, ok := m.pendingApproval()
	if !ok {
		return contract.ApprovalDecisionRequest{}, false
	}
	decision := contract.ConvergenceDecisionApprove
	if action == convergenceActionReject {
		decision = contract.ConvergenceDecisionReject
	}
	return contract.ApprovalDecisionRequest{
		ProposalID:          proposal.ProposalID,
		Decision:            decision,
		Reason:              reason,
		ExpectedFingerprint: proposal.Fingerprint,
		ExpectedSnapshot:    proposal.Snapshot,
	}, true
}

func (m *convergenceModel) buildResumeRequest() (contract.ConvergenceResumeRequest, bool) {
	if !m.snapshot.Handoff.ResumeAvailable {
		return contract.ConvergenceResumeRequest{}, false
	}
	cursor := strings.TrimSpace(m.snapshot.Handoff.ResumeCursor)
	if cursor == "" {
		return contract.ConvergenceResumeRequest{}, false
	}
	return contract.ConvergenceResumeRequest{ExpectedCursor: cursor}, true
}

// pendingApproval returns the first proposal still awaiting a decision, with its
// full fingerprint and snapshot for binding the decision request.
func (m *convergenceModel) pendingApproval() (contract.ConvergenceApproval, bool) {
	for i := range m.snapshot.Approvals {
		if strings.TrimSpace(m.snapshot.Approvals[i].Decision) == "" {
			return m.snapshot.Approvals[i], true
		}
	}
	return contract.ConvergenceApproval{}, false
}

func (m *convergenceModel) approveCmd(action convergenceAction, req contract.ApprovalDecisionRequest) tea.Cmd {
	decide := m.approve
	factory := m.newActionContext
	if decide == nil {
		return func() tea.Msg {
			return convergenceActionResultMsg{action: action, err: errConvergenceActionUnavailable}
		}
	}
	return func() tea.Msg {
		ctx, done := actionContext(factory)
		defer done()
		return convergenceActionResultMsg{action: action, err: decide(ctx, req)}
	}
}

func (m *convergenceModel) resumeCmd(req contract.ConvergenceResumeRequest) tea.Cmd {
	resume := m.resume
	factory := m.newActionContext
	if resume == nil {
		return func() tea.Msg {
			return convergenceActionResultMsg{action: convergenceActionResume, err: errConvergenceActionUnavailable}
		}
	}
	return func() tea.Msg {
		ctx, done := actionContext(factory)
		defer done()
		return convergenceActionResultMsg{action: convergenceActionResume, err: resume(ctx, req)}
	}
}

func (m *convergenceModel) fireCancelCmd() tea.Cmd {
	cancel := m.cancel
	factory := m.newActionContext
	if cancel == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, done := actionContext(factory)
		defer done()
		return convergenceActionResultMsg{action: convergenceActionNone, err: cancel(ctx)}
	}
}

func (m *convergenceModel) scrollBy(delta int) {
	m.scroll += delta
	m.clampScroll()
}

func (m *convergenceModel) clampScroll() {
	if m.scroll < 0 {
		m.scroll = 0
	}
	if maxScroll := m.maxScroll(); m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

// maxScroll bounds finding-list scrolling so navigation never runs past the last
// retained finding. Current actionable findings are never dropped by the bounded
// projection, so scrolling only pages through already-retained entries.
func (m *convergenceModel) maxScroll() int {
	visible := convergenceVisibleFindingRows
	if len(m.view.findings) <= visible {
		return 0
	}
	return len(m.view.findings) - visible
}

func (m *convergenceModel) handleQuitKey() tea.Cmd {
	if m.cfg != nil && m.cfg.DetachOnly {
		return tea.Quit
	}
	if m.isTerminal() {
		return tea.Quit
	}
	m.quitDialog.Open()
	return nil
}

func (m *convergenceModel) handleQuitDialogKey(v tea.KeyPressMsg) tea.Cmd {
	switch strings.ToLower(v.String()) {
	case keyLeft, "h", keyShiftTab:
		m.quitDialog.Move(-1)
		return nil
	case keyRight, "l", keyTab:
		m.quitDialog.Move(1)
		return nil
	case keyEnter, "q", keyCtrlC:
		return m.confirmQuitDialog()
	case keyEscape:
		m.quitDialog.Close()
		return nil
	default:
		return nil
	}
}

func (m *convergenceModel) confirmQuitDialog() tea.Cmd {
	selected := m.quitDialog.Selected
	m.quitDialog.Close()
	switch selected {
	case quitDialogActionClose:
		return tea.Quit
	case quitDialogActionStop:
		if cmd := m.fireCancelCmd(); cmd != nil {
			return tea.Batch(cmd, tea.Quit)
		}
		return tea.Quit
	default:
		return nil
	}
}

func actionContext(factory func() (context.Context, context.CancelFunc)) (context.Context, context.CancelFunc) {
	if factory != nil {
		return factory()
	}
	return defaultConvergenceActionContext()
}

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	return string(runes[:len(runes)-1])
}
