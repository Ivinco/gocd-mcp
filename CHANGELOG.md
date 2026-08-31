# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0] - 2026-08-31

### Added

- Pipeline template management (#4). Read-only tier: `list_templates` lists templates
  with the pipelines using each one and whether the caller may edit / administer it;
  `get_template` returns a template's full config with its ETag. `full` tier (audited):
  `create_template`, `update_template` (optimistic locking via ETag/If-Match) and
  `delete_template`. Verified against the GoCD 25.4.0 templates API (`v7`).
  `update_template` rejects an object whose name differs from the target before the
  round-trip — the API cannot rename templates and answers a mismatch with a
  misleading 422. Deleting a template that pipelines still use is refused by GoCD;
  the refusal, naming those pipelines, is returned as the tool error.
- `trigger_stage` (`actions` tier, audited) runs one stage of an existing pipeline run:
  a manual-approval stage that has not run yet, or a fresh run of a stage that already
  has. Confirmation follows `trigger_pipeline`: GoCD's async accept is not trusted, and
  the tool reports `ok:true` only once the pipeline instance shows the stage scheduled,
  with a counter above the baseline read before the request, and approved by the
  calling user — otherwise the unconfirmed `ok:false` result, never an error, so a
  blind retry does not run the stage twice. One deliberate difference: GoCD's 409 for
  a stage run ("Cannot schedule: … is still in progress") is a synchronous refusal
  that schedules nothing, so it is returned at once as an error carrying that reason
  instead of being folded into the wait.
- `get_pipeline_instance` stages now carry `counter`, `scheduled`, `approval_type`,
  `approved_by` and `can_run`, so a manual stage awaiting approval — and the stage
  counter that `cancel_stage` and `get_job_console_log` need — can be read directly.

### Changed

- A `409` from GoCD now keeps GoCD's message (`gocd.ConflictError`, matching
  `ErrConflict`), and a `412` maps to the new `ErrPreconditionFailed` instead of
  sharing `ErrConflict`. Tool errors for a `409` show GoCD's reason ("GoCD refused:
  …"); an ETag mismatch (`412`) keeps the re-read-and-retry hint. Verified on GoCD
  25.4.0.
- Other GoCD errors (`422` validation failures, `5xx`) surface GoCD's explanation
  instead of the raw JSON body: the tool error reads "GoCD rejected the request (HTTP
  422): <message>", followed by the field-level validation errors GoCD nests under
  `data` ("Details: materials[0].auto_update: …") — where the top-level message often
  says no more than "Validation failed.". Responses that are not in GoCD's
  `{"message": …}` shape still fall back to the raw body.

### Fixed

- `get_pipeline_config` for an unknown pipeline (or any GoCD error) crashed the whole
  server. The tool's error result carried a `null` config, which fails the SDK's
  output-schema validation even on error results; the SDK then hands the tool-call
  logging middleware a nil result, and dereferencing it panicked inside the SDK's
  handler goroutine, beyond the HTTP recovery middleware. Map-typed output fields
  are now omitted on error, and the middleware tolerates a nil result so no handler
  failure can take the process down again.

### Known gaps

- `trigger_stage` confirms through the pipeline instance, which shows only a stage's
  latest run. A re-run of the same stage by someone else inside the wait window —
  possible only once yours has finished, since GoCD refuses concurrent runs — hides
  yours, and the call reports unconfirmed although it ran. Confirming through the
  stage history API would close this; accepted for now.

## [1.1.0] - 2026-08-13

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

[1.2.0]: https://github.com/ivinco/gocd-mcp/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/ivinco/gocd-mcp/compare/v1.0.2...v1.1.0
[1.0.2]: https://github.com/ivinco/gocd-mcp/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/ivinco/gocd-mcp/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/ivinco/gocd-mcp/releases/tag/v1.0.0
