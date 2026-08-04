package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/admin"
	"github.com/jettapindika/zoeyDCBot/internal/roles"
	"github.com/jettapindika/zoeyDCBot/internal/starboard"
)

// handlePrefixReactionRole creates a reaction-role message from a prefix command.
// Usage: x!reactionrole <emoji:role> [emoji:role...] [--title "My Title"] [--description "My Desc"]
// Example: x!reactionrole 🔥:@@Moderator 😀:123456789
func (b *Bot) handlePrefixReactionRole(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	if m.GuildID == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Not In Server", "This command can only be used in a server."))
		return
	}

	member, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	_ = member

	ok, err := admin.BotHasPermission(s, m.GuildID, m.ChannelID, discordgo.PermissionManageRoles)
	if err != nil || !ok {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Bot Permission Required", "I need Manage Roles permission to create reaction roles."))
		return
	}

	if args == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("No Bindings",
			"Usage: `x!reactionrole <emoji:role> [emoji:role...]`\nExample: `x!reactionrole 🔥:<@&123> 😀:456`"))
		return
	}

	// Parse optional --title and --description flags, rest are bindings.
	title := ""
	description := ""
	var bindingStrs []string

	tokens := tokenizeArgs(args)
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "--title="):
			title = strings.Trim(strings.TrimPrefix(tok, "--title="), "\"'")
		case strings.HasPrefix(tok, "--description="):
			description = strings.Trim(strings.TrimPrefix(tok, "--description="), "\"'")
		case strings.HasPrefix(tok, "--desc="):
			description = strings.Trim(strings.TrimPrefix(tok, "--desc="), "\"'")
		default:
			bindingStrs = append(bindingStrs, tok)
		}
	}

	if title == "" {
		title = "🎭 Reaction Roles"
	}
	if description == "" {
		description = "React to get a role!"
	}
	if len(bindingStrs) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("No Bindings",
			"You must provide at least one `emoji:role` binding."))
		return
	}

	// Parse bindings: each string is "emoji:@role" or "emoji:roleID"
	var bindings []roles.Binding
	var parseErrors []string
	for _, bs := range bindingStrs {
		parts := strings.SplitN(bs, ":", 2)
		if len(parts) != 2 {
			parseErrors = append(parseErrors, fmt.Sprintf("`%s` — missing colon separator", bs))
			continue
		}
		emoji := strings.TrimSpace(parts[0])
		roleStr := strings.TrimSpace(parts[1])

		// Extract role ID from mention format <@&ID> or raw ID
		roleID := roleStr
		if strings.HasPrefix(roleStr, "<@&") && strings.HasSuffix(roleStr, ">") {
			roleID = roleStr[3 : len(roleStr)-1]
		}

		if emoji == "" || roleID == "" {
			parseErrors = append(parseErrors, fmt.Sprintf("`%s` — empty emoji or role", bs))
			continue
		}

		// Validate role exists
		if _, err := s.State.Role(m.GuildID, roleID); err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("`%s` — role not found (ID: %s)", bs, roleID))
			continue
		}

		bindings = append(bindings, roles.Binding{Emoji: emoji, RoleID: roleID})
	}

	if len(parseErrors) > 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Binding Parse Errors", strings.Join(parseErrors, "\n")))
		return
	}
	if len(bindings) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("No Valid Bindings", "No valid emoji:role bindings were parsed."))
		return
	}
	if len(bindings) > 20 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Too Many Bindings", "Maximum 20 emoji:role bindings per message."))
		return
	}

	// Build embed
	bindingText := ""
	for _, bd := range bindings {
		bindingText += fmt.Sprintf("%s → <@&%s>\n", bd.Emoji, bd.RoleID)
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: description + "\n\n" + bindingText,
		Color:       colorBlue,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "React with the emoji below to get the role. Remove your reaction to lose it.",
		},
	}

	sent, err := s.ChannelMessageSendEmbed(m.ChannelID, embed)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Failed", "Failed to post message: "+err.Error()))
		return
	}

	// React with each emoji
	var failedReactions []string
	for _, bd := range bindings {
		emojiAPI := bd.Emoji
		if err := s.MessageReactionAdd(m.ChannelID, sent.ID, emojiAPI); err != nil {
			failedReactions = append(failedReactions, fmt.Sprintf("%s (%s)", bd.Emoji, err.Error()))
		}
	}

	if len(failedReactions) > 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Some Reactions Failed",
			fmt.Sprintf("Could not add: %s", strings.Join(failedReactions, ", "))))
	}
}

// handlePrefixRemoveRole removes a reaction-role message by message ID.
// Usage: x!removerrole <message_id>
func (b *Bot) handlePrefixRemoveRole(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	if m.GuildID == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Not In Server", "This command can only be used in a server."))
		return
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}

	msgID := strings.TrimSpace(args)
	if msgID == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing Message ID",
			"Usage: `x!removerrole <message_id>`"))
		return
	}

	// Validate it's a number
	if _, err := strconv.ParseInt(msgID, 10, 64); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Invalid Message ID",
			"Message ID must be a numeric value."))
		return
	}

	// Delete the message
	err := s.ChannelMessageDelete(m.ChannelID, msgID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Failed", "Failed to delete message: "+err.Error()))
		return
	}

	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🗑️ Removed",
		"Reaction-role message has been removed."))
}

// handlePrefixStarboard configures or disables the starboard from a prefix command.
// Usage: x!starboard set <#channel> [threshold]  |  x!starboard disable
func (b *Bot) handlePrefixStarboard(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	if m.GuildID == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Not In Server", "This command can only be used in a server."))
		return
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}

	parts := strings.Fields(args)
	if len(parts) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing Subcommand",
			"Usage: `x!starboard set <#channel> [threshold]` or `x!starboard disable`"))
		return
	}

	sub := strings.ToLower(parts[0])
	switch sub {
	case "set":
		if len(parts) < 2 {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing Channel",
				"Usage: `x!starboard set <#channel> [threshold]`"))
			return
		}

		channelStr := parts[1]
		channelID := channelStr
		// Extract channel ID from mention <#ID>
		if strings.HasPrefix(channelStr, "<#") && strings.HasSuffix(channelStr, ">") {
			channelID = channelStr[2 : len(channelStr)-1]
		}

		// Validate channel exists
		if _, err := s.State.Channel(channelID); err != nil {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Invalid Channel",
				"Could not find that channel. Use a channel mention like #general."))
			return
		}

		threshold := 0
		if len(parts) >= 3 {
			if t, err := strconv.Atoi(parts[2]); err == nil && t > 0 {
				threshold = t
			}
		}

		b.cfg.StarboardChannelID = channelID
		if threshold > 0 {
			b.starboard = starboard.New(threshold, "⭐")
		}

		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("⭐ Starboard Configured",
			fmt.Sprintf("Channel: <#%s>\nThreshold: %d stars\nEmoji: ⭐",
				b.cfg.StarboardChannelID, b.starboard.Threshold())))

	case "disable":
		b.cfg.StarboardChannelID = ""
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("⭐ Starboard Disabled",
			"Starboard has been turned off."))

	default:
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Unknown Subcommand",
			"Usage: `x!starboard set <#channel> [threshold]` or `x!starboard disable`"))
	}
}

// tokenizeArgs splits a command argument string into tokens, respecting
// double-quoted substrings so --title="My Cool Title" works as one token.
func tokenizeArgs(s string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for _, ch := range s {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case (ch == ' ' || ch == '\t') && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
