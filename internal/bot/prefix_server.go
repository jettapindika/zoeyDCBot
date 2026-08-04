package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// parseRoleID extracts a role ID from <@&123> or raw ID.
func parseRoleID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<@&")
	s = strings.TrimSuffix(s, ">")
	return s
}

// parseChannelMention extracts a channel ID from <#123> or raw ID.
func parseChannelID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<#")
	s = strings.TrimSuffix(s, ">")
	return s
}

func (b *Bot) handlePrefixServerCmd(s *discordgo.Session, m *discordgo.MessageCreate, cmd, args string) {
	parts := strings.Fields(args)
	switch cmd {
	case "createchannel":
		b.prefixCreateChannel(s, m, parts)
	case "deletechannel":
		b.prefixDeleteChannel(s, m, parts)
	case "editchannel":
		b.prefixEditChannel(s, m, parts)
	case "createrole":
		b.prefixCreateRole(s, m, parts)
	case "deleterole":
		b.prefixDeleteRole(s, m, parts)
	case "editrole":
		b.prefixEditRole(s, m, parts)
	case "giverole":
		b.prefixGiveRole(s, m, parts)
	case "removerole":
		b.prefixRemoveRole(s, m, parts)
	case "channelinfo":
		b.prefixChannelInfo(s, m, parts)
	case "roleinfo":
		b.prefixRoleInfo(s, m, parts)
	}
}

func (b *Bot) prefixCreateChannel(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 1 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!createchannel <name> [text|voice]`"))
		return
	}
	name := parts[0]
	ctype := "text"
	if len(parts) > 1 {
		ctype = strings.ToLower(parts[1])
	}
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	var channelType discordgo.ChannelType
	switch ctype {
	case "voice":
		channelType = discordgo.ChannelTypeGuildVoice
	case "text", "":
		channelType = discordgo.ChannelTypeGuildText
	default:
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Invalid Type", "Type must be `text` or `voice`."))
		return
	}
	ch, err := s.GuildChannelCreateComplex(m.GuildID, discordgo.GuildChannelCreateData{Name: name, Type: channelType})
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Create Failed", fmt.Sprintf("Failed to create channel: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("📝 Channel Created",
		fmt.Sprintf("**%s** channel <#%s> has been created.", strings.Title(ctype), ch.ID)))
}

func (b *Bot) prefixDeleteChannel(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 1 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!deletechannel <channel_id_or_mention>`"))
		return
	}
	channelID := parseChannelID(parts[0])
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	ch, err := s.Channel(channelID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Error", fmt.Sprintf("Failed to get channel: %v", err)))
		return
	}
	if _, err := s.ChannelDelete(channelID); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Delete Failed", fmt.Sprintf("Failed to delete channel: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🗑️ Channel Deleted",
		fmt.Sprintf("Channel **#%s** has been deleted.", ch.Name)))
}

func (b *Bot) prefixEditChannel(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 3 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!editchannel <channel> <property> <value>`\nProperties: name, topic, slowmode, nsfw"))
		return
	}
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	channelID := parseChannelID(parts[0])
	prop := strings.ToLower(parts[1])
	value := strings.Join(parts[2:], " ")
	edit := &discordgo.ChannelEdit{}
	switch prop {
	case "name":
		edit.Name = value
	case "topic":
		edit.Topic = value
	case "slowmode":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 21600 {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Invalid Value", "Slowmode must be 0-21600 seconds."))
			return
		}
		edit.RateLimitPerUser = &n
	case "nsfw":
		nsfw := strings.ToLower(value) == "true" || value == "1"
		edit.NSFW = &nsfw
	default:
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Invalid Property", "Valid properties: name, topic, slowmode, nsfw"))
		return
	}
	ch, err := s.ChannelEdit(channelID, edit)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Edit Failed", fmt.Sprintf("Failed to edit channel: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("✏️ Channel Updated",
		fmt.Sprintf("Channel <#%s> has been updated.", ch.ID)))
}

func (b *Bot) prefixCreateRole(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 1 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!createrole <name> [color] [hoist] [mentionable]`"))
		return
	}
	name := parts[0]
	color := ""
	hoist := false
	mentionable := false
	if len(parts) > 1 {
		color = parts[1]
	}
	if len(parts) > 2 {
		hoist = strings.ToLower(parts[2]) == "true" || parts[2] == "1"
	}
	if len(parts) > 3 {
		mentionable = strings.ToLower(parts[3]) == "true" || parts[3] == "1"
	}
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	rp := discordgo.RoleParams{Name: name}
	if color != "" {
		c := parseColor(color)
		rp.Color = &c
	}
	if hoist {
		rp.Hoist = &hoist
	}
	if mentionable {
		rp.Mentionable = &mentionable
	}
	role, err := s.GuildRoleCreate(m.GuildID, &rp)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Create Failed", fmt.Sprintf("Failed to create role: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🏷️ Role Created",
		fmt.Sprintf("Role <@&%s> has been created.", role.ID)))
}

func (b *Bot) prefixDeleteRole(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 1 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!deleterole <role_id_or_mention>`"))
		return
	}
	roleID := parseRoleID(parts[0])
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	if err := s.GuildRoleDelete(m.GuildID, roleID); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Delete Failed", fmt.Sprintf("Failed to delete role: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🗑️ Role Deleted", "The role has been deleted."))
}

func (b *Bot) prefixEditRole(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 3 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!editrole <role> <property> <value>`\nProperties: name, color, hoist, mentionable"))
		return
	}
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	roleID := parseRoleID(parts[0])
	prop := strings.ToLower(parts[1])
	value := strings.Join(parts[2:], " ")
	edit := &discordgo.RoleParams{}
	switch prop {
	case "name":
		edit.Name = value
	case "color":
		c := parseColor(value)
		edit.Color = &c
	case "hoist":
		h := strings.ToLower(value) == "true" || value == "1"
		edit.Hoist = &h
	case "mentionable":
		m := strings.ToLower(value) == "true" || value == "1"
		edit.Mentionable = &m
	default:
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Invalid Property", "Valid properties: name, color, hoist, mentionable"))
		return
	}
	role, err := s.GuildRoleEdit(m.GuildID, roleID, edit)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Edit Failed", fmt.Sprintf("Failed to edit role: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("✏️ Role Updated",
		fmt.Sprintf("Role <@&%s> has been updated.", role.ID)))
}

func (b *Bot) prefixGiveRole(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 2 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!giverole <user> <role>`"))
		return
	}
	userID := parseUserID(parts[0])
	roleID := parseRoleID(parts[1])
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	if err := s.GuildMemberRoleAdd(m.GuildID, userID, roleID); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Failed", fmt.Sprintf("Failed to add role: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("✅ Role Added",
		fmt.Sprintf("Added <@&%s> to <@%s>.", roleID, userID)))
}

func (b *Bot) prefixRemoveRole(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 2 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!removerole <user> <role>`"))
		return
	}
	userID := parseUserID(parts[0])
	roleID := parseRoleID(parts[1])
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageRoles); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	if err := s.GuildMemberRoleRemove(m.GuildID, userID, roleID); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Failed", fmt.Sprintf("Failed to remove role: %v", err)))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("✅ Role Removed",
		fmt.Sprintf("Removed <@&%s> from <@%s>.", roleID, userID)))
}

func (b *Bot) prefixChannelInfo(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	channelID := m.ChannelID
	if len(parts) > 0 {
		channelID = parseChannelID(parts[0])
	}
	ch, err := s.Channel(channelID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Error", fmt.Sprintf("Failed to get channel: %v", err)))
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
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Topic", Value: ch.Topic, Inline: false})
	}
	if ch.RateLimitPerUser > 0 {
		fields = append(fields, inlineField("Slowmode", fmt.Sprintf("%ds", ch.RateLimitPerUser)))
	}
	if ch.NSFW {
		fields = append(fields, inlineField("NSFW", "Yes"))
	}
	fields = append(fields, inlineField("Position", fmt.Sprintf("%d", ch.Position)))
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title: "📋 Channel Information", Color: ColorInfo, Fields: fields,
	})
}

func (b *Bot) prefixRoleInfo(s *discordgo.Session, m *discordgo.MessageCreate, parts []string) {
	if len(parts) < 1 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Usage", "`x!roleinfo <role_id_or_mention>`"))
		return
	}
	roleID := parseRoleID(parts[0])
	role, err := s.State.Role(m.GuildID, roleID)
	if err != nil {
		role, err = s.GuildRole(m.GuildID, roleID)
		if err != nil {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Error", fmt.Sprintf("Failed to get role: %v", err)))
			return
		}
	}
	fields := []*discordgo.MessageEmbedField{
		inlineField("Name", role.Name),
		inlineField("ID", role.ID),
		inlineField("Color", fmt.Sprintf("#%06X", role.Color)),
	}
	memberCount := 0
	if guild, err := s.State.Guild(m.GuildID); err == nil {
		for _, mem := range guild.Members {
			for _, r := range mem.Roles {
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
	fields = append(fields, &discordgo.MessageEmbedField{Name: "Key Permissions", Value: formatPermissions(role.Permissions), Inline: false})
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title: "🏷️ Role Information", Color: role.Color, Fields: fields,
	})
}
