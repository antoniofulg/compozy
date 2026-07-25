package convergence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// crashFS wraps OSReceiptFS and injects a failure at one named step of the atomic
// replacement so a test can prove no step leaves a partial destination file. It
// delegates every non-failing step to the real os-backed implementation, so the
// on-disk invariant (readers see prior-complete or new-complete, never partial
// JSON) is exercised against real files.
type crashFS struct {
	OSReceiptFS
	failAt string // "", "writetemp", "rename", "syncdir"
}

func (f crashFS) WriteTemp(dir, pattern string, data []byte) (string, error) {
	if f.failAt == "writetemp" {
		return "", errors.New("injected temp-write failure")
	}
	return f.OSReceiptFS.WriteTemp(dir, pattern, data)
}

func (f crashFS) Rename(oldpath, newpath string) error {
	if f.failAt == "rename" {
		return errors.New("injected rename failure")
	}
	return f.OSReceiptFS.Rename(oldpath, newpath)
}

func (f crashFS) SyncDir(dir string) error {
	if f.failAt == "syncdir" {
		return errors.New("injected dir-sync failure")
	}
	return f.OSReceiptFS.SyncDir(dir)
}

func TestReceiptWriterAtomicReplacementIsCrashSafe(t *testing.T) {
	// IT-004: write a receipt through temp-file, sync, rename, and directory sync;
	// assert readers see either the prior complete receipt or the new complete
	// receipt, never partial JSON.
	t.Parallel()

	ctx := context.Background()
	prior := fixtureSnapshot()
	next := fixtureSnapshot()
	next.Findings[0].State = FindingResolved
	next.Terminal = &TerminalOutcome{Kind: TerminalClean}
	next.LastSeq = prior.LastSeq + 8

	t.Run("happy: a complete receipt is written and reads back valid", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writer := NewReceiptWriter(dir, WithReceiptClock(fixtureClock))
		if _, err := writer.Rebuild(ctx, prior); err != nil {
			t.Fatalf("Rebuild(prior): %v", err)
		}
		assertCompleteReceipt(t, dir, prior.LastSeq)
	})

	failSteps := []string{"writetemp", "rename", "syncdir"}
	for _, step := range failSteps {
		t.Run("crash before commit at "+step+" preserves the prior receipt", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			// Establish a prior complete receipt with the real FS.
			base := NewReceiptWriter(dir, WithReceiptClock(fixtureClock))
			if _, err := base.Rebuild(ctx, prior); err != nil {
				t.Fatalf("Rebuild(prior): %v", err)
			}
			// Attempt a second write that fails partway through.
			crashing := NewReceiptWriter(dir,
				WithReceiptClock(fixtureClock),
				WithReceiptFS(crashFS{failAt: step}),
			)
			if _, err := crashing.Rebuild(ctx, next); err == nil {
				t.Fatalf("Rebuild(next) with %s failure returned no error", step)
			}
			if step == "syncdir" {
				// syncdir fails after a successful atomic rename, so the destination is
				// the new complete receipt; only durability of the directory entry
				// failed. The reader still sees a complete document, never partial JSON.
				assertCompleteReceipt(t, dir, next.LastSeq)
				return
			}
			// Temp-write and rename fail before the destination is replaced, so the
			// reader still sees the prior complete receipt and no partial temp leaks.
			assertCompleteReceipt(t, dir, prior.LastSeq)
			assertNoTempFiles(t, dir)
		})
	}
}

func TestCorruptReceiptRebuildsFromTrustedCanonicalState(t *testing.T) {
	// IT-034: projection corruption is detected, rebuilt from the trusted
	// canonical snapshot, and never changes the canonical source sequence.
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	snapshot := fixtureSnapshot()
	writer := NewReceiptWriter(dir, WithReceiptClock(fixtureClock))
	metadata, err := writer.Rebuild(ctx, snapshot)
	if err != nil {
		t.Fatalf("Rebuild(initial) = %v", err)
	}
	path := ReceiptPath(dir)
	if err := os.WriteFile(path, []byte(`{"schema_version":"tampered"}`), 0o600); err != nil {
		t.Fatalf("tamper receipt = %v", err)
	}
	if err := writer.Validate(ctx, snapshot); !errors.Is(err, ErrReceiptCorrupt) {
		t.Fatalf("Validate(tampered) = %v, want ErrReceiptCorrupt", err)
	}
	recovery := ClassifyProjectionCorruption(nil)
	if !recovery.Rebuild || recovery.Outcome != nil {
		t.Fatalf("trusted recovery = %+v", recovery)
	}
	rebuilt, err := writer.Rebuild(ctx, snapshot)
	if err != nil {
		t.Fatalf("Rebuild(recovery) = %v", err)
	}
	if rebuilt.SourceSeq != metadata.SourceSeq || rebuilt.Checksum != metadata.Checksum {
		t.Fatalf("rebuilt metadata = %+v, want %+v", rebuilt, metadata)
	}
	if err := writer.Validate(ctx, snapshot); err != nil {
		t.Fatalf("Validate(rebuilt) = %v", err)
	}
}

func assertCompleteReceipt(t *testing.T, dir string, wantSeq uint64) Receipt {
	t.Helper()
	data, err := os.ReadFile(ReceiptPath(dir))
	if err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	receipt, err := ParseReceipt(data)
	if err != nil {
		t.Fatalf("on-disk receipt is not a complete, valid document: %v\nbytes: %s", err, data)
	}
	if receipt.SourceSeq != wantSeq {
		t.Fatalf("on-disk receipt source seq = %d, want %d", receipt.SourceSeq, wantSeq)
	}
	return receipt
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ReceiptFileName {
			continue
		}
		if filepath.Ext(name) == ".json" || name != "" && name[0] == '.' {
			t.Fatalf("leftover temp file after failed write: %q", name)
		}
	}
}
