# Roadmap

## Current release scope

A trusted, single-host local control plane: persistent projects and task records,
isolated Git working directories, runner dispatch, changeset review, and explicit
integration. See [Verification](VERIFICATION.md) for demonstrated behavior and limits.

## Possible follow-ups (not shipped)

- Durable acknowledged runner queues, reconnect/resume, and task cancellation UI.
- Explicit conflict-resolution/rebase assistance and richer test artifacts.
- A sandboxed execution backend with per-task credentials and resource quotas.
- OIDC, organizations, multi-tenant authorization, and distributed runners.
- Audited deployments, signing, automatic updates, licensing, and support channels.

No roadmap item is a promise of an implemented capability or hosted availability.
Changes should ship as bounded tested vertical slices, without weakening Git,
authorization, or recovery invariants.
