# Security Criteria

Application-security review criteria for auditing AI-written code. The backbone is
the OWASP Top 10 (2021) plus business-logic integrity. Each evaluation area lists
**what to check** and the **AI failure mode** — the specific way AI-generated code
tends to break that criterion — because that is where to look first.

Guiding principle: **never trust the client.** Any rule enforced in the UI, the
request shape, or client-supplied values must be independently enforced and
re-validated on the server. Most findings in this audit are a server that trusted
something it should have checked.

## Severity Levels

### critical

Directly exploitable to compromise data, funds, or accounts: authentication
bypass, broken object-level authorization (IDOR), injection, server trusting a
client-supplied price/amount, secret exposure. Assume an attacker will find it.

### high

A real security gap that needs fixing before merge even if exploitation needs a
precondition: missing server-side validation at a boundary, over-posting/mass
assignment, sensitive data returned in a response, missing authorization on a
non-obvious path, weak session handling.

### medium

Hardening gaps and defense-in-depth weaknesses: missing security headers,
permissive CORS, verbose error leakage, unpinned/outdated risky dependency,
insufficient security logging.

### low

Minor improvements: a hardening opportunity with low impact, a defense-in-depth
suggestion, or documentation of a security assumption.

## Evaluation Areas

### 1. Access control & authorization (OWASP A01)

- Every sensitive operation performs a **server-side** authorization check; nothing
  relies on the UI hiding an action or omitting a button.
- **Object-level** authorization (IDOR): the server verifies the authenticated
  caller may access the specific resource id in the request, not just that they are
  logged in.
- **Function/role-level** authorization is enforced on the server for every
  privileged endpoint, not inferred from a client-sent role/flag.
- Default-deny: new endpoints require auth unless explicitly and intentionally public.
- _AI failure mode:_ implements the client-side guard and a working endpoint but
  omits the ownership check — `GET /orders/:id` returns any order regardless of who
  owns it; a role is read from `req.body.role` or a JWT claim that the client can set.

### 2. Business-logic integrity

- The server **recomputes or re-validates** every sensitive value rather than
  trusting the client: price, totals, discounts, tax, quantities, currency,
  account/user ids, status transitions.
- **Mass assignment / over-posting** is prevented: request bodies are bound to an
  explicit allowlist of fields, never the whole model (no client setting
  `isAdmin`, `role`, `balance`, `price`, `verified`).
- Idempotency and replay protection on state-changing / financial operations;
  workflow state transitions are validated (a step cannot be skipped or repeated).
- Quantities/limits/rates are enforced server-side (no negative quantity, no
  bypassing a purchase limit).
- _AI failure mode:_ persists `req.body.price` / `total` as-is; binds the entire
  request object onto the ORM model; trusts a client `status: "paid"`; recomputes
  nothing.

### 3. Input validation & injection (OWASP A03)

- All inputs validated **server-side** for type, range, length, and allowed values
  (allowlist over denylist), at the trust boundary.
- SQL/NoSQL: parameterized queries / prepared statements; never string-built queries.
- Output encoding to prevent XSS; no unsanitized HTML sinks
  (`dangerouslySetInnerHTML`, `v-html`, template `| safe`).
- No command injection (no shell string interpolation), path traversal (validate/
  canonicalize file paths), or template/expression injection.
- Redirects and forwards validated against an allowlist (no open redirect).
- _AI failure mode:_ concatenates user input into a query or shell command; renders
  user-controlled HTML directly; builds file paths from user input without
  canonicalization; validates only on the client.

### 4. Sensitive data exposure & secrets (OWASP A02)

- Sensitive data is classified (PII, credentials, tokens, financial, health) and
  handled accordingly; encrypted in transit (TLS) and at rest where appropriate.
- API responses expose the **minimum** necessary fields — no leaking password
  hashes, tokens, internal ids, other users' data, or full object graphs.
- No secrets in logs, error messages, stack traces, or responses; no secrets or
  credentials hardcoded in source (use config/secret manager).
- Correct password storage (strong adaptive hash — bcrypt/scrypt/argon2 — never
  plaintext/MD5/SHA1); tokens stored/compared safely.
- _AI failure mode:_ returns the full user record including `passwordHash`/tokens;
  logs the request body containing PII or an auth header; hardcodes an API key;
  serializes an entire ORM entity to the client.

### 5. Authentication & session management (OWASP A07)

- Authentication is enforced correctly; tokens/sessions are validated on every
  request (signature, expiry, audience, issuer).
- Session/token lifecycle is safe: expiry, rotation, server-side revocation/logout;
  secure cookie flags (`HttpOnly`, `Secure`, `SameSite`).
- No auth bypass via missing checks, trusting unverified claims, or accepting
  `alg: none` / unverified JWTs; sensitive flows protected against brute force.
- _AI failure mode:_ decodes a JWT without verifying the signature; trusts a
  user id from the token body without validation; omits expiry checks; sets a
  session cookie without `HttpOnly`/`Secure`.

### 6. Server-side request forgery & unsafe deserialization (OWASP A10, A08)

- Outbound requests built from user input validate the target against an allowlist;
  no fetching arbitrary user-supplied URLs (SSRF), especially to internal/metadata
  hosts.
- Deserialization of untrusted input uses safe formats/parsers; no unsafe native
  deserialization of attacker-controlled data.
- Webhooks/callbacks verify signatures; file uploads validate type/size and are
  stored outside executable paths.
- _AI failure mode:_ `fetch(req.body.url)` with no validation; loads/evaluates
  attacker-controlled serialized data; trusts an unsigned webhook payload.

### 7. Security misconfiguration (OWASP A05)

- CORS is not permissive (`*` with credentials, or reflecting arbitrary origins).
- Security headers present where relevant (CSP, HSTS, `X-Content-Type-Options`,
  `X-Frame-Options`/frame-ancestors).
- Errors returned to clients do not leak stack traces, queries, or internal details;
  debug endpoints/verbose modes are not enabled in production paths.
- Secure defaults: least-privilege DB/service accounts, no default credentials.
- _AI failure mode:_ enables permissive CORS "to make it work"; returns raw error
  objects/stack traces to the client; leaves a debug flag on.

### 8. Vulnerable & outdated dependencies (OWASP A06)

- New or changed dependencies are reputable and reasonably current; flag additions
  with known-vulnerable versions or obviously abandoned packages.
- No risky transitive patterns introduced (e.g., a package that shells out on
  install/use); lockfile changes are intentional.
- _AI failure mode:_ adds a dependency pinned to an old, known-vulnerable version,
  or an unnecessary package that expands attack surface.

### 9. Security logging & auditability (OWASP A09)

- Security-relevant events (authn/authz failures, privilege changes, sensitive
  mutations) are logged with enough context to investigate.
- Logs do **not** contain secrets, tokens, full PII, or full request bodies.
- _AI failure mode:_ logs nothing on auth failures, or logs everything including
  credentials/PII.

## Review Approach

- Read `_prd.md` / `_techspec.md` first to learn **what is sensitive** in this
  feature (money, PII, auth, multi-tenant boundaries) and weight the audit toward it.
- For every endpoint/handler in scope, ask the three questions that catch most
  AI-generated gaps: **(a)** Is the caller authorized for _this specific object_?
  **(b)** Does the server trust any client-supplied value it should recompute or
  validate? **(c)** What sensitive data does this response/log expose?
- Prefer confirmed, exploitable findings over theoretical ones; assign severity by
  real impact and exploitability.
- Trace data flow from the trust boundary (request) to the sink (DB, response, shell,
  outbound call); the gap is usually a missing server-side check between them.
- One issue per distinct problem. If the same gap repeats across handlers, file one
  representative issue and list the other locations in its Review Comment.
- Do not flag issues a linter/SAST already catches unless they are genuinely
  security-relevant and unaddressed. Acknowledge good security practices in the
  summary; do not create issues for them.
