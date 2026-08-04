package presence

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// fakeSession lets us capture UpdateStatusComplex calls without a real gateway.
type fakeSession struct {
	called  bool
	updates []discordgo.UpdateStatusData
}

func TestSetNowPlaying(t *testing.T) {
	m := New(nil, "")
	m.sess = &discordgo.Session{}
	// We can't easily test the actual send without a real session,
	// but we can verify the internal state is set correctly.
	m.SetNowPlaying("Bohemian Rhapsody", "Queen")
	if m.current == nil {
		t.Fatal("expected current activity to be set")
	}
	if m.current.Name != "Bohemian Rhapsody" {
		t.Fatalf("expected name 'Bohemian Rhapsody', got %q", m.current.Name)
	}
	if m.current.Type != discordgo.ActivityTypeListening {
		t.Fatalf("expected Listening type, got %d", m.current.Type)
	}
}

func TestSetIdle(t *testing.T) {
	m := New(nil, "/help for commands")
	m.SetIdle()
	if m.current == nil {
		t.Fatal("expected idle activity to be set")
	}
	if m.current.Name != "/help for commands" {
		t.Fatalf("expected idle message, got %q", m.current.Name)
	}
}

func TestClear(t *testing.T) {
	m := New(nil, "")
	m.SetNowPlaying("Song", "Artist")
	m.Clear()
	if m.current != nil {
		t.Fatal("expected current to be nil after Clear")
	}
}

func TestDefaultIdleMsg(t *testing.T) {
	m := New(nil, "")
	if m.idleMsg != "/help for commands" {
		t.Fatalf("expected default idle msg, got %q", m.idleMsg)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New(nil, "")
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				m.SetNowPlaying("Song", "Artist")
				m.SetIdle()
				m.Clear()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
