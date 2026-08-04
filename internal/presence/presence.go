// Package presence manages the bot's Discord rich presence status.
//
// When music is playing, the bot shows a "Listening to" activity with the
// track title and artist. When idle, it shows a configurable idle status.
package presence

import (
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Manager tracks the current presence state and updates Discord.
type Manager struct {
	mu      sync.Mutex
	sess    *discordgo.Session
	current *discordgo.Activity
	idleMsg string
}

// New creates a Manager.
func New(sess *discordgo.Session, idleMsg string) *Manager {
	if idleMsg == "" {
		idleMsg = "/help for commands"
	}
	return &Manager{sess: sess, idleMsg: idleMsg}
}

// SetNowPlaying updates the bot's status to show the current track.
func (m *Manager) SetNowPlaying(title, artist string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	activity := &discordgo.Activity{
		Name:  title,
		Type:  discordgo.ActivityTypeListening,
		State: artist,
		Details: title,
		Timestamps: discordgo.TimeStamps{
			StartTimestamp: time.Now().Unix(),
		},
	}
	m.current = activity
	m.send()
}

// SetIdle clears the now-playing status and shows the idle message.
func (m *Manager) SetIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.current = &discordgo.Activity{
		Name: m.idleMsg,
		Type: discordgo.ActivityTypeListening,
	}
	m.send()
}

// Clear clears all activities (bot shows no status).
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = nil
	m.send()
}

// send pushes the current activity to Discord.
func (m *Manager) send() {
	if m.sess == nil {
		return
	}
	var activities []*discordgo.Activity
	if m.current != nil {
		activities = append(activities, m.current)
	}
	_ = m.sess.UpdateStatusComplex(discordgo.UpdateStatusData{
		Status:     "online",
		Activities: activities,
	})
}
