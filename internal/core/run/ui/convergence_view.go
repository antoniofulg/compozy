package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/compozy/compozy/internal/api/contract"
)

// Convergence display status labels rendered in the header.
const (
	convergenceStatusPreparing = "PREPARING"
	convergenceStatusActive    = "ACTIVE"
	convergenceStatusClean     = "CLEAN"
	convergenceStatusParked    = "PARKED"
	convergenceStatusFailed    = "FAILED"
	convergenceStatusCanceled  = "CANCELED"

	convergenceSegmentActive = "active"
	convergenceFindingOpen   = "actionable"
)

// Condition glyphs. A met gate shows a check, a blocked gate a cross, and a
// not-yet-evaluated gate an open circle.
const (
	convergenceGlyphMet     = "✓"
	convergenceGlyphBlocked = "✕"
	convergenceGlyphPending = "○"
)

// convergenceConditionLabels maps a condition kind to its human label. The order
// of the slice below fixes the display order.
var convergenceConditionLabels = map[string]string{
	contract.ConvergenceConditionInitialVerification: "initial verification",
	contract.ConvergenceConditionActionableFindings:  "actionable findings",
	contract.ConvergenceConditionWorkspaceStable:     "workspace stable",
	contract.ConvergenceConditionCleanReview:         "clean review",
	contract.ConvergenceConditionCurrentVerification: "current verification",
	contract.ConvergenceConditionApprovalRequired:    "approval required",
}

// convergenceView is the fully projected, renderable state of one convergence
// snapshot. It is derived only from the projected snapshot, never from raw event
// prose or displayed counts, so a UI rendering it cannot infer clean or parked.
type convergenceView struct {
	version        int
	status         string
	round          int
	maxRounds      int
	phase          string
	targetLabel    string
	snapshot       string
	dirty          bool
	profile        string
	setup          string
	routes         []convergenceRouteLine
	limits         convergenceLimitsView
	conditions     []convergenceConditionView
	batches        []convergenceBatchView
	findings       []convergenceFindingView
	findingsShown  int
	findingsTotal  int
	unresolved     int
	handoff        convergenceHandoffView
	relations      []contract.ConvergenceRelation
	approval       *convergenceApprovalView
	approveEnabled bool
	resumeEnabled  bool
	terminalKind   string
	terminalReason string
	page           contract.ConvergencePage
}

type convergenceRouteLine struct {
	role   string
	route  string
	source string
	fallby string
}

type convergenceLimitsView struct {
	attempt          int
	maxAttempts      int
	noProgress       int
	maxNoProgress    int
	admissionElapsed time.Duration
	admissionLimit   time.Duration
	admissionActive  bool
}

type convergenceConditionView struct {
	kind   string
	label  string
	status string
	glyph  string
}

type convergenceBatchView struct {
	ordinal      int
	batchID      string
	status       string
	findingCount int
	evidenceRef  string
}

type convergenceFindingView struct {
	fingerprint string
	state       string
	severity    string
	attempts    int
	evidenceRef string
	open        bool
}

type convergenceHandoffView struct {
	branch          string
	worktree        string
	snapshot        string
	dirty           bool
	autoCommit      bool
	unresolved      int
	terminalKind    string
	terminalReason  string
	resumeAvailable bool
	resumeCursor    string
	receiptPath     string
}

type convergenceApprovalView struct {
	proposalID  string
	fingerprint string
	action      string
	snapshot    string
	evidenceRef string
}

// projectConvergenceView derives the renderable view from a projected snapshot.
// now supplies the review-admission elapsed clock so the projection stays pure.
func projectConvergenceView(snap contract.ConvergenceSnapshot, now time.Time) convergenceView {
	view := convergenceView{
		version:       snap.Version,
		status:        convergenceStatusLabel(snap),
		round:         snap.Phase.Round,
		maxRounds:     snap.Config.Limits.MaxReviewRounds,
		phase:         strings.ToUpper(strings.TrimSpace(snap.Phase.Kind)),
		targetLabel:   convergenceTargetLabel(snap.Target),
		snapshot:      shortSHA(snap.Handoff.Snapshot),
		dirty:         snap.Handoff.Dirty,
		profile:       firstNonEmpty(snap.Config.Profile, "default"),
		setup:         firstNonEmpty(snap.Config.ModelSetup, "inherited"),
		routes:        convergenceRouteLines(snap.Routes),
		limits:        convergenceLimits(snap, now),
		conditions:    convergenceConditionViews(snap.Conditions),
		batches:       convergenceBatchViews(snap.Batches),
		findings:      convergenceFindingViews(snap.Findings),
		findingsShown: snap.Page.Findings.Shown,
		findingsTotal: snap.Page.Findings.Total,
		unresolved:    snap.UnresolvedCount,
		handoff:       convergenceHandoffFromSnapshot(snap.Handoff),
		relations:     snap.Relations,
		page:          snap.Page,
	}
	if approval := pendingConvergenceApproval(snap.Approvals); approval != nil {
		view.approval = approval
		view.approveEnabled = true
	}
	view.resumeEnabled = snap.Handoff.ResumeAvailable
	if snap.Terminal != nil {
		view.terminalKind = snap.Terminal.Kind
		view.terminalReason = snap.Terminal.Reason
	}
	return view
}

func convergenceStatusLabel(snap contract.ConvergenceSnapshot) string {
	// A terminal kind ("clean"/"parked"/"failed"/"canceled") uppercases directly to
	// its display label, so no non-clean stop is ever rendered as another state.
	if snap.Terminal != nil {
		if kind := strings.TrimSpace(snap.Terminal.Kind); kind != "" {
			return strings.ToUpper(kind)
		}
	}
	if snap.Segment.State == convergenceSegmentActive {
		return convergenceStatusActive
	}
	return convergenceStatusPreparing
}

func convergenceTargetLabel(target contract.ConvergenceTarget) string {
	parts := make([]string, 0, 2)
	if id := strings.TrimSpace(target.WorkspaceID); id != "" {
		parts = append(parts, id)
	}
	scope := firstNonEmpty(strings.TrimSpace(target.TaskGroupID), strings.TrimSpace(target.ExecutionScope))
	if scope != "" {
		parts = append(parts, scope)
	}
	if len(parts) == 0 {
		return "unknown target"
	}
	return strings.Join(parts, " / ")
}

func convergenceRouteLines(routes []contract.ConvergenceRoute) []convergenceRouteLine {
	if len(routes) == 0 {
		return nil
	}
	lines := make([]convergenceRouteLine, 0, len(routes))
	for i := range routes {
		lines = append(lines, convergenceRouteLine{
			role:   strings.TrimSpace(routes[i].Role),
			route:  firstNonEmpty(strings.TrimSpace(routes[i].Selected), strings.TrimSpace(routes[i].Primary)),
			source: strings.TrimSpace(routes[i].ConfigurationSource),
			fallby: strings.TrimSpace(routes[i].FallbackReason),
		})
	}
	return lines
}

func convergenceLimits(snap contract.ConvergenceSnapshot, now time.Time) convergenceLimitsView {
	limits := convergenceLimitsView{
		attempt:        snap.Phase.Attempt,
		maxAttempts:    snap.Config.Limits.MaxFindingAttempts,
		maxNoProgress:  snap.Config.Limits.NoProgressRounds,
		admissionLimit: parseConvergenceDuration(snap.Config.Limits.ReviewAdmissionTimeout),
	}
	if len(snap.Rounds) > 0 {
		limits.noProgress = snap.Rounds[len(snap.Rounds)-1].Progress.NoProgressCount
		admittedAt := snap.Rounds[0].AdmittedAt
		if !admittedAt.IsZero() {
			limits.admissionActive = true
			if elapsed := now.Sub(admittedAt); elapsed > 0 {
				limits.admissionElapsed = elapsed
			}
		}
	}
	return limits
}

func convergenceConditionViews(conditions []contract.ConvergenceCondition) []convergenceConditionView {
	if len(conditions) == 0 {
		return nil
	}
	views := make([]convergenceConditionView, 0, len(conditions))
	for i := range conditions {
		views = append(views, convergenceConditionView{
			kind:   conditions[i].Kind,
			label:  firstNonEmpty(convergenceConditionLabels[conditions[i].Kind], conditions[i].Kind),
			status: conditions[i].Status,
			glyph:  convergenceConditionGlyph(conditions[i].Status),
		})
	}
	return views
}

func convergenceConditionGlyph(status string) string {
	switch status {
	case contract.ConvergenceConditionMet:
		return convergenceGlyphMet
	case contract.ConvergenceConditionBlocked:
		return convergenceGlyphBlocked
	default:
		return convergenceGlyphPending
	}
}

func convergenceBatchViews(batches []contract.ConvergenceBatch) []convergenceBatchView {
	if len(batches) == 0 {
		return nil
	}
	views := make([]convergenceBatchView, 0, len(batches))
	for i := range batches {
		views = append(views, convergenceBatchView{
			ordinal:      i + 1,
			batchID:      strings.TrimSpace(batches[i].BatchID),
			status:       strings.ToUpper(firstNonEmpty(strings.TrimSpace(batches[i].Status), "pending")),
			findingCount: len(batches[i].FindingFingerprints),
			evidenceRef:  strings.TrimSpace(batches[i].AffectedPathsRef),
		})
	}
	return views
}

func convergenceFindingViews(findings []contract.ConvergenceFinding) []convergenceFindingView {
	if len(findings) == 0 {
		return nil
	}
	views := make([]convergenceFindingView, 0, len(findings))
	for i := range findings {
		views = append(views, convergenceFindingView{
			fingerprint: shortSHA(findings[i].Fingerprint),
			state:       strings.TrimSpace(findings[i].State),
			severity:    strings.TrimSpace(findings[i].Severity),
			attempts:    findings[i].Attempts,
			evidenceRef: strings.TrimSpace(findings[i].EvidenceRef),
			open:        findings[i].State == convergenceFindingOpen,
		})
	}
	return views
}

func convergenceHandoffFromSnapshot(handoff contract.ConvergenceHandoff) convergenceHandoffView {
	return convergenceHandoffView{
		branch:          strings.TrimSpace(handoff.Branch),
		worktree:        strings.TrimSpace(handoff.Worktree),
		snapshot:        shortSHA(handoff.Snapshot),
		dirty:           handoff.Dirty,
		autoCommit:      handoff.AutoCommit,
		unresolved:      handoff.UnresolvedCount,
		terminalKind:    strings.TrimSpace(handoff.TerminalKind),
		terminalReason:  strings.TrimSpace(handoff.TerminalReason),
		resumeAvailable: handoff.ResumeAvailable,
		resumeCursor:    strings.TrimSpace(handoff.ResumeCursor),
		receiptPath:     strings.TrimSpace(handoff.ReceiptPath),
	}
}

// pendingConvergenceApproval returns the first approval still awaiting a decision.
func pendingConvergenceApproval(approvals []contract.ConvergenceApproval) *convergenceApprovalView {
	for i := range approvals {
		if strings.TrimSpace(approvals[i].Decision) != "" {
			continue
		}
		return &convergenceApprovalView{
			proposalID:  strings.TrimSpace(approvals[i].ProposalID),
			fingerprint: strings.TrimSpace(approvals[i].Fingerprint),
			action:      strings.TrimSpace(approvals[i].Action),
			snapshot:    shortSHA(approvals[i].Snapshot),
			evidenceRef: strings.TrimSpace(approvals[i].EvidenceRef),
		}
	}
	return nil
}

func parseConvergenceDuration(value string) time.Duration {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	if d, err := time.ParseDuration(trimmed); err == nil {
		return d
	}
	return 0
}

// convergenceMinutes formats a duration as whole minutes for the admission line.
func convergenceMinutes(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
