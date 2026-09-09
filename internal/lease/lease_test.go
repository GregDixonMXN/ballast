package lease

import "testing"

func TestOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"src/auth/*", "src/auth/login.go", true},
		{"src/auth/*", "src/payments/*", false},
		{"src/**", "src/auth/x.go", true},
		{"a/b", "a/b", true},
		{"**", "anything/at/all", true},
		{"src/api/*", "src/web/*", false},
	}
	for _, c := range cases {
		if got := Overlaps(c.a, c.b); got != c.want {
			t.Errorf("Overlaps(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestAcquireWarns(t *testing.T) {
	m := &Manager{}
	_, o1 := m.Acquire("p", "task-a", "src/auth/*")
	if len(o1) != 0 {
		t.Fatal("first claim should not overlap")
	}
	_, o2 := m.Acquire("p", "task-b", "src/auth/login.go")
	if len(o2) != 1 {
		t.Fatalf("expected overlap warning, got %v", o2)
	}
	m.Release("task-a")
	m.Release("task-b")
	_, o3 := m.Acquire("p", "task-c", "src/auth/*")
	if len(o3) != 0 {
		t.Fatalf("released leases should not warn, got %v", o3)
	}
}
