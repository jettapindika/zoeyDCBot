// Package voicestatus manages Discord's Voice Channel Status feature.
//
// Unlike channel renaming (which modifies the channel name and has a strict
// ~2-per-10-minutes rate limit), the Voice Channel Status endpoint
// (PUT /channels/{id}/voice-status) sets a text status that appears in the
// voice channel — ideal for showing the current track without touching the
// channel name.
//
// The status is cleared (set to empty string) when playback stops or the bot
// disconnects, so it never shows a stale song after the music ends.
package voicestatus

import (
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Manager sets and clears the voice channel status for the current track.
// It includes a debounce to avoid firing on every rapid skip.
type Manager struct {
	mu             sync.Mutex
	sess           *discordgo.Session
	currentGuild   string
	debounceTimer  *time.Timer
	debounceDelay  time.Duration
}

// New creates a Manager.
func New(sess *discordgo.Session, debounceMs int) *Manager {
	if debounceMs <= 0 {
		debounceMs = 2000
	}
	return &Manager{
		sess:          sess,
		debounceDelay: time.Duration(debounceMs) * time.Millisecond,
	}
}

// OnTrackStart schedules a debounced status update to show the track title.
// The status text is built from the resolved Track's title and artist,
// never from raw query text.
func (m *Manager) OnTrackStart(guildID, channelID, trackTitle, artist string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentGuild = guildID

	// Cancel any pending update.
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}

	// Build status text from resolved metadata.
	status := buildStatusText(trackTitle, artist)

	// Schedule the status update.
	m.debounceTimer = time.AfterFunc(m.debounceDelay, func() {
		m.setStatus(channelID, status)
	})
}

// Clear clears the voice channel status (sets it to empty string).
// Called when playback stops or the bot disconnects from voice.
func (m *Manager) Clear(guildID, channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}

	m.setStatus(channelID, "")
}

// Forget clears internal state for a guild (used when the bot leaves voice).
func (m *Manager) Forget(guildID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
	if m.currentGuild == guildID {
		m.currentGuild = ""
	}
}

// buildStatusText creates the status string from resolved track metadata.
// Never uses raw query text.
func buildStatusText(title, artist string) string {
	title = trim(title)
	if title == "" {
		return ""
	}
	artist = trim(artist)
	if artist != "" {
		s := "🎵 " + artist + " — " + title
		if len(s) > 500 {
			return s[:500]
		}
		return s
	}
	s := "🎵 " + title
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

// setStatus performs the actual REST call to set the voice channel status.
// Uses the dedicated PUT /channels/{id}/voice-status endpoint.
// Must NOT hold the mutex (it calls Discord REST).
//
// Discord's voice-status endpoint accepts {"status": "<text>"}.
// An empty string clears the status. The endpoint has its own rate limit
// bucket (distinct from channel rename) — we observe X-RateLimit headers
// via discordgo's built-in rate limiter.
func (m *Manager) setStatus(channelID, status string) {
	if m.sess == nil || channelID == "" {
		return
	}

	// The discordgo fork doesn't have a dedicated VoiceStatus method, so we
	// use the raw Request method with the voice-status endpoint.
	// Endpoint: PUT /channels/{channelID}/voice-status
	endpoint := discordgo.EndpointChannel(channelID) + "/voice-status"

	body := map[string]string{"status": status}

	_, err := m.sess.Request("PUT", endpoint, body)
	if err != nil {
		// Rate limit or permission error — silently ignore.
		// 403 would indicate the bot lacks the "Set Voice Channel Status"
		// permission; this is non-fatal.
		return
	}
}
