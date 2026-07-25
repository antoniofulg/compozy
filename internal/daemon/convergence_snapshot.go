package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/compozy/compozy/internal/api/contract"
	apicore "github.com/compozy/compozy/internal/api/core"
	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/core/model"
	"github.com/compozy/compozy/internal/store/convergencestore"
	"github.com/compozy/compozy/internal/store/globaldb"
	"github.com/compozy/compozy/internal/store/rundb"
	"github.com/compozy/compozy/pkg/compozy/events"
	"github.com/compozy/compozy/pkg/compozy/events/kinds"
)

var _ apicore.ConvergenceService = (*RunManager)(nil)

// ConvergenceSnapshot builds the bounded, versioned convergence snapshot for one
// convergence run from its canonical run.db projection. A run without a convergence
// projection (wrong mode or unknown run) maps to the not-found contract so the
// transport returns a precise 404 without leaking repository paths.
func (m *RunManager) ConvergenceSnapshot(
	ctx context.Context,
	runID string,
) (contract.ConvergenceSnapshotResponse, error) {
	trimmed := strings.TrimSpace(runID)
	if trimmed == "" {
		return contract.ConvergenceSnapshotResponse{}, fmt.Errorf(
			"%w: convergence run id is required", globaldb.ErrRunNotFound)
	}
	readCtx := detachContext(ctx)
	if _, err := m.registeredConvergenceRun(readCtx, trimmed); err != nil {
		return contract.ConvergenceSnapshotResponse{}, err
	}
	lease, err := m.acquireRunDB(readCtx, trimmed)
	if err != nil {
		return contract.ConvergenceSnapshotResponse{}, err
	}
	defer func() {
		_ = lease.Close()
	}()
	snapshot, err := convergencestore.New(lease.DB()).Snapshot(readCtx, trimmed)
	if err != nil {
		if errors.Is(err, rundb.ErrConvergenceRunNotFound) {
			return contract.ConvergenceSnapshotResponse{}, fmt.Errorf(
				"%w: convergence run %q", globaldb.ErrRunNotFound, trimmed)
		}
		return contract.ConvergenceSnapshotResponse{}, err
	}
	opts := contract.DefaultConvergenceProjectionOptions()
	opts.Children = m.convergenceContinuationRelations(readCtx, snapshot)
	return contract.NewConvergenceSnapshotResponse(snapshot, opts), nil
}

// DecideConvergenceApproval records one exact, reasoned decision against the
// proposal and snapshot shown to the user. The parked segment remains terminal;
// resume is a separate operation.
func (m *RunManager) DecideConvergenceApproval(
	ctx context.Context,
	runID string,
	req contract.ApprovalDecisionRequest,
) (retErr error) {
	if m == nil {
		return errors.New("daemon: run manager is required")
	}
	trimmedRunID := strings.TrimSpace(runID)
	defer func() {
		retErr = convergenceControlError(retErr, map[string]string{
			"run_id":      trimmedRunID,
			"proposal_id": strings.TrimSpace(req.ProposalID),
		})
	}()
	if trimmedRunID == "" {
		return fmt.Errorf("%w: proposal run id required", errConvergenceApprovalInvalid)
	}

	m.convergenceControlMu.Lock()
	defer m.convergenceControlMu.Unlock()

	if _, err := m.registeredConvergenceRun(ctx, trimmedRunID); err != nil {
		return err
	}
	lease, err := m.acquireRunDB(ctx, trimmedRunID)
	if err != nil {
		return err
	}
	defer func() {
		_ = lease.Close()
	}()
	store := convergencestore.New(lease.DB())
	snapshot, err := store.Snapshot(ctx, trimmedRunID)
	if err != nil {
		return err
	}
	proposal, err := convergenceApprovalProposal(snapshot, req.ProposalID)
	if err != nil {
		return err
	}
	decision, err := decideApproval(
		proposal,
		req,
	)
	if err != nil {
		return err
	}
	if decision.Replayed {
		return nil
	}

	outcome := "approved"
	if decision.Proposal.Decision == contract.ConvergenceDecisionReject {
		outcome = "rejected"
	}
	requestID := firstNonBlankConvergenceID(req.ClientRequestID, snapshot.RequestID, req.ProposalID)
	_, err = lease.DB().AppendSyntheticEvent(
		ctx,
		events.EventKindConvergenceApprovalDecided,
		kinds.ConvergenceApprovalDecidedPayload{
			ConvergenceIdentifiers: kinds.ConvergenceIdentifiers{
				RequestID:     requestID,
				ActorID:       "local-user",
				ResourceID:    decision.Proposal.ProposalID,
				CorrelationID: snapshot.ConvergenceID,
			},
			ProposalFingerprint: decision.Proposal.Fingerprint,
			Snapshot:            decision.Proposal.Snapshot,
			Decision:            decision.Proposal.Decision,
			Reason:              decision.Proposal.Reason,
			Outcome:             outcome,
		},
	)
	if err != nil {
		return fmt.Errorf("record convergence approval decision: %w", err)
	}
	return nil
}

func convergenceApprovalProposal(
	snapshot convergence.Snapshot,
	proposalID string,
) (convergence.ApprovalProposal, error) {
	if snapshot.Terminal == nil ||
		snapshot.Terminal.Kind != convergence.TerminalParked ||
		snapshot.Terminal.Reason != convergence.ParkedApprovalRequired {
		return convergence.ApprovalProposal{},
			fmt.Errorf("%w: segment is not parked for approval", convergence.ErrNotParked)
	}
	proposal, ok := convergenceProposalByID(snapshot.Approvals, proposalID)
	if !ok {
		return convergence.ApprovalProposal{}, fmt.Errorf("%w: proposal not found", convergence.ErrApprovalStale)
	}
	return proposal, nil
}

func convergenceProposalByID(
	proposals []convergence.ApprovalProposal,
	proposalID string,
) (convergence.ApprovalProposal, bool) {
	trimmed := strings.TrimSpace(proposalID)
	for i := range proposals {
		if strings.TrimSpace(proposals[i].ProposalID) == trimmed {
			return proposals[i], true
		}
	}
	return convergence.ApprovalProposal{}, false
}

func firstNonBlankConvergenceID(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "convergence-control"
}

// ResumeConvergence atomically claims the parked cursor in the consumed
// segment's canonical store, then creates or replays one linked continuation.
func (m *RunManager) ResumeConvergence(
	ctx context.Context,
	runID string,
	req contract.ConvergenceResumeRequest,
) (run apicore.Run, retErr error) {
	if m == nil {
		return apicore.Run{}, errors.New("daemon: run manager is required")
	}
	trimmedRunID := strings.TrimSpace(runID)
	defer func() {
		retErr = convergenceControlError(retErr, map[string]string{"run_id": trimmedRunID})
	}()
	if trimmedRunID == "" {
		return apicore.Run{}, fmt.Errorf("%w: convergence run id required", convergence.ErrNotParked)
	}

	m.convergenceControlMu.Lock()
	defer m.convergenceControlMu.Unlock()

	sourceRow, err := m.registeredConvergenceRun(ctx, trimmedRunID)
	if err != nil {
		return apicore.Run{}, err
	}
	lease, err := m.acquireRunDB(ctx, trimmedRunID)
	if err != nil {
		return apicore.Run{}, err
	}
	defer func() {
		_ = lease.Close()
	}()
	store := convergencestore.New(lease.DB())
	snapshot, err := store.Snapshot(ctx, trimmedRunID)
	if err != nil {
		return apicore.Run{}, err
	}
	if err := authorizeResumeTarget(snapshot, req.ExpectedCursor); err != nil {
		return apicore.Run{}, err
	}
	config, err := m.convergenceContinuationConfig(ctx, sourceRow, snapshot.Config, req)
	if err != nil {
		return apicore.Run{}, err
	}
	newRunID, err := m.buildRunID(&model.RuntimeConfig{})
	if err != nil {
		return apicore.Run{}, fmt.Errorf("allocate convergence continuation run id: %w", err)
	}
	segment, _, err := store.ClaimResume(
		ctx,
		buildResumeRequest(snapshot, req.ExpectedCursor, newRunID),
	)
	if err != nil {
		return apicore.Run{}, err
	}
	return m.createConvergenceContinuation(ctx, sourceRow, snapshot, segment, config, req)
}

func (m *RunManager) createConvergenceContinuation(
	ctx context.Context,
	source globaldb.Run,
	snapshot convergence.Snapshot,
	segment convergence.Segment,
	config convergence.FrozenConfiguration,
	req contract.ConvergenceResumeRequest,
) (apicore.Run, error) {
	indexed, exists, err := m.globalDB.ConvergenceRunIndex(ctx, segment.RunID)
	if err != nil {
		return apicore.Run{}, err
	}
	if exists {
		if indexed.ConvergenceID != snapshot.ConvergenceID ||
			indexed.PreviousRunID != segment.PreviousRunID ||
			indexed.SourceRunID != segment.SourceRunID {
			return apicore.Run{}, fmt.Errorf(
				"%w: continuation index identity does not match canonical segment",
				convergence.ErrIntegrityFailed,
			)
		}
		return m.Get(ctx, segment.RunID)
	}

	presentationMode := convergenceContinuationPresentationMode(req.PresentationMode, source.PresentationMode)
	artifacts := m.runArtifacts(segment.RunID)
	if err := reserveRunDirectory(artifacts.RunDir); err != nil && !errors.Is(err, globaldb.ErrRunAlreadyExists) {
		return apicore.Run{}, err
	}
	if _, err := m.globalDB.GetRun(ctx, segment.RunID); errors.Is(err, globaldb.ErrRunNotFound) {
		createdAt := m.now().UTC()
		endedAt := createdAt
		if _, putErr := m.globalDB.PutRun(ctx, globaldb.Run{
			RunID:            segment.RunID,
			WorkspaceID:      source.WorkspaceID,
			WorkflowID:       source.WorkflowID,
			ParentRunID:      source.RunID,
			Mode:             runModeConvergence,
			Status:           runStatusParked,
			PresentationMode: presentationMode,
			StartedAt:        createdAt,
			EndedAt:          &endedAt,
			RequestID:        apicore.RequestIDFromContext(ctx),
		}); putErr != nil {
			return apicore.Run{}, putErr
		}
	} else if err != nil {
		return apicore.Run{}, err
	}

	continuationLease, err := m.acquireRunDB(ctx, segment.RunID)
	if err != nil {
		return apicore.Run{}, err
	}
	defer func() {
		_ = continuationLease.Close()
	}()
	if err := convergencestore.New(continuationLease.DB()).Seed(
		ctx,
		snapshot.ConvergenceID,
		segment,
		snapshot.Target,
		config,
	); err != nil {
		return apicore.Run{}, err
	}
	if err := convergencestore.NewGlobalIndex(m.globalDB).Index(
		ctx,
		convergence.Snapshot{
			ConvergenceID: snapshot.ConvergenceID,
			Segment:       segment,
			Target:        snapshot.Target,
			Config:        config,
		},
		convergence.ReceiptMetadata{},
	); err != nil {
		return apicore.Run{}, err
	}
	return m.Get(ctx, segment.RunID)
}

func (m *RunManager) convergenceContinuationConfig(
	ctx context.Context,
	sourceRun globaldb.Run,
	sourceConfig convergence.FrozenConfiguration,
	req contract.ConvergenceResumeRequest,
) (convergence.FrozenConfiguration, error) {
	config := cloneConvergenceConfiguration(sourceConfig)
	profile := strings.TrimSpace(req.Profile)
	setup := strings.TrimSpace(req.ModelSetup)
	if profile != "" || setup != "" {
		workspace, err := m.globalDB.Get(ctx, sourceRun.WorkspaceID)
		if err != nil {
			return convergence.FrozenConfiguration{}, err
		}
		projectConfig, err := m.loadProjectConfig(ctx, workspace.RootDir)
		if err != nil {
			return convergence.FrozenConfiguration{}, fmt.Errorf(
				"load convergence continuation configuration: %w",
				err,
			)
		}
		candidate := projectConfig.Convergence
		verification := append([]string(nil), sourceConfig.Verification...)
		candidate.Verification.Command = &verification
		defaultSelection := convergence.DefaultProfileName
		candidate.Profile = &defaultSelection
		candidate.ModelSetup = &defaultSelection
		if profile != "" {
			candidate.Profile = &profile
		}
		if setup != "" {
			candidate.ModelSetup = &setup
		}
		resolved, err := convergence.Freeze(convergence.FreezeInput{
			Workspace:  candidate,
			BaseRoute:  sourceConfig.BaseRoute,
			AutoCommit: sourceConfig.AutoCommit,
		})
		if err != nil {
			return convergence.FrozenConfiguration{}, fmt.Errorf(
				"resolve convergence continuation configuration: %w",
				err,
			)
		}
		if profile != "" {
			config.ProfileName = resolved.ProfileName
			config.Limits = resolved.Limits
			config.LimitSources = resolved.LimitSources
		}
		if setup != "" {
			config.ModelSetupName = resolved.ModelSetupName
			config.Review = cloneConvergenceResolvedRoute(resolved.Review)
			config.Correction = cloneConvergenceCorrectionRoutes(resolved.Correction)
		}
	}

	config.Review = cloneConvergenceResolvedRoute(config.ReviewRoute(
		convergenceRouteOverride(req.ReviewOverride),
	))
	correctionOverride := convergenceRouteOverride(req.CorrectionOverride)
	for _, severity := range []convergence.Severity{
		convergence.SeverityCritical,
		convergence.SeverityHigh,
		convergence.SeverityMedium,
		convergence.SeverityLow,
	} {
		config.Correction[severity] = cloneConvergenceResolvedRoute(
			config.CorrectionRoute(severity, correctionOverride),
		)
	}
	return config, nil
}

func convergenceRouteOverride(
	override *contract.ConvergencePhaseRouteOverride,
) *convergence.RouteConfig {
	if override == nil {
		return nil
	}
	return &convergence.RouteConfig{
		IDE:             nonBlankConvergenceValue(override.IDE),
		Model:           nonBlankConvergenceValue(override.Model),
		ReasoningEffort: nonBlankConvergenceValue(override.ReasoningEffort),
	}
}

func nonBlankConvergenceValue(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cloneConvergenceResolvedRoute(route convergence.ResolvedRoute) convergence.ResolvedRoute {
	cloned := route
	if route.Fallback != nil {
		fallback := *route.Fallback
		cloned.Fallback = &fallback
	}
	return cloned
}

func cloneConvergenceCorrectionRoutes(
	routes map[convergence.Severity]convergence.ResolvedRoute,
) map[convergence.Severity]convergence.ResolvedRoute {
	cloned := make(map[convergence.Severity]convergence.ResolvedRoute, len(routes))
	for severity := range routes {
		cloned[severity] = cloneConvergenceResolvedRoute(routes[severity])
	}
	return cloned
}

func cloneConvergenceConfiguration(
	config convergence.FrozenConfiguration,
) convergence.FrozenConfiguration {
	cloned := config
	cloned.Verification = append([]string(nil), config.Verification...)
	cloned.Warnings = append([]string(nil), config.Warnings...)
	cloned.Review = cloneConvergenceResolvedRoute(config.Review)
	cloned.Correction = cloneConvergenceCorrectionRoutes(config.Correction)
	return cloned
}

func (m *RunManager) registeredConvergenceRun(ctx context.Context, runID string) (globaldb.Run, error) {
	trimmed := strings.TrimSpace(runID)
	row, err := m.globalDB.GetRun(ctx, trimmed)
	if err != nil {
		return globaldb.Run{}, err
	}
	if strings.TrimSpace(row.Mode) != runModeConvergence {
		return globaldb.Run{}, fmt.Errorf("%w: run %q is not a convergence run", globaldb.ErrRunNotFound, trimmed)
	}
	return row, nil
}

func convergenceContinuationPresentationMode(requested, source string) string {
	if mode := strings.TrimSpace(requested); mode != "" {
		return mode
	}
	if source != "" {
		return source
	}
	return defaultPresentationMode
}

// convergenceContinuationRelations returns the resumed-continuation segments that
// link back to this run within the same convergence identity. It is best-effort:
// a global index read failure yields no children rather than failing the snapshot,
// because the segment lineage in the snapshot itself is authoritative.
func (m *RunManager) convergenceContinuationRelations(
	ctx context.Context,
	snapshot convergence.Snapshot,
) []contract.ConvergenceRelation {
	convergenceID := strings.TrimSpace(snapshot.ConvergenceID)
	if convergenceID == "" || m.globalDB == nil {
		return nil
	}
	rows, err := m.globalDB.ConvergenceRunIndexByConvergenceID(ctx, convergenceID)
	if err != nil {
		return nil
	}
	var children []contract.ConvergenceRelation
	for i := range rows {
		if rows[i].RunID == snapshot.Segment.RunID {
			continue
		}
		if rows[i].PreviousRunID == snapshot.Segment.RunID {
			children = append(children, contract.ConvergenceRelation{
				Kind:  contract.ConvergenceRelationContinuation,
				RunID: rows[i].RunID,
			})
		}
	}
	return children
}
