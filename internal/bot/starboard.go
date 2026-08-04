package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
	"github.com/jettapindika/zoeyDCBot/internal/recoverutil"
	"github.com/jettapindika/zoeyDCBot/internal/starboard"
)

var starboardLog = logging.Component("starboard")

// onStarboardReactionAdd handles ⭐ reactions for the starboard.
// This is called from onReactionAdd after the reaction-role check.
func (b *Bot) onStarboardReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	defer recoverutil.Recover("onStarboardReactionAdd")

	if b.cfg.StarboardChannelID == "" || b.starboard == nil {
		return
	}
	if r.Emoji.Name != b.starboard.Emoji() {
		return
	}
	if r.UserID == s.State.User.ID {
		return
	}

	// Fetch the message to get content and author.
	msg, err := s.ChannelMessage(r.ChannelID, r.MessageID)
	if err != nil {
		starboardLog.Debug("failed to fetch message for starboard", "err", err)
		return
	}
	if msg.Author == nil || msg.Author.Bot {
		return // don't starboard bot messages
	}

	// Count stars (get all reactions for this emoji).
	reactions, err := s.MessageReactions(r.ChannelID, r.MessageID, r.Emoji.Name, 100, "", "")
	if err != nil {
		starboardLog.Debug("failed to fetch reactions", "err", err)
		return
	}
	count := len(reactions)

	// Update or create the entry.
	entry := b.starboard.UpdateStarCount(r.ChannelID, r.MessageID, msg.Author.ID, msg.Content, count)

	// Check if we should post or update.
	if !b.starboard.ShouldRepost(entry) {
		return
	}

	embed := starboard.FormatStarEmbed(entry, r.GuildID, b.starboard.Emoji())

	if entry.StarboardMessageID == "" {
		// First time reaching threshold — post new starboard message.
		sent, err := s.ChannelMessageSendEmbed(b.cfg.StarboardChannelID, embed)
		if err != nil {
			starboardLog.Error("failed to post starboard message", "err", err)
			return
		}
		entry.StarboardMessageID = sent.ID
		entry.RepostedAt = sent.Timestamp
		b.starboard.SetEntry(entry)
		starboardLog.Info("starboard message posted", "channel", r.ChannelID, "msg", r.MessageID, "stars", count)
	} else {
		// Already posted — update the existing starboard message.
		_, err := s.ChannelMessageEditEmbed(b.cfg.StarboardChannelID, entry.StarboardMessageID, embed)
		if err != nil {
			starboardLog.Error("failed to edit starboard message", "err", err)
			return
		}
		starboardLog.Debug("starboard message updated", "msg", r.MessageID, "stars", count)
	}
}

// onStarboardReactionRemove handles ⭐ reaction removal.
func (b *Bot) onStarboardReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	defer recoverutil.Recover("onStarboardReactionRemove")

	if b.cfg.StarboardChannelID == "" || b.starboard == nil {
		return
	}
	if r.Emoji.Name != b.starboard.Emoji() {
		return
	}
	if r.UserID == s.State.User.ID {
		return
	}

	entry, ok := b.starboard.GetEntry(r.ChannelID, r.MessageID)
	if !ok || entry.StarboardMessageID == "" {
		return
	}

	// Get current star count.
	reactions, err := s.MessageReactions(r.ChannelID, r.MessageID, r.Emoji.Name, 100, "", "")
	if err != nil {
		return
	}
	count := len(reactions)
	entry.StarCount = count

	if count < b.starboard.Threshold() {
		// Below threshold — delete the starboard message.
		_ = s.ChannelMessageDelete(b.cfg.StarboardChannelID, entry.StarboardMessageID)
		b.starboard.RemoveEntry(r.ChannelID, r.MessageID)
		starboardLog.Info("starboard message removed (below threshold)", "msg", r.MessageID, "stars", count)
	} else {
		// Still above threshold — update the count.
		embed := starboard.FormatStarEmbed(entry, r.GuildID, b.starboard.Emoji())
		_, _ = s.ChannelMessageEditEmbed(b.cfg.StarboardChannelID, entry.StarboardMessageID, embed)
		b.starboard.SetEntry(entry)
	}
}

// cmdStarboard configures the starboard channel and threshold.
func (b *Bot) cmdStarboard(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer recoverutil.Recover("cmdStarboard")

	data := i.ApplicationCommandData()

	_, err := b.checkAdmin(i, discordgo.PermissionManageChannels)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	channel := ""
	threshold := 0
	for _, opt := range data.Options {
		switch opt.Name {
		case "channel":
			if ch, ok := opt.Value.(*discordgo.Channel); ok {
				channel = ch.ID
			}
		case "threshold":
			threshold = int(opt.IntValue())
		}
	}

	if channel != "" {
		b.cfg.StarboardChannelID = channel
	}
	if threshold > 0 {
		// Recreate the engine with the new threshold.
		b.starboard = starboard.New(threshold, "⭐")
	}

	embed := successEmbed("⭐ Starboard Configured", fmt.Sprintf(
		"Channel: <#%s>\nThreshold: %d stars\nEmoji: ⭐",
		b.cfg.StarboardChannelID,
		b.starboard.Threshold(),
	))
	b.respondEphemeralEmbed(s, i, embed)
}
