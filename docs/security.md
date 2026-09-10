# Security

The current security and deployment contract is [SECURITY.md](../SECURITY.md).
Ballast is a trusted single-host local tool, not a multi-tenant service. Detached
Git worktrees isolate working directories but do not sandbox agent OS access.
OIDC, credential brokering, and production secret redaction are not implemented.
