package music

import "testing"

func TestQueueLifecycle(t *testing.T) {
	m := NewManager(2)
	if n, err := m.Add("g1", Track{Title: "one"}); err != nil || n != 1 {
		t.Fatalf("add one n=%d err=%v", n, err)
	}
	_, _ = m.Add("g1", Track{Title: "two"})
	if _, err := m.Add("g1", Track{Title: "three"}); err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	now, ok := m.StartNext("g1")
	if !ok || now.Title != "one" {
		t.Fatalf("now=%+v ok=%v", now, ok)
	}
	q, current, _ := m.Queue("g1")
	if len(q) != 1 || current == nil || current.Title != "one" {
		t.Fatalf("queue=%+v current=%+v", q, current)
	}
	if stopped := m.Stop("g1"); stopped != 2 {
		t.Fatalf("stopped=%d want 2", stopped)
	}
}

func TestGuildQueuesIsolated(t *testing.T) {
	m := NewManager(10)
	_, _ = m.Add("g1", Track{Title: "one"})
	_, _ = m.Add("g2", Track{Title: "two"})
	q1, _, _ := m.Queue("g1")
	q2, _, _ := m.Queue("g2")
	if q1[0].Title != "one" || q2[0].Title != "two" {
		t.Fatalf("queues not isolated: %+v %+v", q1, q2)
	}
}

func TestEmptyQueueStartNext(t *testing.T) {
	m := NewManager(10)
	track, ok := m.StartNext("nonexistent")
	if ok || track != nil {
		t.Fatalf("expected nil/false for empty queue, got track=%+v ok=%v", track, ok)
	}
}

func TestPauseResume(t *testing.T) {
	m := NewManager(10)
	_, _ = m.Add("g1", Track{Title: "song"})
	m.StartNext("g1")

	// Pause should work when playing
	if !m.Pause("g1") {
		t.Fatal("pause should return true when something is playing")
	}
	if !m.IsPaused("g1") {
		t.Fatal("should be paused after pause")
	}

	// Resume should work
	if !m.Resume("g1") {
		t.Fatal("resume should return true when something is playing")
	}
	if m.IsPaused("g1") {
		t.Fatal("should not be paused after resume")
	}
}

func TestPauseWhenNotPlaying(t *testing.T) {
	m := NewManager(10)
	if m.Pause("g1") {
		t.Fatal("pause should return false when nothing is playing")
	}
	if m.Resume("g1") {
		t.Fatal("resume should return false when nothing is playing")
	}
}

func TestStopClearsEverything(t *testing.T) {
	m := NewManager(10)
	_, _ = m.Add("g1", Track{Title: "one"})
	_, _ = m.Add("g1", Track{Title: "two"})
	m.StartNext("g1") // now playing "one", queue has "two"

	stopped := m.Stop("g1")
	if stopped != 2 {
		t.Fatalf("stopped=%d, want 2 (1 now-playing + 1 queued)", stopped)
	}

	if m.IsPlaying("g1") {
		t.Fatal("should not be playing after stop")
	}
	if m.HasNext("g1") {
		t.Fatal("should have no next after stop")
	}
	q, now, paused := m.Queue("g1")
	if len(q) != 0 || now != nil || paused {
		t.Fatalf("queue not empty after stop: q=%+v now=%+v paused=%v", q, now, paused)
	}
}

func TestIsEmpty(t *testing.T) {
	m := NewManager(10)
	if !m.IsEmpty("g1") {
		t.Fatal("should be empty initially")
	}
	_, _ = m.Add("g1", Track{Title: "one"})
	if m.IsEmpty("g1") {
		t.Fatal("should not be empty after adding track")
	}
}

func TestHasNext(t *testing.T) {
	m := NewManager(10)
	if m.HasNext("g1") {
		t.Fatal("should have no next initially")
	}
	_, _ = m.Add("g1", Track{Title: "one"})
	if !m.HasNext("g1") {
		t.Fatal("should have next after adding")
	}
	m.StartNext("g1")
	if m.HasNext("g1") {
		t.Fatal("should have no next after starting (queue empty)")
	}
}

func TestIsPlaying(t *testing.T) {
	m := NewManager(10)
	if m.IsPlaying("g1") {
		t.Fatal("should not be playing initially")
	}
	_, _ = m.Add("g1", Track{Title: "one"})
	m.StartNext("g1")
	if !m.IsPlaying("g1") {
		t.Fatal("should be playing after StartNext")
	}
}

func TestSetNowPlaying(t *testing.T) {
	m := NewManager(10)
	track := Track{Title: "custom track"}
	m.SetNowPlaying("g1", &track)
	if !m.IsPlaying("g1") {
		t.Fatal("should be playing after SetNowPlaying")
	}
	m.ClearNowPlaying("g1")
	if m.IsPlaying("g1") {
		t.Fatal("should not be playing after ClearNowPlaying")
	}
}

func TestFormatQueueEmpty(t *testing.T) {
	result := FormatQueue(nil, nil, false, 1)
	if result != "📭 The queue is empty." {
		t.Fatalf("unexpected format for empty queue: %q", result)
	}
}

func TestFormatQueueWithNowPlaying(t *testing.T) {
	now := &Track{Title: "My Song"}
	result := FormatQueue(nil, now, false, 1)
	if result == "" {
		t.Fatal("expected non-empty format")
	}
}

func TestFormatQueueWithNowPlayingPaused(t *testing.T) {
	now := &Track{Title: "My Song"}
	result := FormatQueue(nil, now, true, 1)
	if result == "" {
		t.Fatal("expected non-empty format for paused")
	}
}

func TestFormatQueueWithItems(t *testing.T) {
	q := []Track{
		{Title: "Song A"},
		{Title: "Song B"},
	}
	result := FormatQueue(q, nil, false, 1)
	if result == "" {
		t.Fatal("expected non-empty format")
	}
}

func TestQueueCopyNotMutable(t *testing.T) {
	m := NewManager(10)
	_, _ = m.Add("g1", Track{Title: "original"})
	q, _, _ := m.Queue("g1")
	q[0] = Track{Title: "mutated"}
	// Internal queue should not be affected
	q2, _, _ := m.Queue("g1")
	if q2[0].Title != "original" {
		t.Fatalf("internal queue was mutated: %q", q2[0].Title)
	}
}

func TestNewManagerClampsMaxQueue(t *testing.T) {
	m := NewManager(0)
	if _, err := m.Add("g1", Track{Title: "one"}); err != nil {
		t.Fatalf("maxQueue=0 should clamp to 1, got err: %v", err)
	}
	if _, err := m.Add("g1", Track{Title: "two"}); err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull with clamped maxQueue=1, got %v", err)
	}
}

func TestAddFillsTitleFromQuery(t *testing.T) {
	m := NewManager(10)
	_, _ = m.Add("g1", Track{Query: "some search query"})
	q, _, _ := m.Queue("g1")
	if q[0].Title != "some search query" {
		t.Fatalf("expected title to be filled from query, got %q", q[0].Title)
	}
}

func TestAddSetsQueuedAt(t *testing.T) {
	m := NewManager(10)
	_, _ = m.Add("g1", Track{Title: "test"})
	q, _, _ := m.Queue("g1")
	if q[0].QueuedAt.IsZero() {
		t.Fatal("expected QueuedAt to be set")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0, "?:??"},
		{-5, "?:??"},
		{5, "0:05"},
		{65, "1:05"},
		{241.418, "4:01"},
		{3600, "1:00:00"},
		{3661, "1:01:01"},
		{2320.869, "38:41"},
	}
	for _, tc := range cases {
		if got := FormatDuration(tc.seconds); got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestTotalDurationSumsQueueAndNowPlaying(t *testing.T) {
	now := &Track{Title: "now", Duration: 100}
	q := []Track{{Title: "a", Duration: 200}, {Title: "b", Duration: 300}}
	if got := TotalDuration(q, now); got != 600 {
		t.Errorf("TotalDuration = %v, want 600", got)
	}
	if got := TotalDuration(nil, nil); got != 0 {
		t.Errorf("TotalDuration(empty) = %v, want 0", got)
	}
	if got := TotalDuration(q, nil); got != 500 {
		t.Errorf("TotalDuration(no now) = %v, want 500", got)
	}
}
