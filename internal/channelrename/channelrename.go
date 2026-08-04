// Package channelrename automatically renames the voice channel the bot is
// in to display the current track name. It includes a debounce to avoid
// hitting Discord's rate limit on channel edits.
package channelrename

import (
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Manager tracks the original voice channel name and handles debounced renames.
type Manager struct {
	mu             sync.Mutex
	sess           *discordgo.Session
	originalNames  map[string]string // guildID -> original channel name
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
		originalNames: make(map[string]string),
		debounceDelay: time.Duration(debounceMs) * time.Millisecond,
	}
}

// OnTrackStart records the original channel name and schedules a rename
// to the track title. The rename is debounced to avoid rate limits.
func (m *Manager) OnTrackStart(guildID, channelID, trackTitle string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store original name if we don't have it yet.
	if _, ok := m.originalNames[guildID]; !ok {
		ch, err := m.sess.State.Channel(channelID)
		if err != nil {
			// Try REST API as fallback.
			ch, err = m.sess.Channel(channelID)
			if err != nil {
				return
			}
		}
		if ch != nil {
			m.originalNames[guildID] = ch.Name
		}
	}

	m.currentGuild = guildID

	// Cancel any pending rename.
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}

	// Schedule the rename.
	m.debounceTimer = time.AfterFunc(m.debounceDelay, func() {
		m.doRename(guildID, channelID, trackTitle)
	})
}

// Restore renames the channel back to its original name.
func (m *Manager) Restore(guildID, channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}

	origName, ok := m.originalNames[guildID]
	if !ok {
		return // nothing to restore
	}

	m.doRename(guildID, channelID, origName)
	delete(m.originalNames, guildID)
}

// doRename performs the actual channel rename. Called from debounce timer
// or Restore. Must NOT hold the mutex (it calls Discord REST).
func (m *Manager) doRename(guildID, channelID, name string) {
	// Clamp name to 100 chars (Discord channel name limit).
	if len(name) > 100 {
		name = name[:100]
	}

	_, err := m.sess.ChannelEditComplex(channelID, &discordgo.ChannelEdit{
		Name: name,
	})
	if err != nil {
		// Rate limit or permission error — silently ignore.
		return
	}
}

// Forget clears the stored original name for a guild (used when the bot
// leaves voice).
func (m *Manager) Forget(guildID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
	delete(m.originalNames, guildID)
}
