# cy-security-audit

A first-party Compozy extension that helps you verify AI-written code is secure.

It provides two things:

1. **The `/cy-security-audit` skill** — an on-demand application-security audit of a
   feature's implementation. It reviews the changes against an AppSec ruleset
   (OWASP Top 10 + business logic), focused on where AI-generated code slips:
   missing server-side authorization, client-trusted values like `price`,
   over-posting/mass assignment, and sensitive-data exposure. It writes findings as
   `reviews-NNN/issue_NNN.md` files, so `cy-fix-reviews` can remediate them with the
   normal review→fix loop. It does **not** modify code.

2. **A post-run reminder** — a small subprocess that, after a `compozy tasks run`
   completes successfully, prints a non-blocking recommendation to run
   `/cy-security-audit`. It never blocks or fails a run. That's the only reason the
   subprocess exists; the audit is always your call.

## Usage

```bash
compozy ext install --yes .        # from this directory, or install from the repo
compozy ext enable cy-security-audit
```

Then, after a task run finishes and you see the reminder:

```
/cy-security-audit <feature-name>   # audit the feature's implementation
compozy reviews fix <feature-name>  # remediate the findings (cy-fix-reviews)
```

## How it works

- `run.post_start` records each run's execution mode; `run.post_shutdown` emits the
  reminder only for a successful `prd-tasks` run (that is when new implementation
  code exists). Both hooks are observers that never abort a run.
- The skill reuses the exact `reviews-NNN/issue_NNN.md` format that `cy-review-round`
  produces and `cy-fix-reviews` consumes — no new machinery.

## Development

This is a standalone Go module (it ships its own `go.mod`/`go.sum` so `go run .`
resolves the SDK). From the repo root:

```bash
make verify-extensions   # builds, race-tests, and lints this module
```

The security ruleset lives in
`skills/cy-security-audit/references/security-criteria.md` — that file is the core
value; extend it as you learn from real findings.
