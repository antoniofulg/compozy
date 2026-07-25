package convergence

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recordingCanonical struct {
	snapshot Snapshot
	err      error
	calls    int
	order    *[]string
}

func (f *recordingCanonical) Commit(context.Context) (Snapshot, error) {
	f.calls++
	*f.order = append(*f.order, "canonical")
	return f.snapshot, f.err
}

type recordingReceipt struct {
	metadata ReceiptMetadata
	err      error
	calls    int
	order    *[]string
}

func (f *recordingReceipt) Rebuild(context.Context, Snapshot) (ReceiptMetadata, error) {
	f.calls++
	*f.order = append(*f.order, "receipt")
	return f.metadata, f.err
}

type recordingGlobal struct {
	err   error
	calls int
	order *[]string
}

func (f *recordingGlobal) Index(context.Context, Snapshot, ReceiptMetadata) error {
	f.calls++
	*f.order = append(*f.order, "global")
	return f.err
}

type recordingPublisher struct {
	err   error
	calls int
	order *[]string
}

func (f *recordingPublisher) Publish(context.Context, Snapshot) error {
	f.calls++
	*f.order = append(*f.order, "publish")
	return f.err
}

func TestCommitTransitionFailureBoundaries(t *testing.T) {
	// IT-005..IT-009: durable stages remain ordered, canonical failure exposes
	// nothing, projection failures are rebuildable, and a live sink failure never
	// stops execution.
	t.Parallel()

	t.Run("IT-005 canonical failure starts no downstream stage", func(t *testing.T) {
		t.Parallel()
		order := []string{}
		canonical := &recordingCanonical{err: errors.New("commit failed"), order: &order}
		receipt := &recordingReceipt{order: &order}
		global := &recordingGlobal{order: &order}
		publisher := &recordingPublisher{order: &order}

		result, err := CommitTransition(context.Background(), CommitDeps{
			Canonical: canonical,
			Receipt:   receipt,
			Global:    global,
			Publisher: publisher,
		})
		if err == nil {
			t.Fatal("CommitTransition() error = nil")
		}
		if result.Stage != StageNone || receipt.calls != 0 || global.calls != 0 || publisher.calls != 0 {
			t.Fatalf("result=%+v downstream=%d/%d/%d", result, receipt.calls, global.calls, publisher.calls)
		}
	})

	t.Run("IT-006 receipt failure keeps only canonical state authoritative", func(t *testing.T) {
		t.Parallel()
		order := []string{}
		canonical := &recordingCanonical{snapshot: Snapshot{LastSeq: 7}, order: &order}
		receipt := &recordingReceipt{err: errors.New("disk full"), order: &order}
		global := &recordingGlobal{order: &order}
		publisher := &recordingPublisher{order: &order}
		result, err := CommitTransition(context.Background(), CommitDeps{
			Canonical: canonical,
			Receipt:   receipt,
			Global:    global,
			Publisher: publisher,
		})
		if err == nil || result.Stage != StageCanonical || !result.ProjectionIncomplete {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if want := []string{"canonical", "receipt"}; !reflect.DeepEqual(order, want) {
			t.Fatalf("order=%v want=%v", order, want)
		}
		receipt.err = nil
		if _, err := RebuildProjections(context.Background(), CommitDeps{
			Receipt: receipt,
			Global:  global,
		}, result.Snapshot); err != nil {
			t.Fatalf("RebuildProjections() = %v", err)
		}
		if canonical.calls != 1 || receipt.calls != 2 || global.calls != 1 || publisher.calls != 0 {
			t.Fatalf(
				"calls canonical/receipt/global/publisher = %d/%d/%d/%d",
				canonical.calls,
				receipt.calls,
				global.calls,
				publisher.calls,
			)
		}
	})

	t.Run("IT-007 publication loss replays without a duplicate transition", func(t *testing.T) {
		t.Parallel()
		order := []string{}
		canonical := &recordingCanonical{snapshot: Snapshot{LastSeq: 8}, order: &order}
		publisher := &recordingPublisher{err: errors.New("subscriber gone"), order: &order}
		result, err := CommitTransition(context.Background(), CommitDeps{
			Canonical: canonical,
			Receipt: &recordingReceipt{
				metadata: ReceiptMetadata{SourceSeq: 8, Checksum: "sum"},
				order:    &order,
			},
			Global:    &recordingGlobal{order: &order},
			Publisher: publisher,
		})
		if err != nil {
			t.Fatalf("CommitTransition() error = %v", err)
		}
		if result.Stage != StageProjected || !result.PublicationIncomplete || result.PublicationError == nil {
			t.Fatalf("result=%+v", result)
		}
		if want := []string{"canonical", "receipt", "global", "publish"}; !reflect.DeepEqual(order, want) {
			t.Fatalf("order=%v want=%v", order, want)
		}
		publisher.err = nil
		if err := publisher.Publish(context.Background(), result.Snapshot); err != nil {
			t.Fatalf("Publish(replay) = %v", err)
		}
		if canonical.calls != 1 || publisher.calls != 2 {
			t.Fatalf("canonical calls=%d publisher calls=%d", canonical.calls, publisher.calls)
		}
	})

	t.Run("IT-009 reconnect cursor deduplicates repeated live delivery", func(t *testing.T) {
		t.Parallel()
		order := []string{}
		publisher := &recordingPublisher{err: errors.New("all sinks disconnected"), order: &order}
		result, err := CommitTransition(context.Background(), CommitDeps{
			Canonical: &recordingCanonical{snapshot: Snapshot{LastSeq: 12}, order: &order},
			Receipt:   &recordingReceipt{order: &order},
			Publisher: publisher,
		})
		if err != nil || !result.PublicationIncomplete {
			t.Fatalf("CommitTransition() = %+v, %v", result, err)
		}
		seen := make(map[uint64]struct{})
		delivered := 0
		for _, replayed := range []Snapshot{result.Snapshot, result.Snapshot} {
			if _, duplicate := seen[replayed.LastSeq]; duplicate {
				continue
			}
			seen[replayed.LastSeq] = struct{}{}
			delivered++
		}
		if delivered != 1 {
			t.Fatalf("deduplicated replay deliveries = %d, want 1", delivered)
		}
	})
}

func TestRebuildProjectionsIsIdempotentAndPublishesNothing(t *testing.T) {
	// IT-007 and IT-032: restart rebuild reads canonical state, repeats only
	// reconstructible projections, and leaves live replay to the canonical stream.
	t.Parallel()

	order := []string{}
	receipt := &recordingReceipt{
		metadata: ReceiptMetadata{RelativePath: ReceiptFileName, SourceSeq: 9, Checksum: "sum"},
		order:    &order,
	}
	global := &recordingGlobal{order: &order}
	publisher := &recordingPublisher{order: &order}
	deps := CommitDeps{Receipt: receipt, Global: global, Publisher: publisher}

	first, err := RebuildProjections(context.Background(), deps, Snapshot{LastSeq: 9})
	if err != nil {
		t.Fatalf("RebuildProjections(first) = %v", err)
	}
	second, err := RebuildProjections(context.Background(), deps, Snapshot{LastSeq: 9})
	if err != nil {
		t.Fatalf("RebuildProjections(second) = %v", err)
	}
	if first != second || publisher.calls != 0 {
		t.Fatalf("first=%+v second=%+v publisher calls=%d", first, second, publisher.calls)
	}
	if want := []string{"receipt", "global", "receipt", "global"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
}
