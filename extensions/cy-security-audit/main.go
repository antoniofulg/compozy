// Command cy-security-audit is the subprocess for the cy-security-audit
// extension. Its only job is a non-blocking reminder: after a successful
// PRD task run — the moment new implementation code exists — it prints a
// recommendation to run the /cy-security-audit skill. The audit itself is
// performed by that skill, on demand; this process never mutates a run.
package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	extension "github.com/compozy/compozy/sdk/extension"
)

const (
	extensionName    = "cy-security-audit"
	extensionVersion = "0.1.0"

	// runStatusSucceeded mirrors the host's runshared.RunStatusSucceeded. The
	// extension is a standalone module and cannot import internal packages, so
	// this string tracks the observable run-summary contract.
	runStatusSucceeded = "succeeded"

	nudgeMessage = "✅ tasks complete — run `/cy-security-audit` to verify the changes against the AppSec " +
		"ruleset, then `/cy-fix-reviews` to remediate any findings."
)

// runModeTracker remembers the execution mode of in-flight runs. run.post_start
// carries the mode; run.post_shutdown carries only the status, so the mode must
// be captured at start and consumed at shutdown. It is safe whether the
// subprocess handles one run or many.
type runModeTracker struct {
	mu    sync.Mutex
	modes map[string]extension.ExecutionMode
}

func newRunModeTracker() *runModeTracker {
	return &runModeTracker{modes: make(map[string]extension.ExecutionMode)}
}

func (t *runModeTracker) record(runID string, mode extension.ExecutionMode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modes[runID] = mode
}

// take returns the recorded mode for runID and forgets it, so a run's state is
// released once its shutdown has been observed.
func (t *runModeTracker) take(runID string) (extension.ExecutionMode, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	mode, ok := t.modes[runID]
	delete(t.modes, runID)
	return mode, ok
}

// shouldNudge reports whether a completed run warrants the security-audit
// reminder: only a PRD task run that finished successfully, since that is when
// new implementation code exists to audit.
func shouldNudge(mode extension.ExecutionMode, status string) bool {
	return mode == extension.ExecutionModePRDTasks && status == runStatusSucceeded
}

func main() {
	tracker := newRunModeTracker()
	ext := extension.New(extensionName, extensionVersion).
		WithCapabilities(extension.CapabilityRunMutate).
		OnRunPostStart(func(_ context.Context, _ extension.HookContext, payload extension.RunPostStartPayload) error {
			tracker.record(payload.RunID, payload.Config.Mode)
			return nil
		}).
		OnRunPostShutdown(
			func(_ context.Context, _ extension.HookContext, payload extension.RunPostShutdownPayload) error {
				if mode, ok := tracker.take(payload.RunID); ok && shouldNudge(mode, payload.Summary.Status) {
					fmt.Fprintln(os.Stderr, nudgeMessage)
				}
				return nil
			},
		)

	if err := ext.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
