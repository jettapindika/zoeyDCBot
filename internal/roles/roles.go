// Package roles provides reaction-role management.
//
// A reaction-role message is a message the bot posts that users react to
// with specific emojis to get/lose roles. The mapping is stored in-memory
// keyed by message ID.
package roles

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// Binding maps a single emoji to a single role within a reaction-role message.
type Binding struct {
	Emoji  string // unicode emoji or custom emoji name (e.g. "🎮" or "gomu")
	RoleID string
}

// Message is a reaction-role message tracked by the Manager.
type Message struct {
	ChannelID string
	MessageID string
	Bindings  []Binding
}

// Manager tracks all reaction-role messages in-memory.
type Manager struct {
	mu       sync.RWMutex
	messages map[string]*Message // keyed by message ID
}

// New creates a Manager.
func New() *Manager {
	return &Manager{messages: make(map[string]*Message)}
}

// Register adds a reaction-role message to the manager.
func (m *Manager) Register(channelID, messageID string, bindings []Binding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[messageID] = &Message{
		ChannelID: channelID,
		MessageID: messageID,
		Bindings:  bindings,
	}
}

// Unregister removes a reaction-role message.
func (m *Manager) Unregister(messageID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.messages[messageID]; !ok {
		return false
	}
	delete(m.messages, messageID)
	return true
}

// Lookup returns the reaction-role message for a message ID, if any.
func (m *Manager) Lookup(messageID string) (*Message, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msg, ok := m.messages[messageID]
	return msg, ok
}

// RoleForEmoji returns the role ID bound to the given emoji on the message.
func (m *Message) RoleForEmoji(emoji string) (string, bool) {
	for _, b := range m.Bindings {
		if b.Emoji == emoji {
			return b.RoleID, true
		}
	}
	return "", false
}

// FormatBindings returns a human-readable list of emoji→role bindings.
func (m *Message) FormatBindings() string {
	var out string
	for _, b := range m.Bindings {
		out += fmt.Sprintf("%s → <@&%s>\n", b.Emoji, b.RoleID)
	}
	return out
}

// ResolveRoleName returns the role name for a role ID from the guild.
func ResolveRoleName(s *discordgo.Session, guildID, roleID string) string {
	role, err := s.State.Role(guildID, roleID)
	if err == nil && role != nil {
		return role.Name
	}
	return roleID
}
