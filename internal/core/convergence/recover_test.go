package convergence

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyProjectionCorruption(t *testing.T) {
	// Pure recovery classifier behind IT-034: a readable (trusted) canonical
	// snapshot rebuilds its projection; an unreadable (untrusted) one settles to a
	// failed terminal and never repeats a side effect.
	t.Parallel()

	t.Run("trusted canonical state rebuilds the projection", func(t *testing.T) {
		t.Parallel()
		got := ClassifyProjectionCorruption(nil)
		if !got.Rebuild {
			t.Fatal("trusted canonical state must rebuild")
		}
		if got.Outcome != nil {
			t.Fatalf("trusted canonical state must not set a terminal outcome: %+v", got.Outcome)
		}
	})

	t.Run("untrusted canonical state is a failed terminal without a side effect", func(t *testing.T) {
		t.Parallel()
		got := ClassifyProjectionCorruption(errors.New("run.db unreadable"))
		if got.Rebuild {
			t.Fatal("untrusted canonical state must not rebuild")
		}
		if got.Outcome == nil || got.Outcome.Kind != TerminalFailed {
			t.Fatalf("untrusted canonical state must fail, got %+v", got.Outcome)
		}
	})
}

func TestRecoverCorruptProjectionsNeverRepeatsExecution(t *testing.T) {
	// IT-034 untrusted halves: unreadable run.db, or unreadable run.db plus JSONL,
	// select a validated failed terminal without rebuilding projections,
	// publishing, or re-running any canonical transition.
	t.Parallel()

	for _, name := range []string{"run.db corrupt", "run.db and JSONL corrupt"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			order := []string{}
			receipt := &recordingReceipt{order: &order}
			global := &recordingGlobal{order: &order}
			publisher := &recordingPublisher{order: &order}
			metadata, terminal, err := RecoverCorruptProjections(
				context.Background(),
				CommitDeps{Receipt: receipt, Global: global, Publisher: publisher},
				Snapshot{LastSeq: 42},
				errors.New(name),
			)
			if err != nil {
				t.Fatalf("RecoverCorruptProjections() = %v", err)
			}
			if metadata != (ReceiptMetadata{}) || terminal == nil || terminal.Kind != TerminalFailed {
				t.Fatalf("metadata=%+v terminal=%+v", metadata, terminal)
			}
			if receipt.calls != 0 || global.calls != 0 || publisher.calls != 0 || len(order) != 0 {
				t.Fatalf(
					"untrusted recovery performed side effects: receipt/global/publisher=%d/%d/%d order=%v",
					receipt.calls,
					global.calls,
					publisher.calls,
					order,
				)
			}
		})
	}
}
