package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/compozy/compozy/internal/api/contract"
	apicore "github.com/compozy/compozy/internal/api/core"
	"github.com/compozy/compozy/internal/core/convergence"
	"github.com/compozy/compozy/internal/store/convergencestore"
	"github.com/compozy/compozy/internal/store/globaldb"
	"github.com/compozy/compozy/internal/store/rundb"
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
