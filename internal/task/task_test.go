package task

import "testing"

func TestTransitions(t *testing.T) {
	x := New("p", "Backend", "api", []string{"src/api/*"})
	if x.Status != Todo {
		t.Fatalf("new task = %q", x.Status)
	}
	if !x.Transition(Running) || x.Status != Running {
		t.Fatal("TODO → RUNNING must hold")
	}
	if x.Transition(Done) {
		t.Fatal("RUNNING → DONE must be rejected (via REVIEW)")
	}
	if !x.Transition(Review) || !x.Transition(Done) {
		t.Fatal("RUNNING → REVIEW → DONE must hold")
	}
	if x.Transition(Todo) {
		t.Fatal("DONE is terminal")
	}
}
