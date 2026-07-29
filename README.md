# gocd-mcp

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)](https://go.dev/dl/)
[![MCP 2025-11-25](https://img.shields.io/badge/MCP-2025--11--25-6f42c1.svg)](https://modelcontextprotocol.io)

A production-grade [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server
that exposes [GoCD](https://www.gocd.org/) over **Streamable HTTP**, authenticated **per-user**
with GoCD Personal Access Tokens (PAT).

It lets MCP-capable agents (Claude Code, Claude Desktop, IDE extensions, …) inspect and operate
GoCD pipelines — list and trigger pipelines, read statuses/history/console logs, pause/cancel,
and (optionally) edit pipeline configuration — while **GoCD's own RBAC is enforced** for every
call, because the server acts strictly as the authenticated user.

- **Protocol:** MCP revision `2025-11-25`, Streamable HTTP transport
- **SDK:** official [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
- **Language:** Go (1.25+)
- **Target:** a single GoCD instance (verified against GoCD `25.4.0`)

---

## Table of contents

- [Features](#features)
- [Tools & resources](#tools--resources)
- [Requirements](#requirements)
- [Installation](#installation)
- [Configuration](#configuration)
- [Authentication & authorization](#authentication--authorization)
- [Running the server](#running-the-server)
- [Connecting an MCP client](#connecting-an-mcp-client)
- [Toolsets (risk tiers)](#toolsets-risk-tiers)
- [Architecture](#architecture)
- [Security](#security)
- [Logging & observability](#logging--observability)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- 🔐 **Per-user identity** — the MCP client presents a GoCD PAT as a bearer token; the server
  validates it and acts as that user, so GoCD RBAC applies automatically. No shared service account.
- 🧰 **17 tools across three risk tiers** — read-only, safe actions, and config editing — gated
  by a single `TOOLSET` switch.
- 🧱 **Typed, compact outputs** — focused projections of GoCD's responses to keep token usage low.
- 📓 **Audit log** — every mutating operation is logged (login, action, target); tokens are never logged.
- 🩺 **Health probes** — `/healthz` (liveness) and `/readyz` (checks GoCD reachability).
- 🧪 **Well-tested** — unit (domain), contract (GoCD client via `httptest`), and end-to-end
  (MCP over the real SDK client) tests.

---

## Tools & resources

Mutating tools carry MCP annotations (`destructiveHint` / `idempotentHint`) so the host can ask
for user confirmation before running them.

### Tools

| Tool | Toolset | Kind | Description |
|------|---------|------|-------------|
| `whoami` | all | read | Authenticated GoCD login (verifies the token) |
| `list_pipelines` | all | read | Dashboard pipelines with latest run status, pause/lock state (optional group filter) |
| `get_pipeline_status` | all | read | Pause / lock / schedulable state of a pipeline |
| `get_pipeline_history` | all | read | Past runs (paginated via `offset`) with stage statuses |
| `get_pipeline_instance` | all | read | One run's detail incl. per-stage and per-job state/result |
| `get_job_console_log` | all | read | Console log of a job run (last `tail_lines`, default 200) |
| `list_agents` | all | read | Build agents and their config/runtime state |
| `get_pipeline_config` | all | read | Full pipeline config + ETag (needed to update) |
| `trigger_pipeline` | actions, full | action | Schedule a pipeline run; confirms a new instance materialized (bounded wait) rather than trusting GoCD's async accept |
| `pause_pipeline` | actions, full | action | Pause a pipeline (reason required) |
| `unpause_pipeline` | actions, full | action | Resume a paused pipeline |
| `cancel_stage` | actions, full | action | Cancel a running stage |
| `comment_on_pipeline` | actions, full | action | Comment on a pipeline instance |
| `update_pipeline_config` | full | **destructive** | Replace a pipeline config (optimistic locking via ETag/If-Match) |
| `create_pipeline` | full | **destructive** | Create a new pipeline in a group |
| `update_agent` | full | **destructive** | Patch an agent (enable/disable, resources, environments) |
| `delete_pipeline` | full | **destructive** | Delete a pipeline (irreversible) |

### Resources

Read-only mirrors of dashboard data, addressable by URI for a user/app to attach as context:

- `gocd://dashboard` — all pipelines and latest status
- `gocd://agents` — all build agents
- `gocd://pipeline/{name}/config` — a pipeline's configuration
- `gocd://pipeline/{name}/history` — a pipeline's recent runs

---

## Requirements

- Go **1.25+** (to build), or Docker
- Network access to a GoCD server, and its URL (`GOCD_BASE_URL` — **required**, no default)
- A GoCD **Personal Access Token** per user (see below)
- TLS certificate/key for production (or a TLS-terminating proxy)

---

## Installation

### With `go install`

```bash
go install github.com/ivinco/gocd-mcp/cmd/gocd-mcp@latest   # -> $GOBIN/gocd-mcp
```

### From source

```bash
git clone https://github.com/ivinco/gocd-mcp.git
cd gocd-mcp
go build -o gocd-mcp ./cmd/gocd-mcp
```

### Docker

The image is multi-stage and runs on `distroless/static` as a non-root user (`nonroot`,
uid 65532). It carries no configuration of its own: **mount** the config file (and TLS
certificates), and pass any **overrides via environment variables**.

```bash
docker build -t gocd-mcp .
```

**Recommended: config file + certs mounted, secrets/overrides via env.**

Prepare a `config.yaml` (see [`config.example.yaml`](./config.example.yaml)) and your TLS
material, then mount them read-only and point `CONFIG_FILE` at the mounted path:

```bash
docker run --rm -p 8443:8443 \
  -v /etc/gocd-mcp/config.yaml:/etc/gocd-mcp/config.yaml:ro \
  -v /etc/gocd-mcp/certs:/etc/gocd-mcp/certs:ro \
  -e CONFIG_FILE=/etc/gocd-mcp/config.yaml \
  gocd-mcp
```

Because the file wins over the environment, you can ship a baseline `config.yaml` and still
override a single value at runtime only for keys the file omits — keep environment-specific
or sensitive values out of the file and inject them with `-e` (e.g. from your secret store):

```bash
docker run --rm -p 8443:8443 \
  -v /etc/gocd-mcp/config.yaml:/etc/gocd-mcp/config.yaml:ro \
  -v /etc/gocd-mcp/certs:/etc/gocd-mcp/certs:ro \
  -e CONFIG_FILE=/etc/gocd-mcp/config.yaml \
  -e LOG_LEVEL=debug \
  gocd-mcp
```

> Note: the file takes priority, so a key set in `config.yaml` cannot be overridden by `-e`.
> Put values you want to override at runtime in the environment and omit them from the file.

**Env-only** (no file) — omit `CONFIG_FILE` and pass everything as environment variables:

```bash
docker run --rm -p 8443:8443 \
  -e GOCD_BASE_URL=https://gocd.example.com \
  -e TLS_CERT_FILE=/certs/tls.crt -e TLS_KEY_FILE=/certs/tls.key \
  -e TOOLSET=readonly \
  -v /etc/gocd-mcp/certs:/certs:ro \
  gocd-mcp
```

Each MCP user still authenticates with their own GoCD PAT via the `Authorization: Bearer`
header (see [Authentication](#authentication--authorization)); the container holds no GoCD
credential of its own.

---

## Configuration

The server can be configured from **environment variables**, from a **YAML config file**,
or both. Precedence is:

```
built-in default  <  environment variable  <  YAML file   (the file wins)
```

A key present in the file overrides the matching environment variable; a key omitted from
the file falls back to the env var, then to the default. The file is loaded only when
`CONFIG_FILE` points at it (otherwise configuration is env-only, as before).

| Variable | YAML key | Description | Default |
|----------|----------|-------------|---------|
| `CONFIG_FILE` | — | Path to a YAML config file (enables file-based config) | — |
| `GOCD_BASE_URL` | `gocd_base_url` | GoCD server root URL — **required** | — |
| `LISTEN_ADDR` | `listen_addr` | Address to listen on | `:8443` |
| `TLS_CERT_FILE` | `tls_cert_file` | TLS certificate (set together with key) | — |
| `TLS_KEY_FILE` | `tls_key_file` | TLS key (set together with cert) | — |
| `MCP_ENDPOINT_PATH` | `mcp_endpoint_path` | Path of the MCP endpoint | `/mcp` |
| `GOCD_REQUEST_TIMEOUT` | `gocd_request_timeout` | Per-request timeout to GoCD | `30s` |
| `TOKEN_CACHE_TTL` | `token_cache_ttl` | How long a validated PAT is cached | `60s` |
| `TOOLSET` | `toolset` | `readonly` \| `actions` \| `full` | `full` |
| `LOG_LEVEL` | `log_level` | `debug` \| `info` \| `warn` \| `error` | `info` |
| `LOG_FILE` | `log_file` | Log destination; empty → stderr, else append JSON to this file | — |

`GOCD_BASE_URL` is the only required setting — the server refuses to start without it,
from either source. Everything else has a working default.

See [`config.example.yaml`](./config.example.yaml) for a fully annotated sample. Durations
use Go syntax (`30s`, `2m`, `1h`).

---

## Authentication & authorization

The MCP client sends the user's GoCD PAT as a bearer token on **every** request:

```
Authorization: Bearer <gocd-personal-access-token>
```

The server validates the token against GoCD's `current_user` endpoint (caching the result for
`TOKEN_CACHE_TTL`), then builds a **per-request GoCD client bound to that token**. As a result,
every GoCD call is made *as that user* and is subject to GoCD's role-based access control. The
server stores no credentials of its own.

> **Design note.** This is a deliberate, documented deviation from the OAuth 2.1 section of the
> MCP spec: GoCD itself is the identity provider and the PAT is the credential (conceptually the
> stdio "credentials from the environment" model, lifted onto HTTP). A migration path to full
> OAuth 2.1 is kept open by isolating token verification behind a single interface.

> ⚠️ **TLS is required in production.** The PAT travels in the `Authorization` header. Run with
> `TLS_CERT_FILE`/`TLS_KEY_FILE`, or terminate TLS at a trusted proxy. The PAT is never logged.

### Generating a GoCD Personal Access Token

In GoCD, open your user menu → **Personal Access Tokens** → create a token. Each user uses their
own; the token's GoCD permissions define what that user can do through this server.

---

## Running the server

```bash
# Read-only, TLS-enabled
TLS_CERT_FILE=server.crt TLS_KEY_FILE=server.key \
GOCD_BASE_URL=https://gocd.example.com \
TOOLSET=readonly \
./gocd-mcp
```

Health checks (unauthenticated):

```bash
curl -fsS https://localhost:8443/healthz   # -> ok
curl -fsS https://localhost:8443/readyz    # -> ready (200) if GoCD is reachable
```

Unauthenticated requests to the MCP endpoint return `401 Unauthorized`.

### As a systemd service

The binary is static and configuration-free by itself, so a unit is all it takes. Install
it to `/usr/local/bin/gocd-mcp`, put a `config.yaml` next to your TLS material, and point
`CONFIG_FILE` at it:

```ini
[Unit]
Description=GoCD MCP server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=CONFIG_FILE=/etc/gocd-mcp/config.yaml
ExecStart=/usr/local/bin/gocd-mcp
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Set `log_file` in the config to write JSON logs to a path (rotate it with `logrotate`,
using `copytruncate` — the server keeps the file open), or leave it unset to log to
stderr and let journald collect it.

---

## Connecting an MCP client

Point the client at the Streamable HTTP endpoint and supply the GoCD PAT as a bearer token.

**Claude Code** (`.mcp.json` in your project, or via `claude mcp add`):

```json
{
  "mcpServers": {
    "gocd": {
      "type": "http",
      "url": "https://your-host:8443/mcp",
      "headers": { "Authorization": "Bearer ${GOCD_PAT}" }
    }
  }
}
```

**Claude Desktop** (`claude_desktop_config.json`): use the same `mcpServers` block with an
`http` transport and the `Authorization` header.

Set `GOCD_PAT` in your environment to your GoCD Personal Access Token.

---

## Toolsets (risk tiers)

`TOOLSET` gates which tools are registered, so you can deploy the least-privileged surface
appropriate to a use case:

| `TOOLSET` | Includes |
|-----------|----------|
| `readonly` | Queries only — no state changes |
| `actions` | `readonly` + trigger / pause / unpause / cancel / comment |
| `full` | `actions` + config editing (update / create / delete pipeline, update agent) |

A read-only deployment is useful for broad, low-risk access; `full` should be reserved for
trusted operators (GoCD RBAC still applies on top).

---

## Architecture

The protocol layer is intentionally thin; all logic lives in a transport-agnostic domain layer,
so the GoCD integration is independently testable.

```
MCP client ──Streamable HTTP (Bearer PAT)──▶ httpx (TLS, middleware, /healthz /readyz)
                                                └─ bearer-token auth (validate PAT vs GoCD)
                                                    └─ MCP server (tools / resources / prompts)
                                                        └─ domain (validation, orchestration)
                                                            └─ gocd (typed REST client, acts as user)
```

| Package | Responsibility |
|---------|----------------|
| `cmd/gocd-mcp` | Entry point: config → wiring → graceful shutdown |
| `internal/config` | Env-based configuration + validation |
| `internal/httpx` | net/http server, TLS, middleware (recovery / request-id / access log), health probes |
| `internal/auth` | Bearer-token verification (PAT → GoCD), per-request principal, validation cache |
| `internal/mcpsrv` | MCP tools/resources, argument schemas, audit logging, error mapping |
| `internal/domain` | Input validation and orchestration of GoCD calls (no MCP, no net/http) |
| `internal/gocd` | Typed GoCD REST client (pinned API media-type versions, ETag/If-Match, error mapping) |
| `internal/obs` | Structured logging (slog) |

GoCD's API uses per-endpoint versioned media types (`application/vnd.go.cd.vN+json`) and
optimistic locking (ETag + `If-Match`) for config edits; both are handled in `internal/gocd`.

---

## Security

- **TLS required** in production; the PAT is a bearer credential.
- **Tokens are never logged** — access and audit logs contain the GoCD login only.
- **Authorization is delegated to GoCD** — the server never widens a user's permissions.
- **Audit trail** — every mutating action logs `action`, `login`, and `target`.
- **Least privilege** — run with `TOOLSET=readonly` (or `actions`) where full access isn't needed.
- **Input validation** — pipeline/agent identifiers are validated before building request paths.
- Destructive tools are annotated so hosts can require explicit user confirmation.

---

## Logging & observability

Structured JSON logs (`slog`) at `LOG_LEVEL`. Destination is `LOG_FILE` — unset means
stderr (captured by Docker/journald); a path appends JSON there (rotate externally, e.g.
logrotate). **Tokens are never logged.** Three event types:

| `msg` | When | Key fields |
|-------|------|-----------|
| `request` | every HTTP request | `method`, `path`, `status`, `duration_ms`, `request_id`, `login` (for authenticated `/mcp` calls) |
| `tool_call` | every tool invocation (reads and writes) | `tool`, `login`, `ok`, `duration_ms`, `request_id` |
| `audit` | every mutating tool | `action`, `login`, `target` |

`request_id` correlates the `request` and `tool_call` lines of the same call. Tool
*arguments* are not logged (to avoid leaking config bodies); the `audit` line carries the
mutation target.

---

## Development

```bash
go test ./...     # unit (domain), contract (gocd via httptest), e2e (MCP via SDK client)
go vet ./...
gofmt -l .        # should print nothing
go build ./...
```

Project layout follows the package table in [Architecture](#architecture). Tests run fully
offline (a fake GoCD via `httptest`); no live GoCD instance is required.

---

## Troubleshooting

| Symptom | Likely cause / fix |
|---------|--------------------|
| `401 Unauthorized` on `/mcp` | Missing/invalid `Authorization: Bearer <PAT>`, or GoCD rejected the token |
| Tool error "your GoCD user lacks permission" | The user's GoCD RBAC does not allow the operation |
| Tool error "version conflict (ETag mismatch)" | Config changed since you read it — re-run `get_pipeline_config` and retry |
| `/readyz` returns 503 | GoCD is unreachable from the server (check `GOCD_BASE_URL` / network) |
| A tool isn't listed | It's gated by `TOOLSET` — raise the tier (`actions` / `full`) |
| `GOCD_BASE_URL is required` on startup | The base URL has no default — set it in the config file or the environment |

---

## Contributing

Issues and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
build/test loop and the layering rules the codebase keeps to.

Found a security problem? Please report it privately: see [SECURITY.md](SECURITY.md).

---

## License

[MIT](LICENSE) © Ivinco
