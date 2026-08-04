package channelrename

import (
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	m := New(nil, 0)
	if m.debounceDelay <= 0 {
		t.Fatal("expected non-zero debounce delay")
	}
	if m.originalNames == nil {
		t.Fatal("expected initialized map")
	}
}

func TestForget(t *testing.T) {
	m := New(nil, 0)
	m.originalNames["guild1"] = "Music Room"
	m.Forget("guild1")
	if _, ok := m.originalNames["guild1"]; ok {
		t.Fatal("expected guild to be forgotten")
	}
}

func TestForgetNonExistent(t *testing.T) {
	m := New(nil, 0)
	// Should not panic on non-existent guild.
	m.Forget("nonexistent")
}

func TestConcurrentAccess(t *testing.T) {
	m := New(nil, 0)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Forget("guild1")
				m.Forget("guild2")
			}
		}()
	}
	wg.Wait()
}
