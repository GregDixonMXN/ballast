# Security and trust boundaries

Ballast 1.0.0-rc.1 is a **trusted, single-host local tool**. Do not expose its
API or dashboard directly to the internet. Worktrees are not containers,
virtual machines, or a security boundary against code running as your user.

## Credentials

The server creates a persistent private operator-token file, refuses unsafe
credential-file permissions, and does not print the credential. The dashboard
keeps an entered token in memory only. Refresh or disconnect clears it. CLI
and runner setup should use a private token file instead of command arguments.
Runner credentials are scoped to runner API operations and their assignments.

An operator token grants powerful local actions, including access to local
repository paths and dispatching commands. Protect it like an administrator
credential. Diffs and task text may contain secrets; they are not automatically
redacted. Exclude secrets in the repositories themselves and review changesets.

## Execution

Use only repositories, commands, and agent binaries you trust. A runner has the
OS account's filesystem/network permissions and shares Git administrative data
with other worktrees. HTTP authorization does not prevent a local agent from
editing another path or running Git directly. Do not grant Docker socket access
or elevated host permissions merely to run Ballast.

The local store is a single-control-plane deployment, not a distributed
transaction coordinator. Stop writers before backups and before manipulating
canonical branches outside Ballast. Keep ordinary Git backups; no tool can
protect files from every disk failure or an external process modifying them.

## Reporting

Do not publish credentials, private source, state snapshots, or exploit payloads
against a real deployment in a public issue. Send a minimal synthetic reproducer
through the repository owner's established private contact channel. No dedicated
security mailbox or response-time guarantee has been configured yet.

Public hosting, multi-tenant authorization, OIDC, a secrets broker, cryptographic
release signing, and formal third-party security certification are not included.
