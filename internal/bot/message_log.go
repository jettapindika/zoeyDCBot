package bot

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
	"github.com/jettapindika/zoeyDCBot/internal/recoverutil"
)

var msgLog = logging.Component("msglog")

// onMessageDelete logs deleted messages to the mod-log channel.
func (b *Bot) onMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	defer recoverutil.Recover("onMessageDelete")

	if b.cfg.ModLogChannel == "" {
		return
	}

	// Try to get the message from cache (discordgo State may have it).
	var author, content string
	if msg, err := s.State.Message(m.ChannelID, m.ID); err == nil && msg != nil {
		author = msg.Author.Username
		content = msg.Content
		if content == "" {
			content = "(empty or embed-only message)"
		}
	} else {
		author = "unknown (message not in cache)"
		content = "(content unavailable — message was not cached)"
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🗑️ Message Deleted",
		Color:       colorRed,
		Timestamp:   time.Now().Format(time.RFC3339),
		Description: fmt.Sprintf("**Author:** %s\n**Channel:** <#%s>\n**Message ID:** `%s`", author, m.ChannelID, m.ID),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Content",
				Value:  truncateText(content, 1024),
				Inline: false,
			},
		},
	}
	_, _ = s.ChannelMessageSendEmbed(b.cfg.ModLogChannel, embed)
	msgLog.Debug("logged deleted message", "channel", m.ChannelID, "msgID", m.ID)
}

// onMessageUpdate logs edited messages to the mod-log channel.
func (b *Bot) onMessageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	defer recoverutil.Recover("onMessageUpdate")

	if b.cfg.ModLogChannel == "" {
		return
	}
	if m.Author == nil || m.Author.Bot {
		return
	}

	// Get old content from cache if available.
	oldContent := "(not cached)"
	if oldMsg, err := s.State.Message(m.ChannelID, m.ID); err == nil && oldMsg != nil {
		oldContent = oldMsg.Content
		if oldContent == "" {
			oldContent = "(empty)"
		}
	}

	newContent := m.Content
	if newContent == "" {
		// MessageUpdate may have partial data; check Embeds.
		if len(m.Embeds) > 0 {
			newContent = "(embed content — not logged)"
		} else {
			newContent = "(empty)"
		}
	}

	// Skip if content didn't actually change (e.g. embed added).
	if oldContent == newContent {
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "✏️ Message Edited",
		Color:       colorBlue,
		Timestamp:   time.Now().Format(time.RFC3339),
		Description: fmt.Sprintf("**Author:** %s\n**Channel:** <#%s>\n[Jump to message](https://discord.com/channels/%s/%s/%s)",
			m.Author.Username, m.ChannelID, m.GuildID, m.ChannelID, m.ID),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Before",
				Value:  truncateText(oldContent, 1024),
				Inline: false,
			},
			{
				Name:   "After",
				Value:  truncateText(newContent, 1024),
				Inline: false,
			},
		},
	}
	_, _ = s.ChannelMessageSendEmbed(b.cfg.ModLogChannel, embed)
	msgLog.Debug("logged edited message", "channel", m.ChannelID, "msgID", m.ID)
}

// truncateText clamps a string to maxLen, appending "…" if shortened.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
