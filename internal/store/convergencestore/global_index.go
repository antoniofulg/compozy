package convergencestore

import (
	"context"

	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/store/globaldb"
)

// GlobalIndex projects a canonical convergence snapshot into the global.db index.
// global.db is a rebuildable projection, never a second authority: this adapter
// reconstructs the cross-run relations, terminal outcome, and receipt index from
// the snapshot and the receipt metadata, and leaves the activation and resume
// request fingerprints to the upsert's fingerprint-preserving merge so a rebuild
// from canonical state never erases them.
type GlobalIndex struct {
	db *globaldb.GlobalDB
}

var _ convergence.GlobalIndex = (*GlobalIndex)(nil)

// NewGlobalIndex wraps a global.db handle as a convergence global index.
func NewGlobalIndex(db *globaldb.GlobalDB) *GlobalIndex {
	return &GlobalIndex{db: db}
}

// Index upserts the reconstructible global projection for one convergence segment.
// It is idempotent: indexing identical canonical state yields an identical row.
func (g *GlobalIndex) Index(
	ctx context.Context,
	snap convergence.Snapshot,
	receipt convergence.ReceiptMetadata,
) error {
	row := globaldb.ConvergenceRunIndexRow{
		RunID:            snap.Segment.RunID,
		ConvergenceID:    snap.ConvergenceID,
		PreviousRunID:    snap.Segment.PreviousRunID,
		SourceRunID:      snap.Segment.SourceRunID,
		ReceiptPath:      receipt.RelativePath,
		ReceiptSourceSeq: receipt.SourceSeq,
	}
	if snap.Terminal != nil {
		row.TerminalOutcome = string(snap.Terminal.Kind)
		if snap.Terminal.Kind == convergence.TerminalParked {
			row.TerminalReason = string(snap.Terminal.Reason)
		}
	}
	return g.db.UpsertConvergenceRunIndex(ctx, row)
}
