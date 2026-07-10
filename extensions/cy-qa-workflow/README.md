# cy-qa-workflow

A Compozy extension that automatically injects two QA tasks into a
`compozy.tasks/v2` PRD task run:

1. **QA report** — plans QA artifacts (test plan, cases, regression suites) via
   the `/qa-report` skill.
2. **QA execution** — runs the QA plan against the repository via the
   `/qa-execution` skill.

It hooks `plan.pre_discover` (to create the tasks through `host.tasks.create`)
and `plan.pre_resolve_task_runtime` (to assign each QA task its runtime).

This is a standalone Go module (`go run .`); it ships its own `go.mod`/`go.sum`
so it resolves the SDK when installed on its own.

## Install

```bash
compozy ext install <path-or-github-ref> --yes
compozy ext enable cy-qa-workflow
```

## Configuration

By default the injected QA tasks run on:

| Task         | IDE      | Model         | Reasoning effort |
| ------------ | -------- | ------------- | ---------------- |
| QA report    | `claude` | `opus`        | `xhigh`          |
| QA execution | `codex`  | `gpt-5.6-sol` | `xhigh`          |

Any of these can be overridden with environment variables. The defaults apply
when a variable is unset or blank, so existing installs are unaffected.

| Environment variable               | Default       | Controls                                   |
| ---------------------------------- | ------------- | ------------------------------------------ |
| `CY_QA_REPORT_IDE`                 | `claude`      | IDE for the QA report task                 |
| `CY_QA_REPORT_MODEL`               | `opus`        | Model for the QA report task               |
| `CY_QA_REPORT_REASONING_EFFORT`    | `xhigh`       | Reasoning effort for the QA report task    |
| `CY_QA_EXECUTION_IDE`              | `codex`       | IDE for the QA execution task              |
| `CY_QA_EXECUTION_MODEL`            | `gpt-5.6-sol` | Model for the QA execution task            |
| `CY_QA_EXECUTION_REASONING_EFFORT` | `xhigh`       | Reasoning effort for the QA execution task |

**Model behavior when switching IDE:** if you override an IDE but do not set its
model, the model is left empty so the daemon picks the chosen IDE's default
model. The compiled-in default model only applies while the IDE is unchanged —
this avoids carrying a codex model over to claude (or vice versa). Set the
`*_MODEL` variable explicitly to pin a specific model.

### Where to set the variables

Set them in the extension's `extension.toml` under `[subprocess.env]` (the daemon
passes this table to the extension subprocess):

```toml
[subprocess]
command = "go"
args = ["run", "."]

[subprocess.env]
# Run the QA execution on claude instead of codex:
CY_QA_EXECUTION_IDE = "claude"
# CY_QA_EXECUTION_MODEL = "opus"          # optional; omit to use claude's default
# CY_QA_EXECUTION_REASONING_EFFORT = "medium"
```

After editing `extension.toml`, reinstall/re-enable the extension so the daemon
picks up the change:

```bash
compozy ext install <this-directory> --yes
compozy ext enable cy-qa-workflow
compozy daemon stop && compozy daemon start   # restart so the new config is loaded
```

### Example: run the whole QA flow on claude

```toml
[subprocess.env]
CY_QA_EXECUTION_IDE = "claude"
```

The QA report already runs on `claude`; setting only `CY_QA_EXECUTION_IDE=claude`
makes the QA execution run on `claude` too — so no `codex` runtime is required.

## Development

From the compozy repo root:

```bash
make verify-extensions   # builds, race-tests, and lints this module
```
