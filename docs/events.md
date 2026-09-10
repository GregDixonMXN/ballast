# Events and activity

`GET /activity?project=<id>` returns recent persisted operational history.
`GET /events?project=<id>` streams new events using SSE. Both require bearer
authentication; credentials must stay in headers, never URL query parameters.
The dashboard may poll authenticated state/history rather than using EventSource,
which cannot set an Authorization header directly.

Event fields: id, project_id, actor_type, actor_id, type, entity_id, at, metadata.
Implemented workflows emit project/task/workspace, execution/test, review, and
integration events. The event type declarations also include future-facing types;
the presence of a constant is not evidence that every path emits it.

The bus persists through the configured store and fans out within one process.
SSE does not replay missed history: reconnecting clients should refetch current
state/activity. Events are not a complete transactional compliance audit, a durable
work queue, or an exactly-once distributed message bus. Keep that distinction when
building consumers or interpreting a missing/duplicate event after a failure.
