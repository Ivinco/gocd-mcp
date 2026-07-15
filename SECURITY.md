# Security Policy

## Supported versions

Security fixes land on the latest released minor version. Please upgrade before
reporting an issue against an older release.

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through GitHub: open the repository's **Security** tab and choose
**Report a vulnerability** (GitHub Private Vulnerability Reporting). Include the
affected version, a description of the impact, and — if you have one — a minimal
reproduction.

We aim to acknowledge a report within a few working days and will keep you updated as
we work on a fix. Once a fix is released we are happy to credit you, unless you prefer
to stay anonymous.

## Security model, in short

Understanding these properties will help you judge whether something is a vulnerability:

- **The server holds no GoCD credentials.** Each MCP client presents the *user's* GoCD
  Personal Access Token as a bearer token, and the server acts strictly as that user.
- **Authorization is GoCD's.** Every call goes through GoCD's own RBAC; the server never
  widens a user's permissions.
- **Tokens are never logged.** Access, audit and tool-call logs contain the GoCD login
  only; tool arguments are not logged.
- **TLS is required in production.** The PAT is a bearer credential and travels in the
  `Authorization` header. Run with `TLS_CERT_FILE`/`TLS_KEY_FILE`, or terminate TLS at a
  trusted proxy.
- **Least privilege is available.** `TOOLSET=readonly` (or `actions`) narrows the exposed
  surface; destructive tools are annotated so hosts can demand explicit confirmation.

Anything that breaks one of these properties — a token reaching a log, a user acting
beyond their GoCD permissions, a tool escaping its toolset tier — is a vulnerability and
we want to hear about it.

## Out of scope

- Deployments serving `/mcp` over plain HTTP without a TLS-terminating proxy.
- Granting a user broad GoCD permissions and then observing that they can use them.
