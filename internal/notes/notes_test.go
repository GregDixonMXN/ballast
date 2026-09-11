package notes

import (
	"testing"
)

func TestPostListNewestFirst(t *testing.T) {
	b := New()
	b.Post("p", "w1", "first")
	b.Post("p", "w2", "second")
	l := b.List("p")
	if len(l) != 2 || l[0].Text != "second" || l[1].Text != "first" {
		t.Fatalf("order = %+v", l)
	}
	if len(b.List("other")) != 0 {
		t.Fatal("projects must not leak")
	}
}

func TestCapAndTrim(t *testing.T) {
	b := New()
	long := make([]byte, MaxText+100)
	for i := range long {
		long[i] = 'z'
	}
	n := b.Post("p", "w", string(long))
	if len(n.Text) > MaxText+20 {
		t.Fatalf("not trimmed: %d", len(n.Text))
	}
	for i := 0; i < MaxPerProject+10; i++ {
		b.Post("p", "w", "x")
	}
	if len(b.List("p")) != MaxPerProject {
		t.Fatalf("cap = %d", len(b.List("p")))
	}
}
