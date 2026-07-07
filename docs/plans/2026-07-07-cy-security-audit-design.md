# cy-security-audit — Design & Implementation Plan

**Date:** 2026-07-07
**Status:** Approved design, ready to implement
**Author:** brainstormed with Claude Code

## 1. Problem & goal

AI coding agents reliably implement the happy path but under-enforce security: they
add a client-side guard but forget the server-side check, trust a client-supplied
`price`, over-post request bodies onto models, and leak sensitive fields in
responses. We want a first-party compozy extension that lets a developer **verify,
on demand, that the code an agent produced enforces application-security properly**,
and that **reminds** the developer to run that verification once a task run finishes.

Non-goals (v1): real-time execution guardrails, blocking runs, automatic (forced)
security passes, and PR-triggered audits (deferred to v2).

## 2. Validated design decisions

| Decision              | Choice                                                                                      | Rationale                                                                    |
| --------------------- | ------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Extension name        | `cy-security-audit`                                                                         | First-party `cy-` extension, in the compozy workflow                         |
| Skill name            | `cy-security-audit` (invoked as `/cy-security-audit`)                                       | Matches the `cy-idea-factory` precedent (extension ships a same-named skill) |
| Primary job           | On-demand AppSec verification of AI-written code                                            | User keeps control; audits are expensive/noisy                               |
| Reminder              | `run.post_start` + `run.post_shutdown` hooks; nudge **only on successful `prd-tasks` runs** | That is when new implementation code exists                                  |
| Reminder is blocking? | No — `required = false`, observer hooks that never error                                    | A reminder must never abort a run                                            |
| Audit scope           | The workflow's PRD implementation; read `_prd.md`/`_techspec.md` for what is sensitive      | Smartest, most targeted; matches `cy-review-round`                           |
| Output                | `reviews-NNN/issue_NNN.md`, the exact `cy-fix-reviews` format                               | Reuses the entire review→fix loop; zero new parsing                          |
| Packaging             | Extension ships the skill (`resources.skills`) + a small Go subprocess for the nudge        | One installable unit                                                         |
| Module                | Ships `go.mod`/`go.sum` from day one                                                        | Applies the packaging fix from PR #1 (Defect 1)                              |
| Deferred (v2)         | Security **review provider** for GitHub PRs                                                 | Bolts on without reworking v1                                                |

## 3. Why this is small

The heavy machinery already exists and is reused verbatim:

- **Issue format & parsing** — `reviews-NNN/issue_NNN.md` is parsed by
  `reviews.ReadReviewEntries()` / `prompt.ParseReviewContext()`. We copy
  `skills/cy-review-round/references/issue-template.md`.
- **Round numbering + cross-round dedup** — the `cy-review-round` workflow already
  defines "find the next `reviews-NNN`, don't re-flag issues from prior rounds."
- **Remediation** — `cy-fix-reviews` consumes the round and fixes each issue.

The only genuinely new artifacts are:

1. `references/security-criteria.md` — the AppSec ruleset (the real value).
2. A ~60-line Go subprocess implementing two observer hooks for the nudge.

No daemon changes. No new host API.

## 4. Architecture

```
extensions/cy-security-audit/
  extension.toml            # ships the skill + declares the nudge subprocess/hooks
  go.mod / go.sum           # standalone module (module example.com/cy-security-audit)
  main.go                   # subprocess: run.post_start + run.post_shutdown -> nudge
  main_test.go              # table-driven test of the pure nudge-decision function
  README.md
  skills/
    cy-security-audit/
      SKILL.md                          # the audit workflow (security-lensed review round)
      references/
        security-criteria.md            # the AppSec ruleset (NEW — core value)
        issue-template.md               # copied verbatim from cy-review-round
```

### 4.1 The nudge subprocess

`run.post_shutdown` exposes only `Summary.Status` — **not** the run mode. The mode
(`prd-tasks` / `exec` / `pr-review`) is on `RunPostStartPayload.Config.Mode`.
Therefore the extension registers **two** observer hooks:

- `OnRunPostStart` — record `payload.Config.Mode` keyed by `payload.RunID`.
- `OnRunPostShutdown` — look up the remembered mode for `payload.RunID`; if
  `mode == prd-tasks && Summary.Status == "succeeded"`, print the nudge to stderr,
  then drop the entry.

State is a `map[string]ExecutionMode` guarded by a `sync.Mutex` (robust whether the
subprocess is per-run or persistent across runs). Both handlers **always return
`nil`** — they are advisory. Both `run.*` hooks require the `run.mutate` capability.

Nudge text (stderr):

```
✅ tasks complete — run `/cy-security-audit` to verify the changes against the AppSec ruleset,
   then `/cy-fix-reviews` to remediate any findings.
```

### 4.2 The skill (`/cy-security-audit`)

A security-lensed `cy-review-round`. Inputs: workflow name + PRD dir (same inputs
`cy-review-round` takes). Workflow:

1. Resolve the review round dir: next `reviews-NNN` under `.compozy/tasks/<wf>/`;
   read prior rounds to avoid re-flagging known issues; do **not** create the dir
   until step 4 confirms findings exist.
2. Gather the implementation under audit (the workflow's changed files) and, if
   present, read `_prd.md` / `_techspec.md` to learn what data/flows are sensitive.
3. Review against `references/security-criteria.md`, reasoning explicitly about the
   AI failure mode for each criterion. Assign `critical|high|medium|low`.
4. If no issues: report "clean," write nothing. Otherwise create the round dir and
   one `issue_NNN.md` per finding using `references/issue-template.md`.
5. Report a summary and tell the user to run `/cy-fix-reviews`.

### 4.3 The ruleset (`security-criteria.md`) — outline

Backbone: OWASP Top 10 (2021) + business-logic. Each criterion documents **what to
check**, the **AI failure mode**, and **severity guidance**.

1. **Broken access control / authorization** — server-side authz on every sensitive
   op; object-level ownership checks (IDOR); function/role checks server-side;
   every client-side rule re-enforced server-side. _AI failure mode:_ implements the
   UI guard + endpoint, omits the ownership check. (critical/high)
2. **Business-logic integrity** — server recomputes/validates sensitive values
   (price, totals, discounts, quantities); mass-assignment/over-posting guard;
   idempotency/replay/state validation. _AI failure mode:_ persists `req.body.price`;
   binds the whole request onto the model. (critical/high)
3. **Input validation & injection** — server-side validation (type/range/allowlist);
   parameterized queries (SQLi); output encoding (XSS); command/path/template
   injection; SSRF on outbound calls. _AI failure mode:_ string-built queries,
   unvalidated redirects. (critical/high)
4. **Sensitive data & secrets** — data classification; encryption in transit/at rest;
   no secrets in logs/errors/responses; minimal-exposure responses; no hardcoded
   creds. _AI failure mode:_ returns full user object incl. hash/tokens; logs PII.
   (high)
5. **Authentication & session** — token validation, expiry/revocation, secure
   sessions. (high)
6. **Security misconfiguration** — CORS, security headers, verbose error leakage,
   insecure defaults. (medium)
7. **Vulnerable & outdated dependencies** — flag known-risky/pinned-old deps in the
   diff. (medium)
8. **Security logging & auditability** — security-relevant events logged without
   leaking sensitive data. (low/medium)

### 4.4 extension.toml

```toml
[extension]
name = "cy-security-audit"
version = "0.1.0"
description = "On-demand application-security audit of AI-written code, plus a post-run reminder"
min_compozy_version = "0.2.11"

[subprocess]
command = "go"
args = ["run", "."]

[security]
capabilities = ["skills.ship", "run.mutate"]

[resources]
skills = ["skills/*"]

[[hooks]]
event = "run.post_start"
required = false

[[hooks]]
event = "run.post_shutdown"
required = false
```

## 5. Implementation plan (phased)

### Phase 1 — Extension skeleton & packaging

- Create `extensions/cy-security-audit/` with `extension.toml` (above).
- `go.mod` (`module example.com/cy-security-audit`, `require github.com/compozy/compozy vX.Y.Z`)
  then `go mod tidy` for `go.sum`. Match the pattern established for `cy-qa-workflow`.
- Add `extensions/cy-security-audit` to `EXTENSION_GO_MODULES` in the `Makefile`
  (the `verify-extensions` target already build/test/lints each module).
- Gitignore the compiled binary: `/extensions/cy-security-audit/cy-security-audit`.

### Phase 2 — Nudge subprocess

- `main.go`: `extension.New("cy-security-audit", version).WithCapabilities(run.mutate)
.OnRunPostStart(...).OnRunPostShutdown(...).Start(ctx)`.
- Extract a pure decision function `shouldNudge(mode ExecutionMode, status string) bool`
  returning `mode == prd-tasks && status == "succeeded"` — unit-testable without I/O.
- Guard the `map[string]ExecutionMode` with a `sync.Mutex`; delete the entry on shutdown.
- Both handlers return `nil` unconditionally.
- Confirm during build how the SDK reconciles the subprocess-declared capabilities
  (`run.mutate`) with the manifest's `skills.ship` + `run.mutate`.

### Phase 3 — Skill

- `skills/cy-security-audit/SKILL.md` modeled on `cy-review-round/SKILL.md`, security-lensed.
- `references/security-criteria.md` — author the full ruleset from the §4.3 outline
  (this is the main writing effort; each criterion = check + AI failure mode + severity).
- `references/issue-template.md` — copy from `cy-review-round`.

### Phase 4 — Tests & verification

- `main_test.go`: table-driven `shouldNudge` cases (prd-tasks+succeeded → true; exec/
  pr-review/failed/cancelled → false) and a post_start→post_shutdown sequence test.
- `make verify-extensions` (build + `-race` test + lint, zero issues).
- Manual E2E: install + enable; run a `tasks` workflow; confirm the nudge; run
  `/cy-security-audit`; confirm `reviews-NNN/issue_NNN.md`; run `/cy-fix-reviews`.

### Phase 5 — Docs

- `extensions/cy-security-audit/README.md`; short entry under `docs/extensibility` if warranted.

## 6. Testing strategy

- **Unit (Go):** the nudge decision function and the post_start→post_shutdown state
  handling (per CLAUDE.md: table-driven, `-race`, no production test-only methods).
- **Skill:** exercised manually against a real workflow; the output is validated by
  feeding it to `cy-fix-reviews` (proves format compatibility end-to-end).
- **Regression:** the extension module is covered by `verify-extensions` in CI.

## 7. Risks & open questions

- **Nudge visibility.** Extension stderr surfacing at run end depends on how the
  daemon renders extension output. Verify the nudge is actually visible to the user;
  if not, consider an event-based surface. (Low risk; observer hooks already print to
  stderr in the SDK examples.)
- **Ruleset quality is the product.** The extension's value is the criteria doc;
  budget real effort there and iterate from real findings.
- **False positives / noise.** Security reviews over-flag. Lean on severity + the
  cross-round dedup already in `cy-review-round` to keep signal high.
- **Subprocess lifecycle.** The RunID-keyed, mutex-guarded map is safe whether the
  subprocess is per-run or persistent; confirm the lifecycle during build.
- **SDK version pin.** Pin `require github.com/compozy/compozy` to the current
  release; bump on each release (a release-automation follow-up, same as `cy-qa-workflow`).

## 8. Future (v2+)

- Security **review provider** so `/cy-security-audit` can run against a GitHub PR
  and feed `cy-fix-reviews`.
- Optional `--scope` argument on the skill: PRD implementation (default) | branch
  diff | whole repo.
- Optional deterministic scanners (gitleaks/semgrep) to complement the LLM review
  with high-confidence findings.
