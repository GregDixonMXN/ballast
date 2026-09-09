# Events

Typed, persisted, streamed. Types (stable, append-only):

project.created, task.created, task.assigned, workspace.created,
workspace.ready, workspace.destroyed, agent.started, agent.stopped,
agent.message, file.changed, changeset.created, test.started,
test.failed, test.passed, conflict.detected, review.requested,
approval.granted, approval.rejected, merge.completed.

Record: id, project_id, actor_type (human|agent|system), actor_id,
type, entity_id, at, metadata (JSON).

Transport: `internal/events.Bus` (Publish/Subscribe). MVP = in-process
fan-out + Postgres persistence. NATS JetStream replaces the transport
later: subject `ballast.project.<id>`, durable consumer per projection
(feed, conflict detector, audit exporter). REST `GET /events` upgrades
to SSE (`GET /events?project=`); clients fetch history via list
endpoints and stay current on the stream.
