# Changelog

## v0.5.0

Tool contract bumped to **2.0** — the client stamps every request and the
server rejects a major mismatch, so the two binaries can evolve safely.

### Client (`terra-drift`)

- Edits are **verified**: after applying, `terraform fmt` + `validate` run and
  a fresh refresh-plan must show the resource clean before it is called fixed.
- **Minimal-diff guard**: a proposed patch may only touch files on the drifted
  attribute's provenance chain and the drifted/origin attributes; anything
  else is rejected and the resource skipped with a clear message.
- **PR dedupe**: a stable drift-set hash is embedded in the PR body
  (`drift-hash:`); an open PR with the same hash short-circuits the run, a
  different hash updates the existing PR instead of opening a duplicate.
- Refresh failures are **classified** (no-identity-policy, boundary-denied,
  trust-denied, provider-error) and reported as skipped roots.
- Outcomes reported in three buckets, in logs and the PR body:
  **fixed / skipped-protected / could-not-fix**.
- `sync --dry-run` rewrites, prints the diff, restores — no branch/commit/PR.
- Stable exit codes: 0 = no drift, 2 = drift found, 1 = error.
- Sends the model server only **minimal HCL snippets**, never whole files.

### Server (`terra-drift-mcp`)

- **Redaction**: secrets, ARNs, IPs/CIDRs, and account ids are replaced with
  stable placeholders before the model call and restored in the reply — raw
  infra values never reach the external model.
- **Validated output**: the returned patch is schema-checked and every edit
  must stay inside the client's allowed attribute set; violations retry once,
  then fail with a typed `invalid-output` error. Temperature 0, tightened
  system prompt (edit only these attributes; no new resources; no reformatting).
- **Cost controls**: max prompt size, per-request timeout, and a rate limit —
  all fail closed with typed errors (`budget-exceeded`, `rate-limited`).
- **Auth**: the HTTP transport requires a bearer token
  (`TERRA_DRIFT_MCP_AUTH_TOKEN`) and refuses to start without one.
- **Caching**: proposals are cached by (address, drifted attrs, live value,
  provider, model, contract version) with a TTL, so the daily cron does not
  re-spend on unchanged drift; cache hits are logged.
- **Observability**: structured JSON logs (request id, address, tokens,
  latency, outcome, cache hit) plus `/healthz`, `/readyz`, and `/metrics`.
- Typed errors (`gateway-down`, `invalid-output`, `budget-exceeded`,
  `rate-limited`, `contract-mismatch`) that the client reports in the PR
  instead of failing the run.

### Earlier

- Bitbucket branch+commit published via the REST `/src` endpoint instead of
  `git push` (works with Atlassian API tokens); `git.push_mode: api|git`.
- Drift report shows `file:line` per resource; read-only `explain_drift` tool.
- Trust decision (`code | live | partial`) before any code changes.
