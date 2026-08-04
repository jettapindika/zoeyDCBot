package starboard

import (
	"sync"
	"testing"
)

func TestUpdateStarCount(t *testing.T) {
	e := New(3, "⭐")

	entry := e.UpdateStarCount("chan", "msg", "user1", "hello", 1)
	if entry.StarCount != 1 {
		t.Fatalf("expected 1 star, got %d", entry.StarCount)
	}
	if e.ShouldRepost(entry) {
		t.Fatal("should not repost with 1 star (threshold 3)")
	}

	entry = e.UpdateStarCount("chan", "msg", "user1", "hello", 3)
	if !e.ShouldRepost(entry) {
		t.Fatal("should repost with 3 stars (threshold 3)")
	}
}

func TestGetEntry(t *testing.T) {
	e := New(3, "⭐")
	e.UpdateStarCount("chan", "msg", "user1", "hello", 5)

	entry, ok := e.GetEntry("chan", "msg")
	if !ok {
		t.Fatal("expected to find entry")
	}
	if entry.StarCount != 5 {
		t.Fatalf("expected 5 stars, got %d", entry.StarCount)
	}
	if entry.AuthorID != "user1" {
		t.Fatalf("expected author user1, got %s", entry.AuthorID)
	}
	if entry.Content != "hello" {
		t.Fatalf("expected content 'hello', got %s", entry.Content)
	}
}

func TestRemoveEntry(t *testing.T) {
	e := New(3, "⭐")
	e.UpdateStarCount("chan", "msg", "user1", "hello", 5)

	if !e.RemoveEntry("chan", "msg") {
		t.Fatal("expected RemoveEntry to return true")
	}
	if _, ok := e.GetEntry("chan", "msg"); ok {
		t.Fatal("expected entry to be gone")
	}
	if e.RemoveEntry("chan", "msg") {
		t.Fatal("expected RemoveEntry to return false for non-existent")
	}
}

func TestDefaults(t *testing.T) {
	e := New(0, "")
	if e.Threshold() != 3 {
		t.Fatalf("default threshold should be 3, got %d", e.Threshold())
	}
	if e.Emoji() != "⭐" {
		t.Fatalf("default emoji should be ⭐, got %s", e.Emoji())
	}
}

func TestConcurrentAccess(t *testing.T) {
	e := New(3, "⭐")
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ch := "chan-" + itoa(n)
				msg := "msg-" + itoa(j)
				e.UpdateStarCount(ch, msg, "user", "content", j)
				e.GetEntry(ch, msg)
				e.RemoveEntry(ch, msg)
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				e.GetEntry("chan-0", "msg-0")
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
