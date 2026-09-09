# Roadmap

Done (MVP vertical slice): projects → repo → tasks → worktrees →
agents → changes → changesets → review → controlled integration, with
conflict V1, leases, events+SSE, runner protocol, audit-ready schema.

Next, in order:

1. Wire services to Postgres in cmd/server (persistence behind the
   interfaces already defined in internal/api).
2. Runner work dispatch (long-poll) + test execution + TestRun records.
3. NATS JetStream transport behind events.Bus.
4. OIDC auth + per-workspace scoped credentials + secrets broker.
5. Temporal workflows: long agent runs, approval waits, crash recovery.
6. Preview environments + staging/prod deploys + rollback.
7. OpenFGA fine-grained authorization.
8. Semantic conflicts: symbols, API contracts, schema, dependencies.
9. Cost/token tracking, agent messaging, auto-integration planning.

Non-goals until hosted runners exist: EKS/Kubernetes, multi-repo,
SSO, audit export, policy engine.
