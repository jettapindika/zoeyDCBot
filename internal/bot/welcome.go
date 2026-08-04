package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
	"github.com/jettapindika/zoeyDCBot/internal/recoverutil"
)

var welcomeLog = logging.Component("welcome")

// onGuildMemberAdd sends a welcome message when a member joins.
func (b *Bot) onGuildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	defer recoverutil.Recover("onGuildMemberAdd")

	if !b.cfg.WelcomeEnabled {
		return
	}
	if b.cfg.WelcomeChannelID == "" {
		return
	}

	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		welcomeLog.Debug("guild not in state", "guild", m.GuildID, "err", err)
		guild = nil
	}

	displayName := m.User.Username
	if m.Nick != "" {
		displayName = m.Nick
	}

	msg := b.cfg.WelcomeMessage
	if msg == "" {
		msg = "Welcome to the server, {user}! 🎉"
	}
	msg = replacePlaceholders(msg, displayName, m.User.ID, m.GuildID, guild)

	embed := &discordgo.MessageEmbed{
		Title:       "👋 Welcome!",
		Description: msg,
		Color:       colorGreen,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: m.User.AvatarURL(""),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Member #%d", memberCount(s, m.GuildID)),
		},
	}

	_, err = s.ChannelMessageSendEmbed(b.cfg.WelcomeChannelID, embed)
	if err != nil {
		welcomeLog.Error("failed to send welcome message", "err", err)
		return
	}
	welcomeLog.Info("welcome message sent", "guild", m.GuildID, "user", m.User.ID)
}

// onGuildMemberRemove sends a goodbye message when a member leaves.
func (b *Bot) onGuildMemberRemove(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	defer recoverutil.Recover("onGuildMemberRemove")

	if !b.cfg.GoodbyeEnabled {
		return
	}
	if b.cfg.WelcomeChannelID == "" {
		return
	}

	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		guild = nil
	}

	displayName := m.User.Username
	if m.Nick != "" {
		displayName = m.Nick
	}

	msg := b.cfg.GoodbyeMessage
	if msg == "" {
		msg = "Goodbye, {user}! We'll miss you. 👋"
	}
	msg = replacePlaceholders(msg, displayName, m.User.ID, m.GuildID, guild)

	embed := &discordgo.MessageEmbed{
		Title:       "👋 Goodbye!",
		Description: msg,
		Color:       colorGray,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: m.User.AvatarURL(""),
		},
	}

	_, err = s.ChannelMessageSendEmbed(b.cfg.WelcomeChannelID, embed)
	if err != nil {
		welcomeLog.Error("failed to send goodbye message", "err", err)
		return
	}
	welcomeLog.Info("goodbye message sent", "guild", m.GuildID, "user", m.User.ID)
}

// replacePlaceholders substitutes {user}, {mention}, {server}, {count} in a message template.
func replacePlaceholders(msg, username, userID, guildID string, guild *discordgo.Guild) string {
	r := strings.NewReplacer(
		"{user}", username,
		"{mention}", fmt.Sprintf("<@%s>", userID),
		"{server}", func() string {
			if guild != nil {
				return guild.Name
			}
			return "the server"
		}(),
	)
	return r.Replace(msg)
}

// memberCount returns the approximate member count for a guild.
func memberCount(s *discordgo.Session, guildID string) int {
	guild, err := s.State.Guild(guildID)
	if err != nil || guild == nil {
		return 0
	}
	return guild.MemberCount
}
