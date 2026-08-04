package voicestatus

import (
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	m := New(nil, 0)
	if m.debounceDelay <= 0 {
		t.Fatal("expected non-zero debounce delay")
	}
}

func TestNewCustomDebounce(t *testing.T) {
	m := New(nil, 5000)
	if m.debounceDelay != 5000000000 { // 5s in nanoseconds
		t.Errorf("expected 5s debounce, got %v", m.debounceDelay)
	}
}

func TestBuildStatusText(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		artist  string
		want    string
	}{
		{"title and artist", "Bohemian Rhapsody", "Queen", "🎵 Queen — Bohemian Rhapsody"},
		{"title only", "Unknown Track", "", "🎵 Unknown Track"},
		{"artist only", "", "Some Artist", ""},
		{"both empty", "", "", ""},
		{"with whitespace", "  Track  ", "  Artist  ", "🎵 Artist — Track"},
		{"long title truncation", repeatStr("a", 600), "Artist", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildStatusText(tc.title, tc.artist)
			if tc.name == "long title truncation" {
				if len(got) > 500 {
					t.Errorf("status text length = %d, should be <= 500", len(got))
				}
				return
			}
			if got != tc.want {
				t.Errorf("buildStatusText(%q, %q) = %q, want %q", tc.title, tc.artist, got, tc.want)
			}
		})
	}
}

func TestClear(t *testing.T) {
	m := New(nil, 0)
	// Should not panic with nil session
	m.Clear("guild1", "channel1")
}

func TestForget(t *testing.T) {
	m := New(nil, 0)
	m.currentGuild = "guild1"
	m.Forget("guild1")
	if m.currentGuild != "" {
		t.Fatal("expected currentGuild to be cleared after Forget")
	}
}

func TestForgetNonExistent(t *testing.T) {
	m := New(nil, 0)
	m.Forget("nonexistent") // should not panic
}

func TestConcurrentAccess(t *testing.T) {
	m := New(nil, 0)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Clear("guild1", "channel1")
				m.Forget("guild1")
			}
		}()
	}
	wg.Wait()
}

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
