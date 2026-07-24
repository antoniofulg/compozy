package convergence

import (
	"errors"
	"testing"
)

func TestSequenceGuard(t *testing.T) {
	t.Parallel()
	t.Run("Should admit strictly increasing sequences", func(t *testing.T) {
		t.Parallel()
		guard := NewSequenceGuard()
		for _, seq := range []uint64{1, 2, 3} {
			if err := guard.Admit(seq); err != nil {
				t.Fatalf("expected admit %d, got %v", seq, err)
			}
		}
	})
	t.Run("Should reject a duplicate sequence", func(t *testing.T) {
		t.Parallel()
		guard := NewSequenceGuard()
		_ = guard.Admit(1)
		if err := guard.Admit(1); !errors.Is(err, ErrReplayConflict) {
			t.Fatalf("expected duplicate rejection, got %v", err)
		}
	})
	t.Run("Should reject an out-of-order sequence", func(t *testing.T) {
		t.Parallel()
		guard := NewSequenceGuard()
		_ = guard.Admit(5)
		if err := guard.Admit(3); !errors.Is(err, ErrReplayConflict) {
			t.Fatalf("expected out-of-order rejection, got %v", err)
		}
	})
}

func TestBatchGuardSequential(t *testing.T) {
	t.Parallel()
	t.Run("Should reject a concurrent batch in one worktree", func(t *testing.T) {
		t.Parallel()
		guard := NewBatchGuard()
		if err := guard.Start("batch-1"); err != nil {
			t.Fatalf("start batch-1: %v", err)
		}
		if err := guard.Start("batch-2"); !errors.Is(err, ErrReplayConflict) {
			t.Fatalf("expected concurrent batch rejection, got %v", err)
		}
	})
	t.Run("Should allow sequential batches and reject restarting a completed one", func(t *testing.T) {
		t.Parallel()
		guard := NewBatchGuard()
		mustBatch(t, guard.Start("batch-1"))
		mustBatch(t, guard.Complete("batch-1"))
		mustBatch(t, guard.Start("batch-2"))
		if err := guard.Start("batch-1"); !errors.Is(err, ErrReplayConflict) {
			t.Fatalf("expected completed batch to not restart, got %v", err)
		}
	})
	t.Run("Should reject completing a non-active batch", func(t *testing.T) {
		t.Parallel()
		guard := NewBatchGuard()
		mustBatch(t, guard.Start("batch-1"))
		if err := guard.Complete("batch-2"); !errors.Is(err, ErrReplayConflict) {
			t.Fatalf("expected non-active completion rejection, got %v", err)
		}
	})
}

func TestAdmitRetryUnknownWork(t *testing.T) {
	t.Parallel()
	t.Run("Should refuse a free retry of unknown work", func(t *testing.T) {
		t.Parallel()
		if err := AdmitRetry(RecoveryUnknown); !errors.Is(err, ErrReplayConflict) {
			t.Fatalf("expected unknown work to be refused, got %v", err)
		}
	})
	t.Run("Should permit retry for classified recoverable work", func(t *testing.T) {
		t.Parallel()
		for _, class := range []RecoveryClass{RecoveryStartable, RecoveryRetryable, RecoveryReplayable} {
			if err := AdmitRetry(class); err != nil {
				t.Fatalf("expected %s to be admissible, got %v", class, err)
			}
		}
	})
}

func mustBatch(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("batch guard: %v", err)
	}
}
