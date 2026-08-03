package memory

import (
	"testing"
	"time"
)

func TestAppendAndTrim(t *testing.T) {
	s := NewStore(4, time.Hour)
	h := s.Append("c1", "user", "hello")
	if len(h) != 1 || h[0].Role != "user" {
		t.Fatalf("after 1 append: %+v", h)
	}
	for i := 0; i < 5; i++ {
		h = s.Append("c1", "assistant", "reply")
		h = s.Append("c1", "user", "q")
	}
	// 6 pairs appended = 12 turns, max 4 → keep last 4 (2 pairs).
	if len(h) != 4 {
		t.Fatalf("len = %d, want 4", len(h))
	}
	if h[0].Role != "assistant" || h[1].Role != "user" {
		t.Fatalf("trim broke pair alignment: %+v", h)
	}
}

func TestClear(t *testing.T) {
	s := NewStore(10, time.Hour)
	s.Append("c1", "user", "hi")
	if n := s.Clear("c1"); n != 1 {
		t.Fatalf("cleared %d, want 1", n)
	}
	if h := s.History("c1"); len(h) != 0 {
		t.Fatalf("history after clear = %d, want 0", len(h))
	}
}

func TestTTLEviction(t *testing.T) {
	s := NewStore(10, 50*time.Millisecond)
	s.Append("c1", "user", "hi")
	time.Sleep(80 * time.Millisecond)
	// A new append triggers eviction; c1 must be gone.
	s.Append("c2", "user", "yo")
	if h := s.History("c1"); len(h) != 0 {
		t.Fatalf("c1 not evicted: %+v", h)
	}
}
