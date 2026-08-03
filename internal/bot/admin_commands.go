package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/admin"
)

// resolveMember tries state cache first, then REST.
func (b *Bot) resolveMember(guildID, userID string) (*discordgo.Member, error) {
	if m, err := b.sess.State.Member(guildID, userID); err == nil && m != nil {
		return m, nil
	}
	return b.sess.GuildMember(guildID, userID)
}

// checkAdmin checks invoker permission and bot permission for an admin command.
func (b *Bot) checkAdmin(i *discordgo.InteractionCreate, perm int64) (*discordgo.Member, error) {
	guildID := i.GuildID
	if guildID == "" {
		return nil, fmt.Errorf("this command can only be used in a server")
	}
	if i.Member == nil || i.Member.User == nil {
		return nil, fmt.Errorf("could not resolve your Discord member")
	}
	member, err := b.resolveMember(guildID, i.Member.User.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve your member: %w", err)
	}
	ok, err := admin.HasPermission(b.sess, guildID, i.ChannelID, member, b.cfg.AdminRoleIDs, perm)
	if err != nil {
		return nil, fmt.Errorf("permission check failed: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("you don't have permission to use this command")
	}
	return member, nil
}

func (b *Bot) cmdPurge(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	amount := admin.IntOption(data.Options, "amount", 10)

	member, err := b.checkAdmin(i, discordgo.PermissionManageMessages)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionManageMessages)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Manage Messages permission to purge."))
		return
	}
	msgs, err := s.ChannelMessages(i.ChannelID, int(amount), "", "", "")
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Fetch Failed", "Failed to fetch messages: "+err.Error()))
		return
	}
	var ids []string
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		b.respondEphemeralEmbed(s, i, infoEmbed("No Messages", "No messages to delete."))
		return
	}
	if len(ids) == 1 {
		if err := s.ChannelMessageDelete(i.ChannelID, ids[0]); err != nil {
			b.respondEphemeralEmbed(s, i, errEmbed("Delete Failed", err.Error()))
			return
		}
	} else {
		if err := s.ChannelMessagesBulkDelete(i.ChannelID, ids); err != nil {
			b.respondEphemeralEmbed(s, i, errEmbed("Bulk Delete Failed", err.Error()))
			return
		}
	}
	b.respondEphemeralEmbed(s, i, successEmbed("🧹 Messages Purged", fmt.Sprintf("Deleted **%d** message(s).", len(ids))))
	b.logMod("Purge", i.ChannelID, member.User.ID, "", fmt.Sprintf("%d messages", len(ids)))
}

func (b *Bot) cmdKick(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	target := admin.UserOption(s, data.Options, "user")
	reason := admin.Reason(admin.StringOption(data.Options, "reason"))

	member, err := b.checkAdmin(i, discordgo.PermissionKickMembers)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	if target == nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "Please specify a user."))
		return
	}
	if target.ID == i.Member.User.ID {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "You cannot kick yourself."))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionKickMembers)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Kick Members permission."))
		return
	}
	if err := s.GuildMemberDeleteWithReason(i.GuildID, target.ID, reason); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Kick Failed", err.Error()))
		return
	}
	b.respondPublicEmbed(s, i, successEmbed("👢 Member Kicked", fmt.Sprintf("Kicked **%s**\nReason: %s", target.Username, reason)))
	b.logMod("Kick", i.ChannelID, member.User.ID, target.ID, reason)
}

func (b *Bot) cmdBan(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	target := admin.UserOption(s, data.Options, "user")
	reason := admin.Reason(admin.StringOption(data.Options, "reason"))
	deleteDays := int(admin.IntOption(data.Options, "delete_days", 0))

	member, err := b.checkAdmin(i, discordgo.PermissionBanMembers)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	if target == nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "Please specify a user."))
		return
	}
	if target.ID == i.Member.User.ID {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "You cannot ban yourself."))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionBanMembers)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Ban Members permission."))
		return
	}
	if err := s.GuildBanCreateWithReason(i.GuildID, target.ID, reason, deleteDays); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Ban Failed", err.Error()))
		return
	}
	b.respondPublicEmbed(s, i, successEmbed("🔨 Member Banned", fmt.Sprintf("Banned **%s**\nReason: %s", target.Username, reason)))
	b.logMod("Ban", i.ChannelID, member.User.ID, target.ID, reason)
}

func (b *Bot) cmdTimeout(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	target := admin.UserOption(s, data.Options, "user")
	minutes := admin.IntOption(data.Options, "minutes", 5)
	reason := admin.Reason(admin.StringOption(data.Options, "reason"))

	member, err := b.checkAdmin(i, discordgo.PermissionModerateMembers)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	if target == nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "Please specify a user."))
		return
	}
	if target.ID == i.Member.User.ID {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "You cannot timeout yourself."))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionModerateMembers)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Moderate Members (Timeout) permission."))
		return
	}
	until := admin.TimeoutUntil(minutes)
	if err := s.GuildMemberTimeout(i.GuildID, target.ID, until); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Timeout Failed", err.Error()))
		return
	}
	b.respondPublicEmbed(s, i, successEmbed("⏱️ Member Timed Out", fmt.Sprintf("Timed out **%s** for %d min\nReason: %s", target.Username, minutes, reason)))
	b.logMod("Timeout", i.ChannelID, member.User.ID, target.ID, reason)
}

func (b *Bot) cmdUntimeout(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	target := admin.UserOption(s, data.Options, "user")
	reason := admin.Reason(admin.StringOption(data.Options, "reason"))

	member, err := b.checkAdmin(i, discordgo.PermissionModerateMembers)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	if target == nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Invalid Target", "Please specify a user."))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionModerateMembers)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Moderate Members (Timeout) permission."))
		return
	}
	if err := s.GuildMemberTimeout(i.GuildID, target.ID, nil); err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Untimeout Failed", err.Error()))
		return
	}
	b.respondPublicEmbed(s, i, successEmbed("✅ Timeout Removed", fmt.Sprintf("Removed timeout from **%s**\nReason: %s", target.Username, reason)))
	b.logMod("Untimeout", i.ChannelID, member.User.ID, target.ID, reason)
}

func (b *Bot) cmdSlowmode(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	seconds := admin.IntOption(data.Options, "seconds", 0)

	member, err := b.checkAdmin(i, discordgo.PermissionManageChannels)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionManageChannels)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Manage Channels permission."))
		return
	}
	rateLimit := int(seconds)
	_, err = s.ChannelEdit(i.ChannelID, &discordgo.ChannelEdit{RateLimitPerUser: &rateLimit})
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Slowmode Failed", err.Error()))
		return
	}
	b.respondPublicEmbed(s, i, successEmbed("🐢 Slowmode Updated", fmt.Sprintf("Slowmode set to **%d** seconds.", seconds)))
	b.logMod("Slowmode", i.ChannelID, member.User.ID, "", fmt.Sprintf("%d seconds", seconds))
}

func (b *Bot) cmdLock(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.setLock(s, i, true)
}

func (b *Bot) cmdUnlock(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.setLock(s, i, false)
}

func (b *Bot) setLock(s *discordgo.Session, i *discordgo.InteractionCreate, lock bool) {
	member, err := b.checkAdmin(i, discordgo.PermissionManageChannels)
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Permission Denied", err.Error()))
		return
	}
	ok, err := admin.BotHasPermission(s, i.GuildID, i.ChannelID, discordgo.PermissionManageChannels)
	if err != nil || !ok {
		b.respondEphemeralEmbed(s, i, errEmbed("Bot Permission Required", "I need Manage Channels permission."))
		return
	}
	ch, err := s.State.Channel(i.ChannelID)
	if err != nil {
		ch, err = s.Channel(i.ChannelID)
		if err != nil {
			b.respondEphemeralEmbed(s, i, errEmbed("Channel Fetch Failed", err.Error()))
			return
		}
	}
	// Find existing @everyone overwrite or create a new one.
	var overwrites []*discordgo.PermissionOverwrite
	for _, ow := range ch.PermissionOverwrites {
		if ow.ID == i.GuildID {
			continue // skip existing @everyone; we'll re-add below
		}
		overwrites = append(overwrites, ow)
	}
	ow := &discordgo.PermissionOverwrite{
		ID:   i.GuildID,
		Type: discordgo.PermissionOverwriteTypeRole,
	}
	if lock {
		ow.Deny = discordgo.PermissionSendMessages
	} else {
		ow.Allow = discordgo.PermissionSendMessages
	}
	overwrites = append(overwrites, ow)
	_, err = s.ChannelEdit(i.ChannelID, &discordgo.ChannelEdit{PermissionOverwrites: overwrites})
	if err != nil {
		b.respondEphemeralEmbed(s, i, errEmbed("Lock/Unlock Failed", err.Error()))
		return
	}
	action := "Unlocked"
	if lock {
		action = "Locked"
	}
	b.respondPublicEmbed(s, i, successEmbed("🔒 Channel "+action, fmt.Sprintf("<#%s> has been %s.", i.ChannelID, strings.ToLower(action))))
	b.logMod(action, i.ChannelID, member.User.ID, "", "")
}

func (b *Bot) cmdUserInfo(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	user := admin.UserOption(s, data.Options, "user")
	if user == nil {
		if i.Member != nil && i.Member.User != nil {
			user = i.Member.User
		} else {
			b.respondEphemeralEmbed(s, i, errEmbed("Unknown User", "Could not determine user."))
			return
		}
	}
	var member *discordgo.Member
	if i.GuildID != "" {
		m, err := b.resolveMember(i.GuildID, user.ID)
		if err == nil {
			member = m
		}
	}
	embed := &discordgo.MessageEmbed{
		Title: "👤 User Info",
		Color: 0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "User", Value: user.Username + "#" + user.Discriminator, Inline: true},
			{Name: "ID", Value: user.ID, Inline: true},
			{Name: "Bot", Value: fmt.Sprintf("%v", user.Bot), Inline: true},
		},
	}
	if member != nil {
		if member.Nick != "" {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Nickname", Value: member.Nick, Inline: true})
		}
		if len(member.Roles) > 0 {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Roles", Value: strings.Join(member.Roles, ", "), Inline: false})
		}
		if !member.JoinedAt.IsZero() {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Joined", Value: member.JoinedAt.Format("2006-01-02 15:04"), Inline: true})
		}
	}
	if user.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: user.AvatarURL("128")}
	}
	b.respondEphemeralEmbed(s, i, embed)
}

func (b *Bot) cmdServerInfo(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" {
		b.respondEphemeralEmbed(s, i, errEmbed("Server Only", "This command can only be used in a server."))
		return
	}
	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		guild, err = s.Guild(i.GuildID)
		if err != nil {
			b.respondEphemeralEmbed(s, i, errEmbed("Fetch Failed", "Failed to fetch server: "+err.Error()))
			return
		}
	}
	memberCount := guild.MemberCount
	if memberCount == 0 {
		memberCount = len(guild.Members)
	}
	embed := &discordgo.MessageEmbed{
		Title: "🏠 Server Info",
		Color: 0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Server", Value: guild.Name, Inline: true},
			{Name: "ID", Value: guild.ID, Inline: true},
			{Name: "Owner", Value: fmt.Sprintf("<@%s>", guild.OwnerID), Inline: true},
			{Name: "Members", Value: fmt.Sprintf("%d", memberCount), Inline: true},
			{Name: "Channels", Value: fmt.Sprintf("%d", len(guild.Channels)), Inline: true},
			{Name: "Roles", Value: fmt.Sprintf("%d", len(guild.Roles)), Inline: true},
		},
	}
	if guild.PremiumTier > 0 {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Boost Level", Value: fmt.Sprintf("%d", guild.PremiumTier), Inline: true})
	}
	if guild.Icon != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: guild.IconURL("128")}
	}
	b.respondEphemeralEmbed(s, i, embed)
}
