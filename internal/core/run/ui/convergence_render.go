package ui

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/compozy/compozy/internal/api/contract"
	"github.com/compozy/compozy/internal/charmtheme"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const convergenceVisibleFindingRows = 8

var errConvergenceActionUnavailable = errors.New("convergence action is not available")

func (m *convergenceModel) View() tea.View {
	if m.quitDialog.Active {
		return m.renderQuitDialogView()
	}
	return m.renderRoot(m.renderPanel())
}

func (m *convergenceModel) renderRoot(content string) tea.View {
	v := tea.NewView(rootScreenStyle(m.width, m.height).Render(content))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *convergenceModel) renderPanel() string {
	width := max(m.width, 1)
	height := max(m.height, 1)
	panelWidth := max(width-2, 24)
	statusColor := convergenceStatusColor(m.view.status)
	innerStyle := techPanelStyle(panelWidth, statusColor).Padding(1, 2)
	innerWidth := max(panelWidth-innerStyle.GetHorizontalFrameSize(), 1)

	var lines []string
	add := func(rendered string) {
		lines = append(lines, renderOwnedLineKnownOwned(innerWidth, rendered))
	}
	blank := func() { add("") }

	add(renderTechLabel("convergence"))
	blank()
	if !m.hasSnapshot {
		add(styleMutedText.Render(truncateString("Loading convergence snapshot...", innerWidth)))
		panel := innerStyle.Render(strings.Join(lines, "\n"))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, panel)
	}

	m.appendHeaderLines(add, innerWidth)
	blank()
	m.appendConditionLines(add, innerWidth)
	blank()
	m.appendBatchLines(add, innerWidth)
	m.appendFindingLines(add, innerWidth)
	blank()
	m.appendApprovalLines(add, innerWidth)
	m.appendHandoffLines(add, innerWidth)
	m.appendRelationLines(add, innerWidth)
	blank()
	m.appendFooterLines(add, innerWidth)

	panel := innerStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, panel)
}

func (m *convergenceModel) appendHeaderLines(add func(string), width int) {
	v := m.view
	header := fmt.Sprintf(
		"CONVERGENCE  round %d/%d  phase %s  status %s",
		v.round, v.maxRounds, firstNonEmpty(v.phase, "—"), v.status,
	)
	add(lipgloss.NewStyle().Bold(true).Foreground(convergenceStatusColor(v.status)).Render(
		truncateString(header, width),
	))
	snap := firstNonEmpty(v.snapshot, "—")
	if v.dirty {
		snap += " dirty"
	}
	add(styleBodyText.Render(truncateString(
		fmt.Sprintf("%s%s   Snapshot %s", convergenceLabel("Target"), v.targetLabel, snap), width,
	)))
	add(styleMutedText.Render(truncateString(
		fmt.Sprintf("%s%s   Setup %s", convergenceLabel("Profile"), v.profile, v.setup), width,
	)))
	for _, route := range v.routes {
		line := fmt.Sprintf("%s%s: %s", convergenceLabel("Route"), route.role, firstNonEmpty(route.route, "—"))
		if route.source != "" {
			line += "  (" + route.source + ")"
		}
		if route.fallby != "" {
			line += "  fallback " + route.fallby
		}
		add(styleMutedText.Render(truncateString(line, width)))
	}
	add(styleMutedText.Render(truncateString(m.limitsLine(), width)))
}

func (m *convergenceModel) limitsLine() string {
	l := m.view.limits
	admission := "off"
	if l.admissionActive {
		admission = convergenceMinutes(l.admissionElapsed) + "/" + convergenceMinutes(l.admissionLimit)
	}
	return fmt.Sprintf(
		"%sattempts %d/%d  no-progress %d/%d  admission %s",
		convergenceLabel("Limits"), l.attempt, l.maxAttempts, l.noProgress, l.maxNoProgress, admission,
	)
}

func (m *convergenceModel) appendConditionLines(add func(string), width int) {
	add(styleBodyText.Render("Conditions"))
	if len(m.view.conditions) == 0 {
		add(styleMutedText.Render(truncateString("No conditions projected yet.", width)))
		return
	}
	cells := make([]string, 0, len(m.view.conditions))
	for _, cond := range m.view.conditions {
		glyphStyle := lipgloss.NewStyle().Foreground(convergenceConditionColor(cond.status))
		cells = append(cells, glyphStyle.Render(cond.glyph)+" "+styleMutedText.Render(cond.label))
	}
	for start := 0; start < len(cells); start += 3 {
		end := min(start+3, len(cells))
		add(truncateString(strings.Join(cells[start:end], "   "), width))
	}
}

func (m *convergenceModel) appendBatchLines(add func(string), width int) {
	if len(m.view.batches) == 0 {
		return
	}
	add(styleBodyText.Render("Batches"))
	for _, batch := range m.view.batches {
		line := fmt.Sprintf("%d %s  %d findings", batch.ordinal, batch.status, batch.findingCount)
		if batch.evidenceRef != "" {
			line += "  " + batch.evidenceRef
		}
		add(styleMutedText.Render(truncateString(line, width)))
	}
	add("")
}

func (m *convergenceModel) appendFindingLines(add func(string), width int) {
	v := m.view
	summary := fmt.Sprintf(
		"%sshown %d/%d  unresolved %d",
		convergenceLabel("Findings"), v.findingsShown, v.findingsTotal, v.unresolved,
	)
	if v.page.Findings.Truncated {
		summary += "  (older truncated)"
	}
	add(styleBodyText.Render(truncateString(summary, width)))
	if len(v.findings) == 0 {
		add(styleMutedText.Render(truncateString("No findings recorded.", width)))
		return
	}
	start := min(m.scroll, m.maxScroll())
	end := min(start+convergenceVisibleFindingRows, len(v.findings))
	for _, finding := range v.findings[start:end] {
		marker := "·"
		markerStyle := styleMutedText
		if finding.open {
			marker = "!"
			markerStyle = lipgloss.NewStyle().Foreground(colorWarning)
		}
		line := fmt.Sprintf(
			"%s %s  %s  attempts %d  %s",
			marker, finding.severity, finding.state, finding.attempts, finding.fingerprint,
		)
		add(markerStyle.Render(truncateString(line, width)))
	}
	if end < len(v.findings) {
		add(styleDimText.Render(truncateString(
			fmt.Sprintf("… %d more (scroll ↑/↓)", len(v.findings)-end), width,
		)))
	}
}

func (m *convergenceModel) appendApprovalLines(add func(string), width int) {
	if m.view.approval == nil {
		return
	}
	approval := m.view.approval
	add(lipgloss.NewStyle().Bold(true).Foreground(colorWarning).Render(
		truncateString("Approval required", width),
	))
	line := fmt.Sprintf(
		"%saction %s  proposal %s",
		convergenceLabel(""), firstNonEmpty(approval.action, "—"), shortSHA(approval.proposalID),
	)
	if approval.snapshot != "" {
		line += "  snapshot " + approval.snapshot
	}
	add(styleMutedText.Render(truncateString(line, width)))
	if approval.evidenceRef != "" {
		add(styleMutedText.Render(truncateString(
			convergenceLabel("")+"evidence "+approval.evidenceRef, width,
		)))
	}
	add("")
}

func (m *convergenceModel) appendHandoffLines(add func(string), width int) {
	h := m.view.handoff
	add(styleBodyText.Render("Handoff"))
	branch := fmt.Sprintf("%sbranch %s  worktree %s", convergenceLabel(""),
		firstNonEmpty(h.branch, "—"), firstNonEmpty(h.worktree, "—"))
	if h.dirty {
		branch += "  (dirty)"
	}
	add(styleMutedText.Render(truncateString(branch, width)))
	if h.terminalKind != "" {
		terminal := convergenceLabel("") + "terminal " + h.terminalKind
		if h.terminalReason != "" {
			terminal += ":" + h.terminalReason
		}
		add(styleMutedText.Render(truncateString(terminal, width)))
	}
	resume := convergenceLabel("") + "resume "
	if h.resumeAvailable {
		resume += "available"
	} else {
		resume += "unavailable"
	}
	if h.receiptPath != "" {
		resume += "  receipt " + h.receiptPath
	}
	add(styleMutedText.Render(truncateString(resume, width)))
}

func (m *convergenceModel) appendRelationLines(add func(string), width int) {
	if len(m.view.relations) == 0 {
		return
	}
	parts := make([]string, 0, len(m.view.relations))
	for _, rel := range m.view.relations {
		parts = append(parts, rel.Kind+" "+shortSHA(rel.RunID))
	}
	add(styleMutedText.Render(truncateString(
		convergenceLabel("Related")+strings.Join(parts, "  "), width,
	)))
}

func (m *convergenceModel) appendFooterLines(add func(string), width int) {
	if m.prompt.active {
		m.appendPromptLines(add, width)
		return
	}
	if errText := strings.TrimSpace(m.lastError); errText != "" {
		add(lipgloss.NewStyle().Foreground(colorError).Render(truncateString(errText, width)))
	}
	hints := make([]string, 0, 4)
	if m.view.approveEnabled {
		hints = append(hints, charmtheme.Keycap("a")+" approve", charmtheme.Keycap("r")+" reject")
	}
	if m.view.resumeEnabled {
		hints = append(hints, charmtheme.Keycap("s")+" resume")
	}
	hints = append(hints, charmtheme.Keycap("↑/↓")+" scroll", charmtheme.Keycap("q")+" quit")
	add(styleDimText.Render(truncateString(strings.Join(hints, "   "), width)))
	if m.isTerminal() && !m.view.resumeEnabled {
		add(styleDimText.Render(truncateString("Segment is terminal; resume is not available.", width)))
	}
}

func (m *convergenceModel) appendPromptLines(add func(string), width int) {
	switch m.prompt.action {
	case convergenceActionApprove, convergenceActionReject:
		verb := capitalizeFirst(convergenceActionLabel(m.prompt.action))
		add(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(
			truncateString(fmt.Sprintf("%s proposal — a reason is required", verb), width),
		))
		add(styleBodyText.Render(truncateString("Reason: "+m.prompt.reason+"▏", width)))
		add(styleDimText.Render(truncateString(
			charmtheme.Keycap("enter")+" submit   "+charmtheme.Keycap("esc")+" cancel", width,
		)))
	case convergenceActionResume:
		add(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(
			truncateString("Resume parked segment with the frozen profile and routes?", width),
		))
		add(styleMutedText.Render(truncateString(fmt.Sprintf(
			"profile %s  setup %s  cursor %s",
			m.view.profile, m.view.setup, shortSHA(m.snapshot.Handoff.ResumeCursor),
		), width)))
		add(styleDimText.Render(truncateString(
			charmtheme.Keycap("enter")+" confirm   "+charmtheme.Keycap("esc")+" cancel", width,
		)))
	default:
	}
}

func (m *convergenceModel) renderQuitDialogView() tea.View {
	panelWidth := min(max(m.width-4, 1), quitDialogMaxWidth)
	panelStyle := techPanelStyle(panelWidth, colorBorderFocus).Padding(1, 2)
	innerWidth := max(panelWidth-panelStyle.GetHorizontalFrameSize(), 1)
	lines := []string{
		renderOwnedLineKnownOwned(innerWidth, lipgloss.NewStyle().Bold(true).Foreground(colorAccentDeep).Render(
			truncateString("Leave Active Convergence?", innerWidth),
		)),
		renderOwnedLineKnownOwned(innerWidth, ""),
		renderOwnedLineKnownOwned(innerWidth, styleBodyText.Render(
			truncateString("This convergence run is still active.", innerWidth),
		)),
		renderOwnedLineKnownOwned(innerWidth, styleMutedText.Render(
			truncateString("Close the TUI and keep converging in the daemon.", innerWidth),
		)),
		renderOwnedLineKnownOwned(innerWidth, styleMutedText.Render(
			truncateString("Choose Stop Run to cancel this convergence run.", innerWidth),
		)),
		renderOwnedLineKnownOwned(innerWidth, ""),
		renderOwnedBlock(innerWidth, m.renderQuitDialogActions(innerWidth)),
		renderOwnedLineKnownOwned(innerWidth, ""),
		renderOwnedLineKnownOwned(innerWidth, styleDimText.Render(
			truncateString("[enter/q] confirm  [tab/left/right] choice  [esc] back", innerWidth),
		)),
	}
	panel := panelStyle.Render(strings.Join(lines, "\n"))
	content := lipgloss.Place(max(m.width, 1), max(m.height, 1), lipgloss.Center, lipgloss.Center, panel)
	return m.renderRoot(content)
}

func (m *convergenceModel) renderQuitDialogActions(width int) string {
	actions := []string{
		m.renderQuitDialogAction("Close TUI", quitDialogActionClose),
		m.renderQuitDialogAction("Stop Run", quitDialogActionStop),
		m.renderQuitDialogAction("Cancel", quitDialogActionCancel),
	}
	if width < 44 {
		return strings.Join(actions, "\n")
	}
	return strings.Join(actions, renderGap(1))
}

func (m *convergenceModel) renderQuitDialogAction(label string, action quitDialogAction) string {
	baseStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	if m.quitDialog.Selected == action {
		return baseStyle.Foreground(colorBgSurface).Background(colorAccent).Render(label)
	}
	return baseStyle.Foreground(colorFgBright).Render(label)
}

func convergenceLabel(label string) string {
	return fmt.Sprintf("%-9s", label)
}

func convergenceStatusColor(status string) color.Color {
	switch status {
	case convergenceStatusClean:
		return colorSuccess
	case convergenceStatusParked, convergenceStatusCanceled:
		return colorWarning
	case convergenceStatusFailed:
		return colorError
	case convergenceStatusActive:
		return colorAccentAlt
	default:
		return colorMuted
	}
}

func convergenceConditionColor(status string) color.Color {
	switch status {
	case contract.ConvergenceConditionMet:
		return colorSuccess
	case contract.ConvergenceConditionBlocked:
		return colorError
	default:
		return colorMuted
	}
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
