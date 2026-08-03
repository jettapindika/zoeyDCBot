// Package music implements a concurrency-safe per-guild music queue. The
// Discord voice transport is driven by bot handlers; this package owns state.
package music

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrQueueFull = errors.New("music queue is full")

// Track is a requested audio item. Query may be a URL or plain search query.
type Track struct {
	Title string
	Query string
	URL   string
	// StreamURL is the direct audio URL when the track was resolved ahead of
	// time. Set means playback can skip the yt-dlp resolve step.
	StreamURL       string
	Artist          string
	Thumbnail       string
	Duration        float64 // seconds
	RequestedBy     string
	RequestedByName string
	QueuedAt        time.Time
	// Source is a friendly provider label ("YouTube", "SoundCloud", "Spotify").
	Source string
	// WebpageURL is the human-visitable page for this track, used to make
	// embed titles clickable.
	WebpageURL string
	// PlaylistName is set when the track came from an expanded playlist, so
	// embeds can show where it originated.
	PlaylistName string
}

// Display returns the title, prefixed by the artist when that adds information.
func (t Track) Display() string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = strings.TrimSpace(t.Query)
	}
	artist := strings.TrimSpace(t.Artist)
	if artist == "" || strings.Contains(strings.ToLower(title), strings.ToLower(artist)) {
		return truncate(title, 90)
	}
	return truncate(artist+" — "+title, 90)
}

// MarkdownLink renders the track as a markdown link when a page URL is known.
func (t Track) MarkdownLink() string {
	name := t.Display()
	if t.WebpageURL == "" {
		return name
	}
	// Escape closing brackets so the markdown link cannot be broken by a title.
	safe := strings.NewReplacer("[", "(", "]", ")").Replace(name)
	return "[" + safe + "](" + t.WebpageURL + ")"
}

// truncate clamps a string to n runes, appending "…" if it was shortened.
func truncate(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// GuildPlayer holds one guild's queue state.
type GuildPlayer struct {
	Queue      []Track
	NowPlaying *Track
	Paused     bool
}

// Manager stores all guild queues.
type Manager struct {
	mu       sync.Mutex
	guilds   map[string]*GuildPlayer
	maxQueue int
}

func NewManager(maxQueue int) *Manager {
	if maxQueue < 1 {
		maxQueue = 1
	}
	return &Manager{guilds: make(map[string]*GuildPlayer), maxQueue: maxQueue}
}

func (m *Manager) player(guildID string) *GuildPlayer {
	p, ok := m.guilds[guildID]
	if !ok {
		p = &GuildPlayer{}
		m.guilds[guildID] = p
	}
	return p
}

func (m *Manager) Add(guildID string, t Track) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	if len(p.Queue) >= m.maxQueue {
		return len(p.Queue), ErrQueueFull
	}
	if strings.TrimSpace(t.Title) == "" {
		t.Title = strings.TrimSpace(t.Query)
	}
	if t.QueuedAt.IsZero() {
		t.QueuedAt = time.Now()
	}
	p.Queue = append(p.Queue, t)
	return len(p.Queue), nil
}

// AddMany appends as many tracks as the queue can hold. It returns how many
// were accepted and the queue position of the first accepted track. A playlist
// that overflows the cap is truncated rather than rejected outright, so the
// caller can report "added 40 of 120".
func (m *Manager) AddMany(guildID string, tracks []Track) (added int, firstPos int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	firstPos = len(p.Queue) + 1
	now := time.Now()
	for _, t := range tracks {
		if len(p.Queue) >= m.maxQueue {
			break
		}
		if strings.TrimSpace(t.Title) == "" {
			t.Title = strings.TrimSpace(t.Query)
		}
		if t.QueuedAt.IsZero() {
			t.QueuedAt = now
		}
		p.Queue = append(p.Queue, t)
		added++
	}
	return added, firstPos
}

// Len returns the number of queued tracks (excluding now-playing).
func (m *Manager) Len(guildID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.player(guildID).Queue)
}

// Remaining returns how many more tracks fit in the queue.
func (m *Manager) Remaining(guildID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.maxQueue - len(m.player(guildID).Queue)
	if n < 0 {
		return 0
	}
	return n
}

// PeekNext returns a copy of the next queued track without popping it.
func (m *Manager) PeekNext(guildID string) (Track, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	if len(p.Queue) == 0 {
		return Track{}, false
	}
	return p.Queue[0], true
}

func (m *Manager) StartNext(guildID string) (*Track, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	if len(p.Queue) == 0 {
		p.NowPlaying = nil
		p.Paused = false
		return nil, false
	}
	t := p.Queue[0]
	p.Queue = p.Queue[1:]
	p.NowPlaying = &t
	p.Paused = false
	return &t, true
}

func (m *Manager) Skip(guildID string) (*Track, bool) { return m.StartNext(guildID) }

func (m *Manager) Stop(guildID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	n := len(p.Queue) + 1 // +1 for now-playing
	p.Queue = nil
	p.NowPlaying = nil
	p.Paused = false
	return n
}

func (m *Manager) Pause(guildID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	if p.NowPlaying == nil {
		return false
	}
	p.Paused = true
	return true
}

func (m *Manager) Resume(guildID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	if p.NowPlaying == nil {
		return false
	}
	p.Paused = false
	return true
}

func (m *Manager) Queue(guildID string) ([]Track, *Track, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	// Return a copy so callers can't mutate the internal slice
	q := make([]Track, len(p.Queue))
	copy(q, p.Queue)
	return q, p.NowPlaying, p.Paused
}

func (m *Manager) IsEmpty(guildID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	return len(p.Queue) == 0 && p.NowPlaying == nil
}

func (m *Manager) HasNext(guildID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	return len(p.Queue) > 0
}

func (m *Manager) IsPlaying(guildID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	return p.NowPlaying != nil
}

func (m *Manager) IsPaused(guildID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	return p.NowPlaying != nil && p.Paused
}

func (m *Manager) SetNowPlaying(guildID string, t *Track) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	p.NowPlaying = t
	p.Paused = false
}

func (m *Manager) ClearNowPlaying(guildID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	p.NowPlaying = nil
	p.Paused = false
}

// UpdateNowPlaying applies a patch function to the currently playing track.
// Used to write back resolved metadata (stream URL, artist, duration, …)
// after playback starts. No-op if nothing is playing.
func (m *Manager) UpdateNowPlaying(guildID string, patch func(*Track)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.player(guildID)
	if p.NowPlaying != nil {
		patch(p.NowPlaying)
	}
}

// FormatQueue renders the queue as a Discord-friendly string, including each
// track's duration and the total expected runtime. It caps the output to stay
// well under Discord's 4096-char embed description limit.
func FormatQueue(q []Track, now *Track, paused bool) string {
	var sb strings.Builder
	if now != nil {
		status := "▶️"
		if paused {
			status = "⏸️"
		}
		sb.WriteString(fmt.Sprintf("%s **Now Playing:** %s `[%s]`\n\n",
			status, now.MarkdownLink(), FormatDuration(now.Duration)))
	}
	if len(q) == 0 {
		if now == nil {
			sb.WriteString("📭 The queue is empty.")
		}
		return sb.String()
	}
	sb.WriteString("**Up Next:**\n")
	// Show at most 10 entries to stay well under the 4096-char description cap.
	const maxShown = 10
	var eta float64
	if now != nil {
		eta = now.Duration
	}
	for i, t := range q {
		if i >= maxShown {
			sb.WriteString(fmt.Sprintf("…and **%d** more\n", len(q)-maxShown))
			break
		}
		line := fmt.Sprintf("`%2d.` %s `[%s]`", i+1, t.MarkdownLink(), FormatDuration(t.Duration))
		if eta > 0 {
			line += fmt.Sprintf(" · starts in ~%s", FormatDuration(eta))
		}
		sb.WriteString(line + "\n")
		eta += t.Duration
	}
	total := TotalDuration(q, now)
	sb.WriteString(fmt.Sprintf("\n**%d** in queue · total length **%s**", len(q), FormatDuration(total)))
	// Safety: if we somehow exceeded the limit, truncate.
	result := sb.String()
	const maxLen = 4000
	if len(result) > maxLen {
		result = result[:maxLen-3] + "…"
	}
	return result
}

// FormatDuration renders seconds as m:ss, or h:mm:ss when an hour or longer.
// Zero or negative durations render as "?:??" since we genuinely don't know.
func FormatDuration(seconds float64) string {
	if seconds <= 0 {
		return "?:??"
	}
	total := int(seconds + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// TotalDuration sums the durations of the queued tracks plus now-playing.
func TotalDuration(q []Track, now *Track) float64 {
	var total float64
	if now != nil {
		total += now.Duration
	}
	for _, t := range q {
		total += t.Duration
	}
	return total
}
