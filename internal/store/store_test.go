package store

import (
	"os"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := "test-" + t.Name() + ".db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		os.Remove(path)
		os.Remove(path + "-wal")
		os.Remove(path + "-shm")
	})
	return s
}

func TestSaveAndLoadHistory(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveMessage("chan1", "user", "hello"); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if err := s.SaveMessage("chan1", "assistant", "hi there"); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if err := s.SaveMessage("chan2", "user", "other channel"); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	msgs, err := s.LoadHistory("chan1", 10)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// Should be chronological (oldest first).
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("first msg = %s/%s, want user/hello", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
		t.Fatalf("second msg = %s/%s, want assistant/hi there", msgs[1].Role, msgs[1].Content)
	}
}

func TestClearHistory(t *testing.T) {
	s := newTestStore(t)
	s.SaveMessage("chan1", "user", "a")
	s.SaveMessage("chan1", "user", "b")

	n, err := s.ClearHistory("chan1")
	if err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}

	msgs, _ := s.LoadHistory("chan1", 10)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after clear, got %d", len(msgs))
	}
}

func TestSaveAndLoadQueue(t *testing.T) {
	s := newTestStore(t)

	tracks := []QueuedTrack{
		{Title: "Song A", URL: "url-a", Author: "Artist", Duration: "3:00", Source: "youtube", AddedBy: "user1"},
		{Title: "Song B", URL: "url-b", Author: "Band", Duration: "4:00", Source: "soundcloud", AddedBy: "user2"},
	}

	if err := s.SaveQueue("guild1", tracks); err != nil {
		t.Fatalf("SaveQueue: %v", err)
	}

	loaded, err := s.LoadQueue("guild1")
	if err != nil {
		t.Fatalf("LoadQueue: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(loaded))
	}
	if loaded[0].Title != "Song A" || loaded[1].Title != "Song B" {
		t.Fatalf("titles = %q, %q", loaded[0].Title, loaded[1].Title)
	}
}

func TestSaveQueueReplaces(t *testing.T) {
	s := newTestStore(t)

	s.SaveQueue("guild1", []QueuedTrack{{Title: "Old", URL: "old-url", Duration: "1:00"}})
	s.SaveQueue("guild1", []QueuedTrack{{Title: "New", URL: "new-url", Duration: "2:00"}})

	loaded, _ := s.LoadQueue("guild1")
	if len(loaded) != 1 {
		t.Fatalf("expected 1 track after replace, got %d", len(loaded))
	}
	if loaded[0].Title != "New" {
		t.Fatalf("expected 'New', got %q", loaded[0].Title)
	}
}

func TestSettings(t *testing.T) {
	s := newTestStore(t)

	val, err := s.GetSetting("nonexistent")
	if err != nil {
		t.Fatalf("GetSetting nonexistent: %v", err)
	}
	if val != "" {
		t.Fatalf("expected empty for nonexistent key, got %q", val)
	}

	if err := s.SetSetting("key1", "value1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	val, _ = s.GetSetting("key1")
	if val != "value1" {
		t.Fatalf("expected value1, got %q", val)
	}

	// Update existing.
	s.SetSetting("key1", "value2")
	val, _ = s.GetSetting("key1")
	if val != "value2" {
		t.Fatalf("expected value2 after update, got %q", val)
	}
}
