// Package verification runs the authoritative repository verification command
// for a convergence run and captures trustworthy evidence of its outcome.
//
// Verification is a repository-defined quality gate, never inferred and never
// accepted from model prose. The configured `[convergence.verification].command`
// argv is executed directly in the target worktree root with context-owned
// process cancellation, an inherited safe environment, and no shell — shell
// operators arrive as literal arguments and cannot expand. It never reuses the
// parallel conflict resolver's validation command, which serves an unrelated
// merge-validation purpose.
//
// Each run yields a Result recording the command fingerprint and redacted argv,
// the start and end Git snapshots, timestamps, completion state, exit code,
// bounded redacted stdout/stderr summaries, checksummed paths to the complete
// raw output, and a stable failure fingerprint. Authorize enforces the
// progression rule: only a completed exit-zero result bound to the unchanged
// expected snapshot may authorize another phase, and that same result may be
// replayed after a transport loss. Failed, canceled, timed-out, partial,
// missing-exit, and stale results never imply a pass.
package verification
