package convergence

import (
	"context"
	"fmt"
)

// CommitStage records how far a durable transition progressed. It lets a caller
// recover deterministically after a partial write without treating a projection
// or a live sink as an alternate authority.
type CommitStage string

const (
	// StageNone means the canonical transaction did not commit; nothing is
	// observable and no side effect started.
	StageNone CommitStage = "none"
	// StageCanonical means the canonical event and its run.db projection committed,
	// but the receipt or global projection did not. Canonical state wins; the
	// projections must be rebuilt before the next transition.
	StageCanonical CommitStage = "canonical"
	// StageProjected means canonical, receipt, and global projections committed but
	// live publication did not. Replay from the canonical sequence supplies the
	// event; no transition repeats.
	StageProjected CommitStage = "projected"
	// StagePublished means the transition is fully durable and published.
	StagePublished CommitStage = "published"
)

// CanonicalCommit appends one canonical convergence event and all affected run.db
// projection rows in a single transaction, before any observer can see the
// transition. It returns the projected snapshot the receipt and global index read
// from. A failed commit rolls back the event and every projection together.
type CanonicalCommit interface {
	Commit(ctx context.Context) (Snapshot, error)
}

// ReceiptSink rebuilds the atomic receipt projection from canonical state.
type ReceiptSink interface {
	Rebuild(ctx context.Context, snap Snapshot) (ReceiptMetadata, error)
}

// GlobalIndex reconstructs the global convergence index from canonical state.
type GlobalIndex interface {
	Index(ctx context.Context, snap Snapshot, receipt ReceiptMetadata) error
}

// LivePublisher fans the committed transition out to live observers. Its failure
// never rolls back durable state; reconnect and replay supply the event.
type LivePublisher interface {
	Publish(ctx context.Context, snap Snapshot) error
}

// CommitDeps carries the ordered durable-write dependencies. Receipt is required;
// Global and Publisher are optional so a run without a global index or live
// observer still commits canonically.
type CommitDeps struct {
	Canonical CanonicalCommit
	Receipt   ReceiptSink
	Global    GlobalIndex
	Publisher LivePublisher
}

// CommitResult reports how far the transition progressed and the projected state.
type CommitResult struct {
	Snapshot             Snapshot
	Receipt              ReceiptMetadata
	Stage                CommitStage
	ProjectionIncomplete bool
	// PublicationIncomplete reports a failed best-effort live delivery. Durable
	// execution still succeeds and reconnect/replay supplies the committed event.
	PublicationIncomplete bool
	// PublicationError retains bounded diagnostic context for observability without
	// turning a non-authoritative live sink into an execution failure.
	PublicationError error
}

// CommitTransition performs the canonical-first durable write in the exact order
// the reliability contract requires: transactional run.db append/projection, then
// receipt and global projection, then live publication. It never treats a later
// stage as authoritative. A canonical failure exposes nothing (failure point A /
// commit). A receipt or global failure keeps canonical state authoritative and
// flags the projection for rebuild (failure point B). A publication failure leaves
// durable state intact for replay (failure point C).
func CommitTransition(ctx context.Context, deps CommitDeps) (CommitResult, error) {
	if deps.Canonical == nil {
		return CommitResult{Stage: StageNone}, fmt.Errorf("%w: canonical commit is required", ErrTransitionInvalid)
	}
	if deps.Receipt == nil {
		return CommitResult{Stage: StageNone}, fmt.Errorf("%w: receipt sink is required", ErrTransitionInvalid)
	}
	snap, err := deps.Canonical.Commit(ctx)
	if err != nil {
		return CommitResult{Stage: StageNone}, fmt.Errorf("canonical commit: %w", err)
	}
	result := CommitResult{Snapshot: snap, Stage: StageCanonical}

	receipt, err := deps.Receipt.Rebuild(ctx, snap)
	if err != nil {
		result.ProjectionIncomplete = true
		return result, fmt.Errorf("receipt projection: %w", err)
	}
	result.Receipt = receipt

	if deps.Global != nil {
		if err := deps.Global.Index(ctx, snap, receipt); err != nil {
			result.ProjectionIncomplete = true
			return result, fmt.Errorf("global projection: %w", err)
		}
	}
	result.Stage = StageProjected

	if deps.Publisher != nil {
		if err := deps.Publisher.Publish(ctx, snap); err != nil {
			result.PublicationIncomplete = true
			result.PublicationError = fmt.Errorf("live publication: %w", err)
			return result, nil
		}
	}
	result.Stage = StagePublished
	return result, nil
}

// RebuildProjections reconstructs the receipt and global projections from an
// already-committed canonical snapshot, in the same order CommitTransition uses.
// It is the failure-point-B recovery primitive: after a canonical commit whose
// receipt or global projection did not complete, a restart replays the snapshot
// through this helper to make both projections consistent before the next
// transition. It is idempotent — rebuilding from identical canonical state yields
// identical projections — and it never repeats a model, process, Git, or artifact
// side effect. Global is optional; Publisher is intentionally not invoked here
// because publication is replayed from the canonical journal, not rebuilt.
func RebuildProjections(ctx context.Context, deps CommitDeps, snap Snapshot) (ReceiptMetadata, error) {
	if deps.Receipt == nil {
		return ReceiptMetadata{}, fmt.Errorf("%w: receipt sink is required", ErrTransitionInvalid)
	}
	receipt, err := deps.Receipt.Rebuild(ctx, snap)
	if err != nil {
		return ReceiptMetadata{}, fmt.Errorf("receipt projection rebuild: %w", err)
	}
	if deps.Global != nil {
		if err := deps.Global.Index(ctx, snap, receipt); err != nil {
			return receipt, fmt.Errorf("global projection rebuild: %w", err)
		}
	}
	return receipt, nil
}
