package events

import (
	"context"
	"testing"
)

func TestPersistAndFanout(t *testing.T) {
	ctx := context.Background()
	st := &MemoryStore{}
	bus := NewMemoryBus(st)
	ch, unsub := bus.Subscribe("p")
	defer unsub()
	e := New("p", ActorHuman, "u1", TaskCreated, "t1", map[string]any{"title": "Backend"})
	if err := bus.Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got.Type != TaskCreated || got.EntityID != "t1" {
			t.Fatalf("wrong event: %+v", got)
		}
	default:
		t.Fatal("subscriber got nothing")
	}
	list, err := st.List(ctx, "p", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("persisted = %v %v", list, err)
	}
}
