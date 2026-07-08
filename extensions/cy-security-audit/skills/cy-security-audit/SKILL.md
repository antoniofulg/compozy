---
name: cy-security-audit
description: Performs an application-security audit of a PRD implementation and generates a review round directory with issue files compatible with cy-fix-reviews. Reviews AI-written code against an AppSec ruleset (OWASP Top 10 + business logic) focused on where AI code slips — missing server-side authorization, client-trusted values like price, over-posting, and sensitive-data exposure. Use after implementing a feature to verify it enforces security properly, then run cy-fix-reviews to remediate. Do not use for fixing existing review issues, executing PRD tasks, editing source code, or fetching reviews from external providers.
argument-hint: [feature-name]
---

# Security Audit

Perform a structured application-security review of a PRD implementation and produce
a review round directory that the `cy-fix-reviews` workflow can process. This audits
the code an AI agent produced for security gaps; it does **not** fix them.

Guiding principle: **never trust the client.** Most findings are a server that
trusted a value, an identity, or a rule it should have re-checked. See
`references/security-criteria.md`.

## Required Inputs

- Feature name identifying the `.compozy/tasks/<name>/` directory.
- Optional: specific files or directories to scope the audit.

## Workflow

1. Determine the review round directory.
   - Derive the PRD directory from the feature name: `.compozy/tasks/<name>/`.
   - Verify the PRD directory exists. If it does not, stop and report the missing directory.
   - List existing `reviews-NNN/` subdirectories to determine the next round number. If none exist, use round 1.
   - If prior review rounds exist, read their issue files to build a list of already-known issues. The current round must only contain NEW findings not already tracked in prior rounds. Do not re-flag issues that are pending, valid, or resolved in earlier rounds.
   - Determine the review round directory path: `.compozy/tasks/<name>/reviews-NNN/` with the round number zero-padded to 3 digits. Do NOT create it yet — wait until step 4 confirms there are findings to write. This avoids leaving empty directories when the audit finds nothing.

2. Identify the audit scope and what is sensitive.
   - Read `_prd.md`, `_techspec.md`, and `_tasks.md` from the PRD directory to understand what was implemented and, critically, **what is sensitive** in this feature: money/pricing, PII, authentication, authorization, multi-tenant boundaries, secrets. Weight the audit toward those areas.
   - Read ADRs from `.compozy/tasks/<name>/adrs/` for security-relevant decisions.
   - If `_prd.md` and `_techspec.md` are both missing, warn that the audit will lack sensitivity context but proceed against the ruleset.
   - If the user provided specific files or directories, scope the audit to those paths.
   - If no explicit scope was provided, run `git diff main...HEAD --name-only` to discover all files created or modified on the current branch. If the diff is empty or unhelpful, ask the user to specify files.
   - Spawn an Agent tool call to map the implementation: endpoints/handlers, the trust boundaries (where request data enters), data stores, and outbound calls.

3. Perform the security review.
   - Read `references/security-criteria.md` for severity definitions and the nine evaluation areas, each with its AI failure mode.
   - Read every file in scope completely before forming conclusions. If the scope exceeds 15 files, triage first: audit the endpoints/handlers, auth/authorization code, and anything touching sensitive data in full; scan the rest for obvious issues.
   - For every endpoint/handler, ask the three questions that catch most AI-generated gaps:
     - **(a) Authorization** — is the caller authorized for _this specific object_, server-side (not just authenticated, not just UI-hidden)?
     - **(b) Trusted input** — does the server trust any client-supplied value it should recompute or validate (price, totals, ids, role, status, quantity)?
     - **(c) Exposure** — what sensitive data does this response or log expose?
   - Trace data flow from the trust boundary (request) to each sink (DB query, response body, shell/command, file path, outbound URL, log). The vulnerability is usually a missing server-side check between them.
   - Assign severity by real impact and exploitability using `references/security-criteria.md`.
   - **Deduplicate before writing.** If the same gap (e.g., missing ownership check) appears across handlers, create one issue for the most representative instance and list the other locations in its Review Comment.
   - **Verify before flagging.** Confirm the gap is real: check for an existing server-side guard elsewhere in the flow (middleware, decorator, policy layer), a validating schema, or a test that proves the behavior is safe. Do not flag a control that is enforced somewhere you did not look.
   - Skip findings a linter/SAST already catches unless they are genuinely security-relevant and unaddressed. Run `make lint` first if available to filter noise.
   - **Focus on signal.** Prefer fewer, confirmed, exploitable findings over an exhaustive list. Keep all critical/high; prune marginal medium/low.
   - Note good security practices observed; these inform the summary but do not produce issue files.
   - If no findings after a thorough audit, report the implementation looks secure and skip steps 4 through 6. Do not create the review round directory.

4. Generate issue files.
   - Create the review round directory determined in step 1.
   - Read `references/issue-template.md` for the canonical format and use it exactly.
   - For each finding, create an `issue_NNN.md` file in the review round directory, numbered sequentially from `001`.
   - Frontmatter rules: `provider: manual`, `pr:` empty (or the user-provided PR number), `round` matching the directory number as an integer, `round_created_at` the same current UTC RFC3339 timestamp in every issue this round, `status: pending`, `author: claude-code`, `provider_ref:` empty, `severity` exactly one of `critical|high|medium|low`.
   - The Review Comment must name the vulnerability, explain the exploit, reference the relevant `security-criteria.md` area, and give the concrete **server-side** fix.

5. Summarize and present the audit.
   - Print a summary listing:
     - **Merge recommendation**: if any critical or high findings exist, state "Needs fixes before merge" with the blocking issues. If only medium/low, "Safe to merge with security follow-ups." If none, "Clean — no security findings."
     - Total findings by severity (critical, high, medium, low).
     - The review round directory path and the list of generated issue file names.
     - Good security practices observed.
   - Suggest running `compozy reviews fix <name>` (the `cy-fix-reviews` workflow) to remediate the round.

6. Verify before completion.
   - Use the installed `cy-final-verify` skill before claiming the audit is complete.
   - Read back each generated issue file and verify the frontmatter parses correctly.
   - Verify every issue file in the round shares matching `provider`, `pr`, `round`, and `round_created_at` values.
   - Confirm the review round directory follows the `reviews-NNN` naming convention.

## Critical Rules

- Do not fix the findings. This skill only identifies and documents them; `cy-fix-reviews` handles remediation.
- Do not modify any source code files. This is an audit-only skill.
- Every issue file must have valid YAML frontmatter parseable by `prompt.ParseReviewContext()`.
- Do not create or maintain review `_meta.md`; round metadata lives in each issue file frontmatter.
- Do not create empty review rounds. If no findings, report a clean audit and do not create the round directory.
- Prefer confirmed, exploitable findings over theoretical ones. A precise, exploitable finding beats ten speculative ones.
- Do not call provider-specific scripts or `gh` mutations.

## Error Handling

- If the PRD directory does not exist, stop and report the missing directory.
- If no files can be identified for audit and the user did not provide explicit paths, ask the user to specify files.
- If both `_prd.md` and `_techspec.md` are missing, warn about the lack of sensitivity context but proceed against the ruleset.
- If the review round directory cannot be created, stop and report the filesystem error.
- If writing an issue file fails, stop and report which file could not be written.
- If `make lint` cannot run, note it in the summary and proceed with the audit; do not skip the audit because linting failed.
