# discordgo Fork Diff — yeongaori/discordgo vs bwmarrin/discordgo v0.29.0

**Date:** 2026-08-03
**Fork:** `github.com/yeongaori/discordgo v0.0.0-20260307092356-fd09989565b3`
**Upstream:** `github.com/bwmarrin/discordgo v0.29.0`
**Purpose:** Document what the fork gives us beyond upstream, so Phase B agents
know what's already available before assuming something is missing.

## Summary

The fork is **ahead of upstream** in several important areas: DAVE (Discord
Audio/Video End-to-End Encryption) support, newer voice protocol (v8 with
ChaCha20-Poly1305 instead of NaCl secretbox), additional REST endpoints, new
component types (Label, FileUpload), and a new event type. It is not a
behavioral rewrite — all existing upstream types/functions remain compatible.

## New Files (fork only)

| File | Lines | Purpose |
|------|-------|---------|
| `dave.go` | 240 | DAVE protocol session management — E2E encryption for voice |
| `dave_crypto.go` | 80 | Cryptographic primitives for DAVE |
| `dave_mls.go` | 622 | Messaging Layer Security implementation for DAVE |

**What this means:** The fork supports Discord's DAVE protocol — the
end-to-end-encrypted voice protocol. Upstream discordgo v0.29.0 does not.
This is relevant if we ever need to handle encrypted voice, but for our
yt-dlp→ffmpeg→gopus pipeline it's not directly used.

## Changed Files — Key Differences

### `voice.go` — Voice protocol upgrade

- **Encryption:** Switched from `nacl/secretbox` (XSalsa20-Poly1305) to
  `chacha20poly1305` (ChaCha20-Poly1305 AEAD). This is the new Discord voice
  encryption standard.
- **Protocol version:** WebSocket gateway URL now includes `?v=8` (upstream
  uses no version parameter).
- **SecretKey:** Changed from `[32]byte` (fixed array) to `[]byte` (slice) to
  accommodate DAVE protocol keys of varying length.
- **DAVE support:** `VoiceConnection` struct has a new `dave *DAVESession`
  field and `seqAck int` for sequence number tracking.
- **op8:** New `voiceOP8` struct for voice operation 8 (HELLO) with
  `HeartbeatInterval`.
- **Removed:** `connected chan bool` field (was used for blocking until
  connected). Replaced by DAVE-aware connection flow.

### `structs.go` — New types and fields

- `MessagePin` struct — pinned message with timestamp and embedded `*Message`
- `ChannelMessagePinsList` struct — paginated pinned messages with `HasMore`
- `resumeGatewayURL` field on session state — for Discord resume gateway URL
- `ErrCodeUnknownVoiceState = 10065` — new error code

### `restapi.go` — New REST endpoints

| Function | Purpose |
|----------|---------|
| `GuildRole(guildID, roleID)` | Get a single role by ID (upstream only has list-all) |
| `GuildRoleMemberCounts(guildID)` | Map of role ID → member count (for reaction role management) |
| `ChannelMessagesPinned(channelID, before, limit)` | Paginated pinned messages with `before` timestamp filter and `limit` |
| `UserVoiceState(guildID, userID)` | Get a user's voice state via REST (not just via gateway state cache) |

Also: `GuildMembers` now sets `GuildID` on returned members (bug fix — upstream
leaves it empty).

### `interactions.go` — Renamed type + new field

- `MessageComponentInteractionDataResolved` → renamed to
  `ComponentInteractionDataResolved`
- Added `Attachments map[string]*MessageAttachment` to resolved data
- `ModalSubmitInteractionData` now has `Resolved` field

**⚠️ Breaking note:** The rename from
`MessageComponentInteractionDataResolved` to
`ComponentInteractionDataResolved` is a type rename. Our code doesn't
reference this type directly, so it's safe, but agents should be aware.

### `components.go` — New component types

- `LabelComponent` (type 18) — top-level layout component that wraps modal
  components with text labels
- `FileUploadComponent` (type 19) — file upload component
- `Label` struct — full implementation with `UnmarshalJSON`
- `File` struct — file upload component implementation
- `SelectMenuOption.Required` changed from `bool` to `*bool` (pointer, for
  omitempty)
- `TextInput.Required` changed from `bool` to `*bool` (pointer, for
  omitempty)
- Fixed typo: `spoiler,omitemoty` → `spoiler,omitempty` in `Attachment`
- `SelectMenu.Values` field — populated when receiving interaction responses

### `eventhandlers.go` — New event

- `MESSAGE_REACTION_REMOVE_EMOJI` event type + handler
- `MessageReactionRemoveEmoji` event — fired when all reactions of a specific
  emoji are removed from a message

**Relevance:** Useful for reaction role cleanup — if someone removes all
  reactions of an emoji, we can clean up the associated role assignment.

### `endpoints.go` — New endpoint constants

- `EndpointGuildRole(guildID, roleID)` — single role endpoint
- `EndpointGuildRoleMemberCounts(guildID)` — role member counts
- `EndpointChannelMessagesPins(channelID)` — pins (changed return type)

### `state.go` — Minor

- Resume gateway URL support in state tracking.

### `wsapi.go` — Minor

- Resume gateway URL handling in WebSocket session.

## What This Means for Phase B

1. **Reaction roles:** The fork gives us `GuildRoleMemberCounts` (for tracking
   how many members have a role) and `MessageReactionRemoveEmoji` (for cleanup
   when all reactions of an emoji are removed). Both are useful for building
   reaction role management.

2. **Select menus / modals:** The fork has `Label` and `FileUpload` component
   types beyond upstream. `SelectMenu.Values` is now populated on interaction
   responses, which helps with the A1 selection UI.

3. **Voice:** DAVE/ChaCha20-Poly1305 support means the fork is compatible with
   Discord's latest voice protocol. Our ffmpeg→gopus pipeline doesn't need to
   change — the encryption is handled at the transport layer by discordgo.

4. **Pinned messages:** Paginated pin retrieval with `before`/`limit` is
   available if we need it for channel management features.

5. **No AutoMod types:** The fork does **not** add AutoMod types. If we need
   AutoMod (§5 of the build prompt), we'll need to either use REST calls
   directly or extend the fork. discordgo upstream has some AutoMod support in
   newer versions; we should check if the fork cherry-picked it.

6. **No button/select-menu builder helpers:** The fork adds component *types*
   but not ergonomic builders. We'll build our own helpers in `internal/bot/`.
