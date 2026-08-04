// Package starboard provides a starboard engine.
//
// A starboard is a channel where messages that receive enough ⭐ reactions
// are reposted. The engine tracks starred messages in-memory.
package starboard

import (
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Entry tracks a starred message and its starboard repost.
type Entry struct {
	OriginalChannelID string
	OriginalMessageID string
	AuthorID          string
	Content           string
	StarCount         int
	StarboardMessageID string // empty until first repost
	RepostedAt        time.Time
}

// Engine manages starboard state in-memory.
type Engine struct {
	mu      sync.RWMutex
	entries map[string]*Entry // keyed by "channelID:messageID"
	threshold int
	starEmoji string
}

// New creates an Engine with the given star threshold and emoji.
func New(threshold int, emoji string) *Engine {
	if threshold <= 0 {
		threshold = 3
	}
	if emoji == "" {
		emoji = "⭐"
	}
	return &Engine{
		entries:   make(map[string]*Entry),
		threshold: threshold,
		starEmoji: emoji,
	}
}

// Threshold returns the star threshold.
func (e *Engine) Threshold() int { return e.threshold }

// Emoji returns the star emoji.
func (e *Engine) Emoji() string { return e.starEmoji }

// Key returns the canonical key for a message.
func Key(channelID, messageID string) string {
	return channelID + ":" + messageID
}

// GetEntry returns the entry for a message, if any.
func (e *Engine) GetEntry(channelID, messageID string) (*Entry, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	entry, ok := e.entries[Key(channelID, messageID)]
	return entry, ok
}

// SetEntry stores or updates an entry.
func (e *Engine) SetEntry(entry *Entry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.entries[Key(entry.OriginalChannelID, entry.OriginalMessageID)] = entry
}

// RemoveEntry removes an entry.
func (e *Engine) RemoveEntry(channelID, messageID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := Key(channelID, messageID)
	if _, ok := e.entries[key]; !ok {
		return false
	}
	delete(e.entries, key)
	return true
}

// UpdateStarCount sets the star count for an entry, creating it if needed.
func (e *Engine) UpdateStarCount(channelID, messageID, authorID, content string, count int) *Entry {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := Key(channelID, messageID)
	entry, ok := e.entries[key]
	if !ok {
		entry = &Entry{
			OriginalChannelID: channelID,
			OriginalMessageID: messageID,
			AuthorID:          authorID,
			Content:           content,
		}
		e.entries[key] = entry
	}
	entry.StarCount = count
	return entry
}

// ShouldRepost returns true if the star count meets the threshold.
func (e *Engine) ShouldRepost(entry *Entry) bool {
	return entry.StarCount >= e.threshold
}

// FormatStarEmbed builds the embed for a starboard repost.
func FormatStarEmbed(entry *Entry, guildID string, starEmoji string) *discordgo.MessageEmbed {
	if starEmoji == "" {
		starEmoji = "⭐"
	}
	jumpURL := fmt.Sprintf("https://discord.com/channels/%s/%s/%s",
		guildID, entry.OriginalChannelID, entry.OriginalMessageID)

	content := entry.Content
	if content == "" {
		content = "*(embed-only or attachment message)*"
	}
	if len(content) > 1024 {
		content = content[:1023] + "…"
	}

	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s %d", starEmoji, entry.StarCount),
		Color:       0xFFAC33, // gold
		Description: content,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Author",
				Value:  fmt.Sprintf("<@%s>", entry.AuthorID),
				Inline: true,
			},
			{
				Name:   "Channel",
				Value:  fmt.Sprintf("<#%s>", entry.OriginalChannelID),
				Inline: true,
			},
			{
				Name:   "Jump",
				Value:  fmt.Sprintf("[Click here](%s)", jumpURL),
				Inline: true,
			},
		},
		Timestamp: entry.RepostedAt.Format(time.RFC3339),
	}
}
