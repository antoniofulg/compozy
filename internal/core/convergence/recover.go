package convergence

import "context"

// CorruptRecovery is the safe recovery action for a stale, missing, or corrupt
// projection. It never permits a repeated model, process, Git, or artifact side
// effect: a trusted canonical history rebuilds its projection deterministically,
// while an untrusted one settles to a failed terminal without executing anything.
type CorruptRecovery struct {
	// Rebuild is true when the canonical snapshot is trustworthy and the projection
	// must be reconstructed from it. Outcome is nil in this case.
	Rebuild bool
	// Outcome is the terminal result for an untrusted canonical history. It is set
	// only when Rebuild is false and is always a failed terminal.
	Outcome *TerminalOutcome
}

// ClassifyProjectionCorruption decides recovery from the result of reading the
// canonical snapshot. It implements the techspec recovery rule for corrupt or
// insufficient evidence: when the canonical snapshot is readable (canonicalErr is
// nil) the projection is rebuilt from it; when it is unreadable the segment is a
// trusted failure and no side effect repeats. This is the pure classifier behind
// the receipt/global-projection rebuild-or-fail decision after a restart.
func ClassifyProjectionCorruption(canonicalErr error) CorruptRecovery {
	if canonicalErr == nil {
		return CorruptRecovery{Rebuild: true}
	}
	failed := TerminalOutcome{Kind: TerminalFailed}
	return CorruptRecovery{Outcome: &failed}
}

// RecoverCorruptProjections applies the corruption classification without ever
// scheduling execution work. Trusted canonical state rebuilds only receipt and
// global projections; untrusted canonical state returns a failed terminal and
// touches no projection or live sink.
func RecoverCorruptProjections(
	ctx context.Context,
	deps CommitDeps,
	snap Snapshot,
	canonicalErr error,
) (ReceiptMetadata, *TerminalOutcome, error) {
	recovery := ClassifyProjectionCorruption(canonicalErr)
	if !recovery.Rebuild {
		return ReceiptMetadata{}, recovery.Outcome, nil
	}
	receipt, err := RebuildProjections(ctx, deps, snap)
	if err != nil {
		return ReceiptMetadata{}, nil, err
	}
	return receipt, nil, nil
}
