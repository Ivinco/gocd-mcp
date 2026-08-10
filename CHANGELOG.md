# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Breaking:** `get_pipeline_history` is now cursor-paginated and the `offset` input
  is gone. The old parameter was passed through to GoCD, but the current history API
  (verified on GoCD 25.4.0) silently ignores it — every call returned the first page
  regardless. That API paginates by an opaque cursor, so the tool now follows suit:
  the response carries `next_after`, and passing it back as `after` fetches the next
  (older) page. Requests that still send `offset` (even `offset: 0`) are rejected by
  the input schema instead of being silently served the first page. MCP clients
  discover tool schemas at session start, so only hard-coded callers are affected.

### Fixed

- `trigger_pipeline` now attributes the confirmed instance to the caller instead of
  trusting counter growth alone (#3): confirmation requires a new run that is *forced*
  and *approved by the calling user* in GoCD's build cause. A timer trigger, a material
  change or another user's run of the same pipeline inside the wait window is no longer
  reported as this call's instance.

### Added

- `get_pipeline_history` (and the history resource) now include each run's build cause:
  `triggered_by` (the approver — a user login, or `timer` / `changes` for automatic
  runs) and `trigger_forced`.

### Known gaps

- Two concurrent forced triggers by the *same* user within the wait window remain
  indistinguishable — GoCD records only the approver in the build cause, so both calls
  may report the same (earliest) new instance. The most likely way to hit this is
  retrying after an unconfirmed result: if the first attempt's run materializes late,
  the retry may report that earlier instance while a second run also exists — another
  reason to check history instead of retrying blindly. This residual ambiguity is
  accepted; see the discussion in [#3](https://github.com/ivinco/gocd-mcp/issues/3).

## [1.0.2] - 2026-07-30

### Fixed

- `trigger_pipeline` no longer returns a hard error when the schedule request has
  already been accepted but the confirmation poll fails (history read error, cancelled
  request). An error at that point invites a retry, and a retry after an accepted
  schedule can double-run the pipeline — such failures now degrade to the unconfirmed
  result instead. Errors are still returned for failures before anything is scheduled
  (validation, baseline read, non-conflict schedule failures).

### Known gaps

- Run confirmation is counter-based and does not attribute the new instance to this
  specific trigger: a concurrent run of the same pipeline started by another source
  within the wait window can be reported as this call's instance. Proper instance
  attribution is tracked in
  [#3](https://github.com/ivinco/gocd-mcp/issues/3).

## [1.0.1] - 2026-07-29

### Fixed

- `trigger_pipeline` no longer reports success on GoCD's asynchronous `202 Accepted`
  alone. It now watches the pipeline's run counter and returns success only once a new
  instance has actually materialized (bounded wait); if none appears in the window it
  returns an unconfirmed result instead of a false positive. Fixes silent failures on
  concurrent triggers (#1).
- A scheduling conflict (`409`) from `trigger_pipeline` is no longer surfaced as an error.
  GoCD can answer `409` and still schedule the run asynchronously, so failing the call
  produced a false negative — and advising a retry risked double-running the pipeline. A
  conflict is now folded into the same confirmation wait as a `202`: if no new instance is
  confirmed, the tool returns an unconfirmed result telling the caller to check history
  before retrying, never a hard error.

## [1.0.0] - 2026-07-13

First public release. An MCP server for GoCD over Streamable HTTP (MCP `2025-11-25`),
authenticated per-user with GoCD Personal Access Tokens.

### Added

- **Transport & auth**: Streamable HTTP endpoint with TLS support; bearer-token middleware
  validating GoCD PATs against `current_user` (with a short validation cache); a per-request
  GoCD client that acts as the authenticated user, so GoCD's own RBAC applies to every call.
  `/healthz` and `/readyz` probes, recovery / request-id / access-log middleware, graceful
  shutdown.
- **Read-only tools**: `whoami`, `list_pipelines`, `get_pipeline_status`,
  `get_pipeline_history`, `get_pipeline_instance` (per-job detail), `list_agents`,
  `get_pipeline_config`, `get_job_console_log` (files API, with tail).
- **Action tools** (`TOOLSET=actions|full`): `trigger_pipeline`, `pause_pipeline`,
  `unpause_pipeline`, `cancel_stage`, `comment_on_pipeline` — with destructive/idempotent
  annotations so hosts can require confirmation, and an audit log of every mutation.
- **Config tools** (`TOOLSET=full`): `update_pipeline_config` (ETag/If-Match optimistic
  locking), `create_pipeline`, `update_agent`, `delete_pipeline`.
- **Resources**: `gocd://dashboard`, `gocd://agents`, `gocd://pipeline/{name}/config`,
  `gocd://pipeline/{name}/history`.
- **Toolset tiers**: a single `TOOLSET` switch (`readonly` | `actions` | `full`) gates which
  tools are registered, so a deployment can expose the least-privileged surface it needs.
- **Configuration** from environment variables and/or a YAML file (`CONFIG_FILE`). Every
  parameter is readable from either source. Precedence: default < env < file (the file wins
  per-key; omitted keys fall back to env, then to the default). `GOCD_BASE_URL` is the only
  required setting — the server refuses to start without it. Annotated sample in
  `config.example.yaml`.
- **Logging**: structured JSON (`slog`) to stderr or to `LOG_FILE`. Three event types —
  `request` (every HTTP request), `tool_call` (every tool invocation) and `audit` (every
  mutation, with `action` / `login` / `target`), correlated by `request_id`. Tokens are never
  logged, and tool arguments are not logged either (no config-body leakage).
- **Tests**: domain unit tests, GoCD client contract tests (`httptest`), and end-to-end MCP
  tests through the SDK client (auth, toolset gating, forbidden→tool-error, ETag conflict,
  audit). The suite runs fully offline.

### Notes

- GoCD API media-type versions are verified against GoCD 25.4.0; mutating-endpoint versions
  were confirmed with side-effect-free probes. GoCD versions each endpoint independently
  (`application/vnd.go.cd.vN+json`), and config writes use optimistic locking via
  `ETag` / `If-Match`; both are handled in `internal/gocd`.
- Authentication is a deliberate, documented deviation from the OAuth 2.1 section of the MCP
  spec: GoCD itself is the identity provider and the PAT is the credential. See the
  Authentication section of the README.
- `unpause` requires the `X-GoCD-Confirm: true` header on GoCD 25.4.0.

### Known gaps

- `update_agent` is contract-tested only (live verification would mutate a real agent).
- No Prometheus metrics and no MCP prompts yet.
- No retries to GoCD — transient failures surface as tool errors, and the agent may retry.
- HTTP transport only; stdio is not supported yet.

[1.0.2]: https://github.com/ivinco/gocd-mcp/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/ivinco/gocd-mcp/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/ivinco/gocd-mcp/releases/tag/v1.0.0
