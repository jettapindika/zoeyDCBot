package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/admin"
)


// --- Channel Management -----------------------------------------------------

func (b *Bot) cmdCreateChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	name := admin.StringOption(data.Options, "name")
	if name == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Name", "Channel name is required."))
		return
	}
	ctype := admin.StringOption(data.Options, "type")
	categoryID := admin.StringOption(data.Options, "category")
	nsfw := false
	if v, ok := getBoolOption(data.Options, "nsfw"); ok {
		nsfw = v
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageChannels); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	var channelType discordgo.ChannelType
	switch strings.ToLower(ctype) {
	case "voice":
		channelType = discordgo.ChannelTypeGuildVoice
	case "text", "":
		channelType = discordgo.ChannelTypeGuildText
	default:
		b.respondEphemeralEmbed(s, i, warnEmbed("Invalid Type", "Channel type must be `text` or `voice`."))
		return
	}

	ch, err := s.GuildChannelCreateComplex(i.GuildID, discordgo.GuildChannelCreateData{
		Name:      name,
		Type:      channelType,
		ParentID:  categoryID,
		NSFW:      nsfw,
	})
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Create Failed", fmt.Sprintf("Failed to create channel: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("📝 Channel Created",
		fmt.Sprintf("**%s** channel <#%s> has been created.", strings.Title(ctype), ch.ID)))
	b.logModAction(i, fmt.Sprintf("Channel created: #%s (%s)", name, ch.ID))
}

func (b *Bot) cmdDeleteChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	channelID := ""
	for _, opt := range data.Options {
		if opt.Name == "channel" {
			if ch := opt.ChannelValue(s); ch != nil {
				channelID = ch.ID
			}
		}
	}
	if channelID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Channel", "Channel is required."))
		return
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageChannels); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	ch, err := s.Channel(channelID)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Error", fmt.Sprintf("Failed to get channel: %v", err)))
		return
	}

	if _, err := s.ChannelDelete(channelID); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Delete Failed", fmt.Sprintf("Failed to delete channel: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("🗑️ Channel Deleted",
		fmt.Sprintf("Channel **#%s** has been deleted.", ch.Name)))
	b.logModAction(i, fmt.Sprintf("Channel deleted: #%s (%s)", ch.Name, channelID))
}

func (b *Bot) cmdEditChannel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	channelID := ""
	for _, opt := range data.Options {
		if opt.Name == "channel" {
			if ch := opt.ChannelValue(s); ch != nil {
				channelID = ch.ID
			}
		}
	}
	if channelID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Channel", "Channel is required."))
		return
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageChannels); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	edit := &discordgo.ChannelEdit{}
	changed := false
	for _, opt := range data.Options {
		switch opt.Name {
		case "name":
			if v, ok := opt.Value.(string); ok && v != "" {
				edit.Name = v
				changed = true
			}
		case "topic":
			if v, ok := opt.Value.(string); ok {
				edit.Topic = v
				changed = true
			}
		case "slowmode":
			if v, ok := opt.Value.(float64); ok {
				ri := int(v)
				edit.RateLimitPerUser = &ri
				changed = true
			}
		case "nsfw":
			if v, ok := opt.Value.(bool); ok {
				edit.NSFW = &v  // already *bool
				changed = true
			}
		}
	}
	if !changed {
		b.respondEphemeralEmbed(s, i, warnEmbed("No Changes", "Provide at least one property to edit."))
		return
	}

	ch, err := s.ChannelEdit(channelID, edit)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Edit Failed", fmt.Sprintf("Failed to edit channel: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("✏️ Channel Updated",
		fmt.Sprintf("Channel <#%s> has been updated.", ch.ID)))
	b.logModAction(i, fmt.Sprintf("Channel edited: #%s (%s)", ch.Name, ch.ID))
}

// --- Role Management --------------------------------------------------------

func (b *Bot) cmdCreateRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	name := admin.StringOption(data.Options, "name")
	if name == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Name", "Role name is required."))
		return
	}
	color := admin.StringOption(data.Options, "color")
	hoist := false
	if v, ok := getBoolOption(data.Options, "hoist"); ok {
		hoist = v
	}
	mentionable := false
	if v, ok := getBoolOption(data.Options, "mentionable"); ok {
		mentionable = v
	}
	perms := int64(0)
	if v, ok := optString(data.Options, "permissions"); ok && v != "" {
		perms = parsePermissionBits(v)
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageRoles); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	roleParams := discordgo.RoleParams{
		Name: name,
	}
	if color != "" {
		c := parseColor(color)
		roleParams.Color = &c
	}
	if hoist {
		roleParams.Hoist = &hoist
	}
	if mentionable {
		roleParams.Mentionable = &mentionable
	}
	if perms != 0 {
		roleParams.Permissions = &perms
	}

	role, err := s.GuildRoleCreate(i.GuildID, &roleParams)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Create Failed", fmt.Sprintf("Failed to create role: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("🏷️ Role Created",
		fmt.Sprintf("Role <@&%s> has been created.", role.ID)))
	b.logModAction(i, fmt.Sprintf("Role created: %s (%s)", name, role.ID))
}

func (b *Bot) cmdDeleteRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	roleID := ""
	for _, opt := range data.Options {
		if opt.Name == "role" {
			if r := opt.RoleValue(s, i.GuildID); r != nil {
				roleID = r.ID
			}
		}
	}
	if roleID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Role", "Role is required."))
		return
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageRoles); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	if err := s.GuildRoleDelete(i.GuildID, roleID); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Delete Failed", fmt.Sprintf("Failed to delete role: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("🗑️ Role Deleted", "The role has been deleted."))
	b.logModAction(i, fmt.Sprintf("Role deleted: %s", roleID))
}

func (b *Bot) cmdEditRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	roleID := ""
	for _, opt := range data.Options {
		if opt.Name == "role" {
			if r := opt.RoleValue(s, i.GuildID); r != nil {
				roleID = r.ID
			}
		}
	}
	if roleID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Role", "Role is required."))
		return
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageRoles); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	edit := &discordgo.RoleParams{}
		var nameVal, colorVal, permsValStr string
		var hoistVal, mentionVal bool
		_ = nameVal
		_ = colorVal
		_ = permsValStr
		_ = hoistVal
		_ = mentionVal
	changed := false
	for _, opt := range data.Options {
		switch opt.Name {
		case "name":
			if v, ok := opt.Value.(string); ok && v != "" {
				edit.Name = v
				changed = true
			}
		case "color":
			if v, ok := opt.Value.(string); ok {
				cv := parseColor(v); edit.Color = &cv
				changed = true
			}
		case "hoist":
			if v, ok := opt.Value.(bool); ok {
				edit.Hoist = &v
				changed = true
			}
		case "mentionable":
			if v, ok := opt.Value.(bool); ok {
				edit.Mentionable = &v
				changed = true
			}
		case "permissions":
			if v, ok := opt.Value.(string); ok && v != "" {
				pv := parsePermissionBits(v); edit.Permissions = &pv
				changed = true
			}
		}
	}
	if !changed {
		b.respondEphemeralEmbed(s, i, warnEmbed("No Changes", "Provide at least one property to edit."))
		return
	}

	role, err := s.GuildRoleEdit(i.GuildID, roleID, edit)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Edit Failed", fmt.Sprintf("Failed to edit role: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("✏️ Role Updated",
		fmt.Sprintf("Role <@&%s> has been updated.", role.ID)))
	b.logModAction(i, fmt.Sprintf("Role edited: %s (%s)", role.Name, role.ID))
}

func (b *Bot) cmdGiveRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	userID := ""
	roleID := ""
	for _, opt := range data.Options {
		switch opt.Name {
		case "user":
			if u := opt.UserValue(s); u != nil {
				userID = u.ID
			}
		case "role":
			if r := opt.RoleValue(s, i.GuildID); r != nil {
				roleID = r.ID
			}
		}
	}
	if userID == "" || roleID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Parameters", "Both user and role are required."))
		return
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageRoles); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	if err := s.GuildMemberRoleAdd(i.GuildID, userID, roleID); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Failed", fmt.Sprintf("Failed to add role: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("✅ Role Added",
		fmt.Sprintf("Added <@&%s> to <@%s>.", roleID, userID)))
	b.logModAction(i, fmt.Sprintf("Role added: %s → user %s", roleID, userID))
}

func (b *Bot) cmdRemoveRole(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	userID := ""
	roleID := ""
	for _, opt := range data.Options {
		switch opt.Name {
		case "user":
			if u := opt.UserValue(s); u != nil {
				userID = u.ID
			}
		case "role":
			if r := opt.RoleValue(s, i.GuildID); r != nil {
				roleID = r.ID
			}
		}
	}
	if userID == "" || roleID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Parameters", "Both user and role are required."))
		return
	}

	if _, err := b.checkAdmin(i, discordgo.PermissionManageRoles); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}

	if err := s.GuildMemberRoleRemove(i.GuildID, userID, roleID); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Failed", fmt.Sprintf("Failed to remove role: %v", err)))
		return
	}

	b.respondPublicEmbed(s, i, successEmbed("✅ Role Removed",
		fmt.Sprintf("Removed <@&%s> from <@%s>.", roleID, userID)))
	b.logModAction(i, fmt.Sprintf("Role removed: %s → user %s", roleID, userID))
}

// --- Info Commands ----------------------------------------------------------

func (b *Bot) cmdChannelInfo(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	channelID := i.ChannelID
	for _, opt := range data.Options {
		if opt.Name == "channel" {
			if ch := opt.ChannelValue(s); ch != nil {
				channelID = ch.ID
			}
		}
	}

	ch, err := s.Channel(channelID)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Error", fmt.Sprintf("Failed to get channel: %v", err)))
		return
	}

	typeName := "Text"
	switch ch.Type {
	case discordgo.ChannelTypeGuildVoice:
		typeName = "Voice"
	case discordgo.ChannelTypeGuildCategory:
		typeName = "Category"
	case discordgo.ChannelTypeGuildNews:
		typeName = "Announcement"
	case discordgo.ChannelTypeGuildStageVoice:
		typeName = "Stage"
	}

	fields := []*discordgo.MessageEmbedField{
		inlineField("Name", "#"+ch.Name),
		inlineField("ID", ch.ID),
		inlineField("Type", typeName),
	}
	if ch.Topic != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Topic",
			Value:   ch.Topic,
			Inline:  false,
		})
	}
	if ch.RateLimitPerUser > 0 {
		fields = append(fields, inlineField("Slowmode", fmt.Sprintf("%ds", ch.RateLimitPerUser)))
	}
	if ch.NSFW {
		fields = append(fields, inlineField("NSFW", "Yes"))
	}
	if ch.ParentID != "" {
		fields = append(fields, inlineField("Category", fmt.Sprintf("<#%s>", ch.ParentID)))
	}
	fields = append(fields, inlineField("Position", fmt.Sprintf("%d", ch.Position)))

	b.respondPublicEmbed(s, i, &discordgo.MessageEmbed{
		Title:  "📋 Channel Information",
		Color:  ColorInfo,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s", i.Member.User.Username),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (b *Bot) cmdRoleInfo(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	roleID := ""
	for _, opt := range data.Options {
		if opt.Name == "role" {
			if r := opt.RoleValue(s, i.GuildID); r != nil {
				roleID = r.ID
			}
		}
	}
	if roleID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Missing Role", "Role is required."))
		return
	}

	role, err := s.State.Role(i.GuildID, roleID)
	if err != nil {
		role, err = s.GuildRole(i.GuildID, roleID)
		if err != nil {
			b.respondEphemeralEmbed(s, i, errEmbed("Error", fmt.Sprintf("Failed to get role: %v", err)))
			return
		}
	}

	fields := []*discordgo.MessageEmbedField{
		inlineField("Name", role.Name),
		inlineField("ID", role.ID),
		inlineField("Color", fmt.Sprintf("#%06X", role.Color)),
	}
	memberCount := 0
	if guild, err := s.State.Guild(i.GuildID); err == nil {
		for _, m := range guild.Members {
			for _, r := range m.Roles {
				if r == roleID {
					memberCount++
					break
				}
			}
		}
	}
	fields = append(fields, inlineField("Members", fmt.Sprintf("%d", memberCount)))
	fields = append(fields, inlineField("Hoisted", boolStr(role.Hoist)))
	fields = append(fields, inlineField("Mentionable", boolStr(role.Mentionable)))
	fields = append(fields, inlineField("Position", fmt.Sprintf("%d", role.Position)))

	permStr := formatPermissions(role.Permissions)
	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Key Permissions",
		Value:   permStr,
		Inline:  false,
	})

	embed := &discordgo.MessageEmbed{
		Title:  "🏷️ Role Information",
		Color:  role.Color,
		Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s", i.Member.User.Username),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	b.respondPublicEmbed(s, i, embed)
}

// --- Helpers ----------------------------------------------------------------

func (b *Bot) logModAction(i *discordgo.InteractionCreate, action string) {
	if b.cfg.ModLogChannel == "" {
		return
	}
	msg := fmt.Sprintf("**%s** (<@%s>) — %s", i.Member.User.Username, i.Member.User.ID, action)
	_, _ = b.sess.ChannelMessageSend(b.cfg.ModLogChannel, msg)
}

func getBoolOption(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (bool, bool) {
	for _, opt := range opts {
		if opt.Name == name {
			if v, ok := opt.Value.(bool); ok {
				return v, true
			}
		}
	}
	return false, false
}

func optString(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (string, bool) {
	for _, opt := range opts {
		if opt.Name == name {
			if v, ok := opt.Value.(string); ok {
				return v, true
			}
		}
	}
	return "", false
}

func boolStr(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func parseColor(s string) int {
	s = strings.TrimPrefix(s, "#")
	if s == "" {
		return 0
	}
	var c int
	if _, err := fmt.Sscanf(s, "%x", &c); err != nil {
		return 0
	}
	return c
}

// parsePermissionBits converts a comma-separated list of permission names
// (e.g. "send_messages,manage_messages") into a Discord permission bitfield.
func parsePermissionBits(s string) int64 {
	var perms int64
	for _, name := range strings.Split(s, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		switch name {
		case "administrator", "admin":
			perms |= discordgo.PermissionAdministrator
		case "kick_members", "kick":
			perms |= discordgo.PermissionKickMembers
		case "ban_members", "ban":
			perms |= discordgo.PermissionBanMembers
		case "manage_guild", "manage_server":
			perms |= discordgo.PermissionManageGuild
		case "manage_channels", "manage_channel":
			perms |= discordgo.PermissionManageChannels
		case "manage_roles", "manage_role":
			perms |= discordgo.PermissionManageRoles
		case "manage_messages":
			perms |= discordgo.PermissionManageMessages
		case "send_messages", "send":
			perms |= discordgo.PermissionSendMessages
		case "read_messages", "view_channel":
			perms |= discordgo.PermissionReadMessages
		case "embed_links":
			perms |= discordgo.PermissionEmbedLinks
		case "attach_files":
			perms |= discordgo.PermissionAttachFiles
		case "read_message_history":
			perms |= discordgo.PermissionReadMessageHistory
		case "mention_everyone":
			perms |= discordgo.PermissionMentionEveryone
		case "connect":
			perms |= discordgo.PermissionVoiceConnect
		case "speak":
			perms |= discordgo.PermissionVoiceSpeak
		case "mute_members":
			perms |= discordgo.PermissionVoiceMuteMembers
		case "move_members":
			perms |= discordgo.PermissionVoiceMoveMembers
		case "deafen_members":
			perms |= discordgo.PermissionVoiceDeafenMembers
		case "priority_speaker":
			perms |= discordgo.PermissionVoicePrioritySpeaker
		case "view_audit_log":
			perms |= discordgo.PermissionViewAuditLogs
		case "add_reactions":
			perms |= discordgo.PermissionAddReactions
		case "stream":
		case "change_nickname":
			perms |= discordgo.PermissionChangeNickname
		case "manage_nicknames":
			perms |= discordgo.PermissionManageNicknames
		case "manage_webhooks":
			perms |= discordgo.PermissionManageWebhooks
		case "manage_emojis":
			perms |= discordgo.PermissionManageEmojis
		}
	}
	return perms
}

// formatPermissions returns a human-readable list of key permissions.
func formatPermissions(perms int64) string {
	var names []string
	checks := []struct {
		bit  int64
		name string
	}{
		{discordgo.PermissionAdministrator, "Administrator"},
		{discordgo.PermissionKickMembers, "Kick"},
		{discordgo.PermissionBanMembers, "Ban"},
		{discordgo.PermissionManageGuild, "Manage Server"},
		{discordgo.PermissionManageChannels, "Manage Channels"},
		{discordgo.PermissionManageRoles, "Manage Roles"},
		{discordgo.PermissionManageMessages, "Manage Messages"},
		{discordgo.PermissionSendMessages, "Send Messages"},
		{discordgo.PermissionReadMessages, "View Channels"},
		{discordgo.PermissionEmbedLinks, "Embed Links"},
		{discordgo.PermissionAttachFiles, "Attach Files"},
		{discordgo.PermissionReadMessageHistory, "Read History"},
		{discordgo.PermissionMentionEveryone, "Mention Everyone"},
		{discordgo.PermissionVoiceConnect, "Connect"},
		{discordgo.PermissionVoiceSpeak, "Speak"},
		{discordgo.PermissionVoiceMoveMembers, "Move Members"},
		{discordgo.PermissionAddReactions, "Add Reactions"},
		{discordgo.PermissionManageWebhooks, "Manage Webhooks"},
		{discordgo.PermissionManageEmojis, "Manage Emojis"},
	}
	for _, c := range checks {
		if perms&c.bit != 0 {
			names = append(names, c.name)
			if c.bit == discordgo.PermissionAdministrator {
				break // Admin implies everything else.
			}
		}
	}
	if len(names) == 0 {
		return "None"
	}
	return strings.Join(names, ", ")
}

