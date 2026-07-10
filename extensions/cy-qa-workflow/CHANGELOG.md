# Changelog — cy-qa-workflow

All notable changes to this extension. Dates are release-independent; entries are
grouped by the extension `version` in `extension.toml`.

## 0.2.0

### Fixed

- **Ship a standalone Go module.** The extension now includes its own
  `go.mod`/`go.sum`, so the `go run .` subprocess resolves the SDK when the
  extension is installed on its own. Before, the installed package contained only
  `extension.toml` + `main.go`, and the subprocess failed to start with
  `go: go.mod file not found`, aborting the run before `extension.ready`.

### Added

- **Configurable QA runtime.** The IDE, model, and reasoning effort of the two
  injected QA tasks can be overridden with environment variables. The previous
  hardcoded values remain the defaults, so existing installs are unaffected.
  This makes it possible to run the QA execution on `claude` instead of `codex`
  (e.g. when codex is unavailable). See **Configuration** in `README.md`.

### Related core change (compozy daemon — same fix, different file)

- **`host.tasks.create` is now `compozy.tasks/v2`-aware.** The daemon previously
  understood only the legacy Markdown-table `_tasks.md` and searched for the
  header `| # | Title | Status | Complexity | Dependencies |`. On a v2
  frontmatter-graph manifest it failed with **"tasks table header not found"**,
  which aborted every `compozy tasks run` while this extension was enabled
  (the extension's `plan.pre_discover` hook calls `host.tasks.create`). It now
  detects the v2 manifest and injects the new task into `graph.nodes` /
  `graph.edges`, writing the task file without a `dependencies` key. This change
  lives in the compozy core (`internal/core/extension/host_writes.go` and
  `internal/core/tasks`), not in this extension, but is part of the same fix.

## 0.1.0

- Initial release. Injects a QA report task and a QA execution task into
  `compozy.tasks/v2` PRD task runs via `plan.pre_discover`, and assigns their
  runtimes via `plan.pre_resolve_task_runtime`.
