// Package events is the typed nervous system of Ballast.
// Every important state transition emits an Event, persisted to Postgres
// and fanned out to subscribers. The Bus interface is transport-agnostic:
// the MVP uses an in-process + DB bus; NATS JetStream can replace it
// without touching domain code (subject per project, durable consumers).
package events

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Type enumerates all MVP event types. Future types (symbol leases,
// deployments, agent negotiation) extend this list, never rename it.
type Type string

const (
	ProjectCreated     Type = "project.created"
	TaskCreated        Type = "task.created"
	TaskAssigned       Type = "task.assigned"
	WorkspaceCreated   Type = "workspace.created"
	WorkspaceReady     Type = "workspace.ready"
	WorkspaceDestroyed Type = "workspace.destroyed"
	AgentStarted       Type = "agent.started"
	AgentStopped       Type = "agent.stopped"
	AgentMessage       Type = "agent.message"
	FileChanged        Type = "file.changed"
	ChangesetCreated   Type = "changeset.created"
	TestStarted        Type = "test.started"
	TestFailed         Type = "test.failed"
	TestPassed         Type = "test.passed"
	ConflictDetected   Type = "conflict.detected"
	ReviewRequested    Type = "review.requested"
	ApprovalGranted    Type = "approval.granted"
	ApprovalRejected   Type = "approval.rejected"
	MergeCompleted     Type = "merge.completed"
)

// ActorType distinguishes who caused an event.
type ActorType string

const (
	ActorHuman  ActorType = "human"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

// Event is the unit of auditability.
type Event struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id,omitempty"`
	ActorType ActorType      `json:"actor_type"`
	ActorID   string         `json:"actor_id,omitempty"`
	Type      Type           `json:"type"`
	EntityID  string         `json:"entity_id,omitempty"`
	At        time.Time      `json:"at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// New builds a validated event with fresh ID and timestamp.
func New(projectID string, actorType ActorType, actorID string, typ Type, entityID string, meta map[string]any) Event {
	return Event{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		ActorType: actorType,
		ActorID:   actorID,
		Type:      typ,
		EntityID:  entityID,
		At:        time.Now().UTC(),
		Metadata:  meta,
	}
}

// Store persists events (Postgres in prod, memory in tests).
type Store interface {
	Append(ctx context.Context, e Event) error
	List(ctx context.Context, projectID string, limit int) ([]Event, error)
}

// Bus publishes to subscribers and persists via Store.
type Bus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(projectID string) (<-chan Event, func())
}

// MemoryBus is the MVP transport: persisted + in-process fan-out.
// A NATSBus with the same interface replaces it later.
type MemoryBus struct {
	mu    sync.RWMutex
	subs  map[string]map[chan Event]struct{}
	store Store
}

// NewMemoryBus wires a store (may be nil for pure fan-out).
func NewMemoryBus(store Store) *MemoryBus {
	return &MemoryBus{subs: map[string]map[chan Event]struct{}{}, store: store}
}

func (b *MemoryBus) Publish(ctx context.Context, e Event) error {
	if b.store != nil {
		if err := b.store.Append(ctx, e); err != nil {
			return err
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[e.ProjectID] {
		select {
		case ch <- e:
		default: // slow consumer drops live signal; history stays in store
		}
	}
	for ch := range b.subs[""] {
		select {
		case ch <- e:
		default:
		}
	}
	return nil
}

func (b *MemoryBus) Subscribe(projectID string) (<-chan Event, func()) {
	ch := make(chan Event, 128)
	b.mu.Lock()
	if b.subs[projectID] == nil {
		b.subs[projectID] = map[chan Event]struct{}{}
	}
	b.subs[projectID][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs[projectID], ch)
		b.mu.Unlock()
	}
}

// MemoryStore keeps events in order for tests and dev fallback.
type MemoryStore struct {
	mu sync.Mutex
	ev []Event
}

// NewMemoryStore builds an empty event store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

func (m *MemoryStore) Append(_ context.Context, e Event) error {
	m.mu.Lock()
	m.ev = append(m.ev, e)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) List(_ context.Context, projectID string, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for i := len(m.ev) - 1; i >= 0 && len(out) < limit; i-- {
		if projectID == "" || m.ev[i].ProjectID == projectID {
			out = append(out, m.ev[i])
		}
	}
	return out, nil
}
