# Contributing

Thanks for taking the time to contribute. Bug reports, feature requests and pull
requests are all welcome.

## Getting started

```bash
git clone https://github.com/ivinco/gocd-mcp.git
cd gocd-mcp
go build ./...
go test ./...
```

Go **1.25+** is required. The test suite runs fully offline — GoCD is faked with
`httptest`, so no live GoCD instance is needed.

## Before opening a pull request

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./...
```

- **Keep the layers intact.** `internal/domain` must not import MCP or `net/http`;
  `internal/gocd` speaks only to the GoCD REST API. Protocol details live in
  `internal/mcpsrv`, transport in `internal/httpx`. See the architecture table in the
  [README](README.md#architecture).
- **Tests come with the change.** Domain logic gets a unit test; a new GoCD endpoint
  gets a contract test against `httptest`; a new tool gets an end-to-end test through
  the MCP SDK client.
- **Never log a token.** Access, audit and tool-call logs carry the GoCD *login* only.
- **Annotate mutating tools** with `destructiveHint` / `idempotentHint` so hosts can
  ask the user for confirmation.
- **Respect the toolset tiers.** A new tool must be registered in the right tier
  (`readonly`, `actions`, `full`) — see [Toolsets](README.md#toolsets-risk-tiers).

## Adding a GoCD endpoint

GoCD versions each endpoint's media type independently
(`application/vnd.go.cd.vN+json`), and config writes use optimistic locking via
`ETag` / `If-Match`. Both are handled in `internal/gocd`; pin the version you verified
in `internal/gocd/apiversions.go` and note the GoCD release you tested against.

## Commit messages and PRs

Write a short imperative subject ("Add pipeline-group filter") and explain *why* in the
body when it isn't obvious. Update `CHANGELOG.md` under an `Unreleased` heading for any
user-visible change.

## Reporting security issues

Please do **not** open a public issue — see [SECURITY.md](SECURITY.md).
