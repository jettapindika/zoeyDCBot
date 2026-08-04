package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/admin"
	"github.com/jettapindika/zoeyDCBot/internal/logging"
	"github.com/jettapindika/zoeyDCBot/internal/recoverutil"
	"github.com/jettapindika/zoeyDCBot/internal/roles"
)

var reactionLog = logging.Component("reactions")

// onReactionAdd handles reaction-role assignments.
func (b *Bot) onReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	defer recoverutil.Recover("onReactionAdd")

	if r.UserID == s.State.User.ID {
		return // ignore bot's own reactions
	}

	msg, ok := b.roles.Lookup(r.MessageID)
	if !ok {
		return
	}

	roleID, ok := msg.RoleForEmoji(r.Emoji.Name)
	if !ok {
		return
	}

	if err := s.GuildMemberRoleAdd(r.GuildID, r.UserID, roleID); err != nil {
		reactionLog.Error("failed to add role", "guild", r.GuildID, "user", r.UserID, "role", roleID, "err", err)
		return
	}
	reactionLog.Debug("role added via reaction", "guild", r.GuildID, "user", r.UserID, "role", roleID, "emoji", r.Emoji.Name)
}

// onReactionRemove handles reaction-role removal.
func (b *Bot) onReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	defer recoverutil.Recover("onReactionRemove")

	if r.UserID == s.State.User.ID {
		return
	}

	msg, ok := b.roles.Lookup(r.MessageID)
	if !ok {
		return
	}

	roleID, ok := msg.RoleForEmoji(r.Emoji.Name)
	if !ok {
		return
	}

	if err := s.GuildMemberRoleRemove(r.GuildID, r.UserID, roleID); err != nil {
		reactionLog.Error("failed to remove role", "guild", r.GuildID, "user", r.UserID, "role", roleID, "err", err)
		return
	}
	reactionLog.Debug("role removed via reaction", "guild", r.GuildID, "user", r.UserID, "role", roleID, "emoji", r.Emoji.Name)
}

// cmdReactionRole creates a reaction-role message.
//
// Usage: /reactionrole <description> <emoji1>:<@role1> <emoji2>:<@role2> ...
//
// The bot posts an embed with the description, then reacts with each emoji.
// Users can react to get the corresponding role.
func (b *Bot) cmdReactionRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer recoverutil.Recover("cmdReactionRole")

	data := i.ApplicationCommandData()

	// Permission check: Manage Roles + Manage Messages
	member, err := b.checkAdmin(i, discordgo.PermissionManageRoles)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionManageRoles)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Manage Roles permission to create reaction roles."))
		return
	}

	title := ""
	description := ""
	var bindingStrs []string
	for _, opt := range data.Options {
		switch opt.Name {
		case "title":
			title = opt.StringValue()
		case "description":
			description = opt.StringValue()
		case "bindings":
			// Split space-separated bindings into individual entries.
			for _, b := range strings.Fields(opt.StringValue()) {
				bindingStrs = append(bindingStrs, b)
			}
		}
	}

	if title == "" {
		title = "🎭 Reaction Roles"
	}
	if description == "" {
		description = "React to get a role!"
	}
	if len(bindingStrs) == 0 {
		b.respondEphemeralEmbed(s, i, errEmbed("No Bindings", "You must provide at least one emoji:role binding."))
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
		if _, err := s.State.Role(i.GuildID, roleID); err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("`%s` — role not found (ID: %s)", bs, roleID))
			continue
		}

		bindings = append(bindings, roles.Binding{Emoji: emoji, RoleID: roleID})
	}

	if len(parseErrors) > 0 {
		b.respondEphemeralEmbed(s, i, errEmbed("Binding Parse Errors", strings.Join(parseErrors, "\n")))
		return
	}
	if len(bindings) == 0 {
		b.respondEphemeralEmbed(s, i, errEmbed("No Valid Bindings", "No valid emoji:role bindings were parsed."))
		return
	}
	if len(bindings) > 20 {
		b.respondEphemeralEmbed(s, i, errEmbed("Too Many Bindings", "Maximum 20 emoji:role bindings per message."))
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
		Color:      colorBlue,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "React with the emoji below to get the role. Remove your reaction to lose it.",
		},
	}

	// Acknowledge interaction first
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Creating reaction-role message…",
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		reactionLog.Error("failed to acknowledge interaction", "err", err)
		return
	}

	// Post the reaction-role message
	sent, err := s.ChannelMessageSendEmbed(i.ChannelID, embed)
	if err != nil {
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: ptr("Failed to post message: " + err.Error()),
		})
		return
	}

	// React with each emoji
	var failedReactions []string
	for _, bd := range bindings {
		emojiAPI := bd.Emoji
		// For custom emoji, discordgo expects "name:id" format
		if err := s.MessageReactionAdd(i.ChannelID, sent.ID, emojiAPI); err != nil {
			failedReactions = append(failedReactions, fmt.Sprintf("%s (%s)", bd.Emoji, err.Error()))
		}
	}

	// Register with the manager
	b.roles.Register(i.ChannelID, sent.ID, bindings)

	// Update the interaction response with success
	resultText := fmt.Sprintf("✅ Reaction-role message posted in <#%s>.\nMessage ID: `%s`\nRoles bound: %d", i.ChannelID, sent.ID, len(bindings))
	if len(failedReactions) > 0 {
		resultText += "\n\n⚠️ Failed to add some reactions:\n" + strings.Join(failedReactions, "\n")
	}
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: ptr(resultText),
	})

	reactionLog.Info("reaction-role message created", "guild", i.GuildID, "channel", i.ChannelID, "msgID", sent.ID, "bindings", len(bindings), "user", member.User.ID)
}

// cmdRemoveReactionRole removes a reaction-role message and unregisters it.
func (b *Bot) cmdRemoveReactionRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer recoverutil.Recover("cmdRemoveReactionRole")

	data := i.ApplicationCommandData()
	msgID := admin.StringOption(data.Options, "message_id")

	if msgID == "" {
		b.respondEphemeralEmbed(s, i, errEmbed("Missing Message ID", "Please provide the message ID of the reaction-role message to remove."))
		return
	}

	_, err := b.checkAdmin(i, discordgo.PermissionManageRoles)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	msg, ok := b.roles.Lookup(msgID)
	if !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Not Found", "No reaction-role message found with that ID. It may have been created before the bot restarted (state is in-memory)."))
		return
	}

	// Delete the message
	if err := s.ChannelMessageDelete(msg.ChannelID, msg.MessageID); err != nil {
		reactionLog.Error("failed to delete reaction-role message", "err", err)
		// Still unregister even if delete fails
	}

	b.roles.Unregister(msgID)
	b.respondEphemeralEmbed(s, i, successEmbed("🗑️ Removed", "Reaction-role message deleted and unregistered."))
	reactionLog.Info("reaction-role message removed", "guild", i.GuildID, "msgID", msgID, "user", i.Member.User.ID)
}

// ptr returns a pointer to s (helper for webhook edits).
func ptr(s string) *string { return &s }
