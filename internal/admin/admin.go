// Package admin contains safe helpers for moderation/admin Discord commands.
package admin

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// HasAnyRole reports whether a member has at least one configured admin role.
func HasAnyRole(member *discordgo.Member, roleIDs []string) bool {
	if member == nil || len(roleIDs) == 0 {
		return false
	}
	want := make(map[string]struct{}, len(roleIDs))
	for _, id := range roleIDs {
		want[id] = struct{}{}
	}
	for _, id := range member.Roles {
		if _, ok := want[id]; ok {
			return true
		}
	}
	return false
}

// HasPermission checks the member's guild/channel permissions, falling back to
// configured admin roles. Administrators always pass Discord permission checks.
func HasPermission(s *discordgo.Session, guildID, channelID string, member *discordgo.Member, adminRoleIDs []string, perm int64) (bool, error) {
	if HasAnyRole(member, adminRoleIDs) {
		return true, nil
	}
	if member == nil || member.User == nil {
		return false, nil
	}
	perms, err := s.UserChannelPermissions(member.User.ID, channelID)
	if err != nil {
		// Fall back to state-based channel permissions if REST lookup fails.
		if s.State != nil {
			perms, err = s.State.UserChannelPermissions(member.User.ID, channelID)
			if err != nil {
				return false, err
			}
		} else {
			return false, err
		}
	}
	if perms&discordgo.PermissionAdministrator != 0 {
		return true, nil
	}
	return perms&perm == perm, nil
}

// BotHasPermission checks bot permissions in a channel.
func BotHasPermission(s *discordgo.Session, guildID, channelID string, perm int64) (bool, error) {
	if s.State == nil || s.State.User == nil {
		return false, fmt.Errorf("bot user is not ready")
	}
	perms, err := s.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		if s.State != nil {
			perms, err = s.State.UserChannelPermissions(s.State.User.ID, channelID)
			if err != nil {
				return false, err
			}
		} else {
			return false, err
		}
	}
	if perms&discordgo.PermissionAdministrator != 0 {
		return true, nil
	}
	return perms&perm == perm, nil
}

// Option helpers keep interaction handlers compact.
func StringOption(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, o := range opts {
		if o.Name == name {
			return o.StringValue()
		}
	}
	return ""
}

func IntOption(opts []*discordgo.ApplicationCommandInteractionDataOption, name string, def int64) int64 {
	for _, o := range opts {
		if o.Name == name {
			return o.IntValue()
		}
	}
	return def
}

func UserOption(s *discordgo.Session, opts []*discordgo.ApplicationCommandInteractionDataOption, name string) *discordgo.User {
	for _, o := range opts {
		if o.Name == name {
			return o.UserValue(s)
		}
	}
	return nil
}

func Reason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "No reason provided"
	}
	if len(reason) > 450 {
		return reason[:450] + "…"
	}
	return reason
}

func Clamp(n, min, max int64) int64 {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func TimeoutUntil(minutes int64) *time.Time {
	m := Clamp(minutes, 1, 40320) // Discord max timeout is 28 days.
	t := time.Now().Add(time.Duration(m) * time.Minute)
	return &t
}
