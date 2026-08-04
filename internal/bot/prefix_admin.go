package bot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/admin"
)

// prefixAdminCheck verifies the user is an administrator or has the required
// permission for prefix-based admin commands. Returns the member if allowed.
func (b *Bot) prefixAdminCheck(s *discordgo.Session, m *discordgo.MessageCreate, perm int64) (*discordgo.Member, error) {
	if m.GuildID == "" {
		return nil, fmt.Errorf("this command can only be used in a server")
	}
	member, err := b.resolveMember(m.GuildID, m.Author.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve your member: %w", err)
	}
	// Bot owners bypass all permission checks.
	if admin.IsBotOwner(m.Author.ID, b.cfg.AdminUserIDs) {
		return member, nil
	}
	// Administrators or admin-role members bypass specific permission checks.
	if admin.IsAdministrator(s, m.GuildID, m.ChannelID, member, b.cfg.AdminRoleIDs) {
		return member, nil
	}
	ok, err := admin.HasPermission(s, m.GuildID, m.ChannelID, member, b.cfg.AdminRoleIDs, perm)
	if err != nil {
		return nil, fmt.Errorf("permission check failed: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("you don't have permission to use this command")
	}
	return member, nil
}

// parseUserID extracts a user ID from a mention (<@123>, <@!123>) or raw ID.
func parseUserID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<@")
	s = strings.TrimPrefix(s, "!")
	s = strings.TrimSuffix(s, ">")
	return s
}

func (b *Bot) handlePrefixPurge(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	amount := int64(10)
	if args != "" {
		if n, err := strconv.ParseInt(args, 10, 64); err == nil {
			amount = n
		}
	}
	if amount < 1 {
		amount = 1
	} else if amount > 100 {
		amount = 100
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageMessages); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	ok, err := admin.BotHasPermission(s, m.GuildID, m.ChannelID, discordgo.PermissionManageMessages)
	if err != nil || !ok {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Bot Missing Permission", "I need Manage Messages permission."))
		return
	}

	msgs, err := s.ChannelMessages(m.ChannelID, int(amount), "", "", "")
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Purge Failed", err.Error()))
		return
	}
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		ids = append(ids, msg.ID)
	}
	if err := s.ChannelMessagesBulkDelete(m.ChannelID, ids); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Purge Failed", err.Error()))
		return
	}
	b.logMod("Purge", m.ChannelID, m.Author.ID, "", fmt.Sprintf("%d messages", len(ids)))
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🧹 Purged", fmt.Sprintf("Deleted **%d** messages.", len(ids))))
}

func (b *Bot) handlePrefixKick(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing User", "Usage: `x!kick @user [reason]`"))
		return
	}
	userID := parseUserID(parts[0])
	reason := ""
	if len(parts) > 1 {
		reason = strings.Join(parts[1:], " ")
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionKickMembers); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	if err := s.GuildMemberDeleteWithReason(m.GuildID, userID, reason); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Kick Failed", err.Error()))
		return
	}
	b.logMod("Kick", m.ChannelID, m.Author.ID, userID, reason)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("👢 Kicked", fmt.Sprintf("Kicked <@%s>.", userID)))
}

func (b *Bot) handlePrefixBan(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing User", "Usage: `x!ban @user [reason] [delete_days]`"))
		return
	}
	userID := parseUserID(parts[0])
	reason := ""
	deleteDays := 0
	if len(parts) > 1 {
		if d, err := strconv.Atoi(parts[len(parts)-1]); err == nil && d >= 0 && d <= 7 {
			deleteDays = d
			if len(parts) > 2 {
				reason = strings.Join(parts[1:len(parts)-1], " ")
			}
		} else {
			reason = strings.Join(parts[1:], " ")
		}
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionBanMembers); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	if err := s.GuildBanCreateWithReason(m.GuildID, userID, reason, deleteDays); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Ban Failed", err.Error()))
		return
	}
	b.logMod("Ban", m.ChannelID, m.Author.ID, userID, reason)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🔨 Banned", fmt.Sprintf("Banned <@%s>.", userID)))
}

func (b *Bot) handlePrefixTimeout(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing Args", "Usage: `x!timeout @user <minutes> [reason]`"))
		return
	}
	userID := parseUserID(parts[0])
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 1 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Invalid Duration", "Minutes must be a positive number."))
		return
	}
	reason := ""
	if len(parts) > 2 {
		reason = strings.Join(parts[2:], " ")
	}
	if minutes > 43200 {
		minutes = 43200
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionModerateMembers); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	until := admin.TimeoutUntil(int64(minutes))
	if err := s.GuildMemberTimeout(m.GuildID, userID, until); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Timeout Failed", err.Error()))
		return
	}
	b.logMod("Timeout", m.ChannelID, m.Author.ID, userID, fmt.Sprintf("%d minutes — %s", minutes, reason))
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("⏸️ Timed Out", fmt.Sprintf("Timed out <@%s> for **%d** minutes.", userID, minutes)))
}

func (b *Bot) handlePrefixUntimeout(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Missing User", "Usage: `x!untimeout @user`"))
		return
	}
	userID := parseUserID(parts[0])

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionModerateMembers); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	if err := s.GuildMemberTimeout(m.GuildID, userID, nil); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Untimeout Failed", err.Error()))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("▶️ Untimeout", fmt.Sprintf("Removed timeout from <@%s>.", userID)))
}

func (b *Bot) handlePrefixSlowmode(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	seconds := 0
	if args != "" {
		if n, err := strconv.Atoi(args); err == nil {
			seconds = n
		}
	}
	if seconds < 0 {
		seconds = 0
	} else if seconds > 21600 {
		seconds = 21600
	}

	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	rateLimit := seconds
	_, err := s.ChannelEdit(m.ChannelID, &discordgo.ChannelEdit{RateLimitPerUser: &rateLimit})
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Slowmode Failed", err.Error()))
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🐌 Slowmode", fmt.Sprintf("Set slowmode to **%d** seconds.", seconds)))
}

func (b *Bot) handlePrefixLock(s *discordgo.Session, m *discordgo.MessageCreate) {
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	b.setPrefixLock(s, m, true)
}

func (b *Bot) handlePrefixUnlock(s *discordgo.Session, m *discordgo.MessageCreate) {
	if _, err := b.prefixAdminCheck(s, m, discordgo.PermissionManageChannels); err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Permission Denied", err.Error()))
		return
	}
	b.setPrefixLock(s, m, false)
}

// setPrefixLock locks/unlocks a channel for @everyone via prefix command.
func (b *Bot) setPrefixLock(s *discordgo.Session, m *discordgo.MessageCreate, lock bool) {
	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		ch, err = s.Channel(m.ChannelID)
		if err != nil {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Channel Fetch Failed", err.Error()))
			return
		}
	}
	var overwrites []*discordgo.PermissionOverwrite
	for _, ow := range ch.PermissionOverwrites {
		if ow.ID == m.GuildID {
			continue
		}
		overwrites = append(overwrites, ow)
	}
	ow := &discordgo.PermissionOverwrite{
		ID:   m.GuildID,
		Type: discordgo.PermissionOverwriteTypeRole,
	}
	if lock {
		ow.Deny = discordgo.PermissionSendMessages
	} else {
		ow.Allow = discordgo.PermissionSendMessages
	}
	overwrites = append(overwrites, ow)
	_, err = s.ChannelEdit(m.ChannelID, &discordgo.ChannelEdit{PermissionOverwrites: overwrites})
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Lock/Unlock Failed", err.Error()))
		return
	}
	action := "Unlocked"
	if lock {
		action = "Locked"
	}
	b.logMod(action, m.ChannelID, m.Author.ID, "", "")
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🔒 Channel "+action, fmt.Sprintf("<#%s> has been %s.", m.ChannelID, strings.ToLower(action))))
}

func (b *Bot) handlePrefixUserInfo(s *discordgo.Session, m *discordgo.MessageCreate, args string) {
	userID := m.Author.ID
	if args != "" {
		userID = parseUserID(strings.Fields(args)[0])
	}
	member, err := b.resolveMember(m.GuildID, userID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("User Not Found", err.Error()))
		return
	}
	embed := buildUserInfoEmbed(member.User, member)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

func (b *Bot) handlePrefixServerInfo(s *discordgo.Session, m *discordgo.MessageCreate) {
	guild, err := s.Guild(m.GuildID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Guild Error", err.Error()))
		return
	}
	embed := buildServerInfoEmbed(guild)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
}
