# Issue File Template

Use this exact structure for every issue file. The file is parsed by
`reviews.ReadReviewEntries()` and `prompt.ParseReviewContext()`, so it must be
byte-compatible with the format `cy-review-round` produces and `cy-fix-reviews`
consumes.

## Format

```
---
provider: manual
pr:
round: <N>
round_created_at: <UTC timestamp in RFC3339 format>
status: pending
file: path/to/file.go
line: 42
severity: critical|high|medium|low
author: claude-code
provider_ref:
---

# Issue NNN: <concise title summarizing the security problem>

## Review Comment

<what the vulnerability is, why it is exploitable, and a concrete server-side fix.
Reference the security-criteria.md area (e.g., "Access control / IDOR"). Keep any
code snippet under 15 lines.>

## Triage

- Decision: `UNREVIEWED`
- Notes:
```

## Field Definitions

- **NNN**: Three-digit zero-padded issue number (001, 002, ...). File name must be
  `issue_NNN.md` for `prompt.ExtractIssueNumber()` to recognize it.
- **provider**: Always `manual` for this audit.
- **pr**: Empty unless the user supplied a PR number.
- **round**: The review round number as an integer (not zero-padded); matches the
  `reviews-NNN` directory number.
- **round_created_at**: One current UTC RFC3339 timestamp, identical across every
  issue written in the same round.
- **status**: Starts `pending`; the remediation loop moves it through `valid` /
  `invalid` and finally `resolved`.
- **file**: Repository-root-relative path to the affected file. Use `unknown` only
  for a purely architectural finding not tied to one file.
- **line**: Line where the issue is most visible; `0` when none applies.
- **severity**: Exactly one of `critical`, `high`, `medium`, `low` (see
  `security-criteria.md`).
- **author**: Always `claude-code`.
- **provider_ref**: Always empty.

## Rules

- One issue per distinct vulnerability. If the same gap repeats across files, file
  one representative issue and list the other locations in the Review Comment.
- The Review Comment must be actionable: name the vulnerability, explain the
  exploit, and give the concrete server-side fix.
- Keep the title descriptive but short (max 72 characters).
  Good: "IDOR: GET /orders/:id returns orders owned by other users".
  Bad: "Security issue".
