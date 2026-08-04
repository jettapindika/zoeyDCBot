// Package admin contains safe helpers for moderation/admin Discord commands.
package admin

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// IsBotOwner reports whether the given user ID is in the configured bot-owner
// list. Bot owners bypass all permission checks.
func IsBotOwner(userID string, adminUserIDs []string) bool {
	if userID == "" || len(adminUserIDs) == 0 {
		return false
	}
	for _, id := range adminUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

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

// IsGuildOwner reports whether the given user ID is the owner of the guild.
// Guild owners implicitly have all permissions. The guild is resolved from
// state cache first (owner ID never changes), then REST as fallback.
func IsGuildOwner(s *discordgo.Session, guildID, userID string) bool {
	if guildID == "" || userID == "" || s == nil {
		return false
	}
	var ownerID string
	if s.State != nil {
		if g, err := s.State.Guild(guildID); err == nil && g != nil {
			ownerID = g.OwnerID
		}
	}
	if ownerID == "" {
		if g, err := s.Guild(guildID); err == nil && g != nil {
			ownerID = g.OwnerID
		}
	}
	return ownerID == userID
}

// IsAdministrator reports whether the member has the Discord Administrator
// permission or is in the configured admin-roles list. This is a fast check
// for gating high-risk commands without needing a specific permission bit.
//
// The member's own Permissions field (populated by Discord in interaction
// payloads) is checked first — this is always fresher than a cache lookup.
// Only when that field is empty do we fall back to s.UserChannelPermissions.
func IsAdministrator(s *discordgo.Session, guildID, channelID string, member *discordgo.Member, adminRoleIDs []string) bool {
	if HasAnyRole(member, adminRoleIDs) {
		return true
	}
	if member == nil || member.User == nil {
		return false
	}
	// Guild owners implicitly have all permissions.
	if IsGuildOwner(s, guildID, member.User.ID) {
		return true
	}
	// member.Permissions is populated by Discord in interaction payloads —
	// always prefer it over a cache lookup that may be stale.
	if member.Permissions != 0 {
		return member.Permissions&discordgo.PermissionAdministrator != 0
	}
	// Fall back to resolved permissions only when member.Permissions is empty
	// (e.g. prefix commands resolved from gateway state). If we have no
	// session, we can't resolve permissions.
	if s == nil {
		return false
	}
	perms, err := s.UserChannelPermissions(member.User.ID, channelID)
	if err != nil && s.State != nil {
		perms, err = s.State.UserChannelPermissions(member.User.ID, channelID)
	}
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionAdministrator != 0
}

// HasPermission checks the member's guild/channel permissions, falling back to
// configured admin roles. Administrators and guild owners always pass Discord
// permission checks.
//
// The member's own Permissions field (populated by Discord in interaction
// payloads) is checked first — this is always fresher than a cache lookup.
// Only when that field is empty do we fall back to s.UserChannelPermissions.
func HasPermission(s *discordgo.Session, guildID, channelID string, member *discordgo.Member, adminRoleIDs []string, perm int64) (bool, error) {
	if HasAnyRole(member, adminRoleIDs) {
		return true, nil
	}
	if member == nil || member.User == nil {
		return false, nil
	}
	// Guild owners implicitly have all permissions.
	if IsGuildOwner(s, guildID, member.User.ID) {
		return true, nil
	}
	// member.Permissions is populated by Discord in interaction payloads.
	// Prefer it over a potentially stale cache lookup.
	if member.Permissions != 0 {
		if member.Permissions&discordgo.PermissionAdministrator != 0 {
			return true, nil
		}
		return member.Permissions&perm != 0, nil
	}
	// No member.Permissions — fall back to cache/REST. If we have no session,
	// we can't resolve permissions.
	if s == nil {
		return false, fmt.Errorf("no session available to resolve permissions")
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
	return perms&perm != 0, nil
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
