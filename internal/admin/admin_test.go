package admin

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// makeMember builds a Member with the given permissions and roles for testing.
func makeMember(perm int64, roles []string) *discordgo.Member {
	return &discordgo.Member{
		Permissions: perm,
		Roles:        roles,
		User:         &discordgo.User{ID: "test-user"},
	}
}

// TestHasPermissionBitmask is the core table-driven test for §2.
// It verifies that the bitmask check uses &!= 0 (not ==), and that
// member.Permissions (from the interaction payload) is preferred over
// a cache lookup.
func TestHasPermissionBitmask(t *testing.T) {
	const req = discordgo.PermissionManageMessages

	cases := []struct {
		name     string
		member   *discordgo.Member
		perm     int64
		want     bool
		wantErr  bool
	}{
		{
			name:   "exactly the required permission",
			member: makeMember(req, nil),
			perm:   req,
			want:   true,
		},
		{
			name:   "required permission plus several unrelated ones",
			member: makeMember(req|discordgo.PermissionKickMembers|discordgo.PermissionChangeNickname|discordgo.PermissionViewChannel, nil),
			perm:   req,
			want:   true,
		},
		{
			name:   "administrator flag set (should pass any perm)",
			member: makeMember(discordgo.PermissionAdministrator, nil),
			perm:   req,
			want:   true,
		},
		{
			name:   "administrator plus many other perms",
			member: makeMember(discordgo.PermissionAdministrator|discordgo.PermissionKickMembers|discordgo.PermissionBanMembers, nil),
			perm:   req,
			want:   true,
		},
		{
			name:   "only unrelated permissions",
			member: makeMember(discordgo.PermissionKickMembers|discordgo.PermissionBanMembers, nil),
			perm:   req,
			want:   false,
		},
		{
			name:   "no permissions at all",
			member: makeMember(0, nil),
			perm:   req,
			// With nil session and member.Permissions==0, we fall through to
			// s.UserChannelPermissions which will panic on nil session.
			// This test verifies member.Permissions==0 doesn't short-circuit
			// incorrectly — but since we pass nil session, it will error.
			want:    false,
			wantErr: true,
		},
		{
			name:   "nil member",
			member: nil,
			perm:   req,
			want:   false,
		},
		{
			name:   "member with admin role",
			member: makeMember(0, []string{"admin-role-1"}),
			perm:   req,
			want:   true,
		},
		{
			name:   "member with non-admin role only",
			member: makeMember(0, []string{"some-other-role"}),
			perm:   req,
			// No admin role, no permissions, nil session → error on cache lookup.
			want:    false,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pass nil session — forces the member.Permissions path.
			// When member.Permissions==0, it will try s.UserChannelPermissions
			// on nil session and error.
			got, err := HasPermission(nil, "guild-1", "channel-1", tc.member, []string{"admin-role-1"}, tc.perm)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("HasPermission() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsAdministratorBitmask verifies the administrator check uses bitwise AND.
func TestIsAdministratorBitmask(t *testing.T) {
	cases := []struct {
		name   string
		member *discordgo.Member
		want   bool
	}{
		{
			name:   "exactly administrator permission",
			member: makeMember(discordgo.PermissionAdministrator, nil),
			want:   true,
		},
		{
			name:   "administrator plus many other perms",
			member: makeMember(discordgo.PermissionAdministrator|discordgo.PermissionKickMembers|discordgo.PermissionBanMembers|discordgo.PermissionManageChannels, nil),
			want:   true,
		},
		{
			name:   "non-administrator permissions only",
			member: makeMember(discordgo.PermissionKickMembers|discordgo.PermissionBanMembers, nil),
			want:   false,
		},
		{
			name:   "no permissions",
			member: makeMember(0, nil),
			want:   false,
		},
		{
			name:   "admin role member",
			member: makeMember(0, []string{"admin-role-1"}),
			want:   true,
		},
		{
			name:   "nil member",
			member: nil,
			want:   false,
		},
		{
			name:   "member with user but no perms and no admin role",
			member: &discordgo.Member{User: &discordgo.User{ID: "u1"}, Permissions: 0},
			want:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// nil session — IsAdministrator will check member.Permissions first,
			// then try cache lookup (which will fail gracefully on nil session).
			got := IsAdministrator(nil, "guild-1", "channel-1", tc.member, []string{"admin-role-1"})
			if got != tc.want {
				t.Errorf("IsAdministrator() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHasPermissionBitmaskNotEquality is a focused regression test for the
// specific bug: perms&perm == perm (equality) vs perms&perm != 0 (bitmask).
// A user with ManageMessages + KickMembers has a permission integer that
// is NOT equal to PermissionManageMessages, but the bitmask check should pass.
func TestHasPermissionBitmaskNotEquality(t *testing.T) {
	combined := int64(discordgo.PermissionManageMessages | discordgo.PermissionKickMembers)
	if combined == int64(discordgo.PermissionManageMessages) {
		t.Fatal("test setup invalid: combined perms should not equal the single flag")
	}
	member := makeMember(combined, nil)
	got, err := HasPermission(nil, "g", "c", member, nil, discordgo.PermissionManageMessages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("bitmask check failed: user with ManageMessages|KickMembers should have ManageMessages")
	}
}

// TestIsBotOwnerTableDriven tests the owner bypass.
func TestIsBotOwnerTableDriven(t *testing.T) {
	owners := []string{"owner-1", "owner-2"}
	cases := []struct {
		name   string
		userID string
		owners []string
		want   bool
	}{
		{"first owner", "owner-1", owners, true},
		{"second owner", "owner-2", owners, true},
		{"not an owner", "random-user", owners, false},
		{"empty user id", "", owners, false},
		{"empty owner list", "anyone", nil, false},
		{"empty user and empty owners", "", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsBotOwner(tc.userID, tc.owners)
			if got != tc.want {
				t.Errorf("IsBotOwner(%q, %v) = %v, want %v", tc.userID, tc.owners, got, tc.want)
			}
		})
	}
}

// TestHasAnyRoleTableDriven tests role checking.
func TestHasAnyRoleTableDriven(t *testing.T) {
	adminRoles := []string{"role-1", "role-2"}
	cases := []struct {
		name   string
		member *discordgo.Member
		roles  []string
		want   bool
	}{
		{"has first admin role", makeMember(0, []string{"role-1"}), adminRoles, true},
		{"has second admin role", makeMember(0, []string{"role-2"}), adminRoles, true},
		{"has non-admin role", makeMember(0, []string{"role-3"}), adminRoles, false},
		{"has admin role among others", makeMember(0, []string{"role-3", "role-1"}), adminRoles, true},
		{"no roles", makeMember(0, nil), adminRoles, false},
		{"nil member", nil, adminRoles, false},
		{"empty admin roles", makeMember(0, []string{"role-1"}), nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasAnyRole(tc.member, tc.roles)
			if got != tc.want {
				t.Errorf("HasAnyRole() = %v, want %v", got, tc.want)
			}
		})
	}
}
