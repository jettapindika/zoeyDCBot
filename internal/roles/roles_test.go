package roles

import (
	"sync"
	"testing"
)

func TestRegisterAndLookup(t *testing.T) {
	m := New()
	bindings := []Binding{
		{Emoji: "🎮", RoleID: "111"},
		{Emoji: "🎵", RoleID: "222"},
	}
	m.Register("chan-1", "msg-1", bindings)

	msg, ok := m.Lookup("msg-1")
	if !ok {
		t.Fatal("expected to find registered message")
	}
	if len(msg.Bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(msg.Bindings))
	}

	roleID, ok := msg.RoleForEmoji("🎮")
	if !ok || roleID != "111" {
		t.Fatalf("expected role 111 for 🎮, got %q ok=%v", roleID, ok)
	}

	roleID, ok = msg.RoleForEmoji("nonexistent")
	if ok {
		t.Fatal("expected no role for unknown emoji")
	}
}

func TestUnregister(t *testing.T) {
	m := New()
	m.Register("chan-1", "msg-1", []Binding{{Emoji: "🎮", RoleID: "111"}})

	if !m.Unregister("msg-1") {
		t.Fatal("expected Unregister to return true for existing message")
	}
	if _, ok := m.Lookup("msg-1"); ok {
		t.Fatal("expected message to be gone after Unregister")
	}
	if m.Unregister("msg-1") {
		t.Fatal("expected Unregister to return false for non-existent message")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup

	// Concurrent registerers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				msgID := "msg-" + itoa(n) + "-" + itoa(j)
				m.Register("chan", msgID, []Binding{{Emoji: "🎮", RoleID: "role"}})
				m.Lookup(msgID)
				m.Unregister(msgID)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				m.Lookup("msg-0-0")
			}
		}()
	}

	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
