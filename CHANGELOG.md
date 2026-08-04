# ZoeyDCBot — Changelog

A running log of the continuous improvement loop. One entry per cycle.
Read `docs/feature-matrix.md` for the full feature inventory and status.

---

## Cycle 1 — 2026-08-04

**Researched:** Surveyed Carl-bot, Dyno, MEE6, Jockie Music, and Go Discord bot reliability patterns. Catalogued feature gaps across music (loop/repeat, shuffle, seek, remove/move in queue, lyrics, DJ role, vote skip), moderation (warn, unban, AutoMod, mod-log case numbering), and infrastructure (panic recovery, voice reconnection, health checks). Prioritized panic recovery as highest-impact, self-contained, and testable.

**Implemented:** Panic recovery across all gateway handlers and spawned goroutines.
- Created `internal/recoverutil/` package with `Guard()`, `GuardGo()`, `Recover()`, and `Handler2()` wrappers
- `Guard(label, fn)` — runs fn with deferred panic recovery; for goroutines
- `GuardGo(label, fn)` — launches fn in a new goroutine with Guard
- `Recover(label)` — deferred-only helper for event handlers (`defer recoverutil.Recover("onMessageCreate")`)
- Wrapped all 3 gateway handlers: `onReady`, `onMessageCreate`, `onInteractionCreate`
- Wrapped all 7 goroutine launch sites: 3× `tryStartPlayback` + 1× `replayCurrent` in bot.go, 2× `tryStartPlayback` + 1× `replayCurrent` in music_commands.go
- Wrapped worker pool goroutines with `GuardGo`
- 9 unit tests including 50-pass loop tests for `Guard` and `GuardGo` under concurrency

**Verification level:** 50-pass race detector
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages)
- `go test -race ./…` ✅ (full suite)
- `go test -race -count=50 ./internal/recoverutil/` ✅ (0 failures)
- `go test -race -count=50 ./internal/music/` ✅ (0 failures)

---

## Cycle 2 — 2026-08-04

**Researched:** Catalogued music bot features from Jockie, FredBoat, and Rythm — loop/repeat (track + queue), shuffle, remove-from-queue, move-in-queue, lyrics, DJ role, vote skip, seek, audio filters. Prioritized loop, shuffle, and remove as the highest-value, self-contained queue management features.

**Implemented:** Three new music queue management commands.
- `/loop <mode>` — cycles repeat mode: `off` (➡️), `track` (🔂), `queue` (🔁). Added `LoopMode` type and `LoopOff`/`LoopTrack`/`LoopQueue` constants to `music.Manager`. New `Advance()` method handles loop-aware track transitions: LoopTrack replays the current track, LoopQueue rotates the queue, LoopOff pops the next track. `Stop()` now resets loop mode.
- `/shuffle` — Fisher-Yates shuffle of the queued tracks (not the currently playing one). Uses a per-Manager `rand.Rand` to avoid global lock contention. Returns count of shuffled tracks.
- `/remove <position>` — removes a track at a 1-based position from the queue. Validates position bounds, returns the removed track name in the confirmation embed.
- Refactored `advanceOrFinish` to check loop mode before deciding to auto-advance or stop. `tryStartPlayback` now calls `Advance()` instead of `StartNext()` so loop logic is centralized.

**Verification level:** 50-pass race detector
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages)
- `go test -race -count=50 ./internal/music/` ✅ (0 failures)

---

## Cycle 3 — 2026-08-04

**Researched:** Evaluated keyless lyrics APIs. lrclib.net provides a free, no-key REST API with both plain and synced (LRC) lyrics, CORS-enabled, rate-limited but generous. Tested with `GET /api/search?track_name=Bohemian+Rhapsody&artist_name=Queen` — returns JSON array with `plainLyrics`, `syncedLyrics`, `instrumental`, `albumName`, `duration`.

**Implemented:** `/lyrics` command.
- Created `internal/lyrics/` package with `Client.Search(ctx, track, artist)` querying lrclib.net
- `/lyrics [query]` — if no query provided, uses the currently playing track's title + artist
- Shows lyrics in a green embed with artist — track title, album in footer
- Handles instrumental tracks (shows "🎼 This track is instrumental")
- Truncates lyrics to 4000 chars (Discord embed limit is 4096) with ellipsis
- Acknowledges interaction immediately, then follows up with results (avoids 3-second timeout)

**Verification level:** Build + vet + test
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages)

---

## Cycle 4 — 2026-08-04

**Researched:** Queue pagination and move-in-queue are standard features in Jockie, FredBoat, and Rythm. The existing `FormatQueue` showed only 10 tracks with "…and N more" — no way to see beyond the first page.

**Implemented:** Queue pagination + `/move` command.
- `FormatQueue` now accepts a `page` parameter (10 tracks per page). Shows "page X/Y" in the footer when multiple pages exist. ETA calculation accounts for tracks before the current page.
- `/queue [page]` — optional integer page parameter, defaults to 1
- `/move <from> <to>` — moves a track from one 1-based position to another. Added `Manager.Move()` method with bounds validation. Both positions must be valid and different.

**Verification level:** Build + vet + test
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages)

---

## Cycle 5 — 2026-08-04

**Researched:** Surveyed AutoMod features in Carl-bot, Dyno, and MEE6: spam detection (message rate limiting), link filtering (with domain/role allowlists), word filtering (banned words with whole-word matching), and exemption roles. Designed a per-guild rules engine configurable via env vars.

**Implemented:** AutoMod — spam protection, link filter, word filter.
- Created `internal/automod/` package with `Engine`, `Rules`, `Violation`, and `Action` types
- **Spam detection**: rate-limits messages per user (configurable max messages in N seconds). Sliding window with mutex-protected tracking. Action: warn or mute (timeout).
- **Link filter**: regex-based URL detection including bare domains (.com, .gg, .io, etc.). Domain allowlist and role exemptions. Action: warn or mute.
- **Word filter**: case-insensitive whole-word matching (not substring). Banned words from env. Action: warn or mute.
- **Role exemptions**: roles in `AUTOMOD_EXEMPT_ROLES` bypass all rules. Link filter has its own `AUTOMOD_LINK_ALLOW` domain allowlist.
- Wired into `onMessageCreate` — checks before any other processing. Deletes offending message, sends warning embed, applies timeout if configured, logs to mod-log channel.
- 9 unit tests covering spam detection, link filter (with allowlist/exempt roles), word filter (whole-word matching), exempt role bypass, empty content, and spam reset.
- Config: `AUTOMOD_ENABLED`, `AUTOMOD_SPAM_MAX`, `AUTOMOD_SPAM_WINDOW`, `AUTOMOD_SPAM_ACTION`, `AUTOMOD_SPAM_MUTE_MIN`, `AUTOMOD_LINK_FILTER`, `AUTOMOD_LINK_ACTION`, `AUTOMOD_LINK_ALLOW`, `AUTOMOD_WORD_FILTER`, `AUTOMOD_WORD_ACTION`, `AUTOMOD_BANNED_WORDS`, `AUTOMOD_EXEMPT_ROLES`

**Verification level:** Build + vet + test
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages, including 9 new automod tests)

---

## Cycle 6 — 2026-08-04

**Researched:** Surveyed message logging features in Dyno, Carl-bot, and MEE6. Standard features: deleted message logging (with original content + author), edited message logging (before/after content), and bulk delete logging. Discord's gateway fires `MessageDelete` for individual and bulk deletes, and `MessageUpdate` for edits. `discordgo` State cache stores recent messages if `StateEnabled` (default true), enabling recovery of original content for deleted messages.

**Implemented:** Message logging — deleted and edited messages logged to mod-log channel.
- Created `internal/bot/message_log.go` with `onMessageDelete` and `onMessageUpdate` handlers
- **Deleted messages**: logs author (from State cache if available), channel, message ID, and original content. Falls back to "(not cached)" when the message wasn't in State cache.
- **Edited messages**: logs author, channel, jump link, and before/after content. Skips bot messages and edits where content didn't change (e.g. embed-only updates).
- Both handlers wrapped with `recoverutil.Recover` for panic safety
- Registered in `New()` alongside existing gateway handlers
- No new config required — uses existing `MOD_LOG_CHANNEL_ID` (`b.cfg.ModLogChannel`)
- `truncateText` helper clamps content to 1024 chars (Discord embed field limit) with ellipsis

**Verification level:** Build + vet + test
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages)

---

## Cycle 7 — 2026-08-04

**Researched:** Reaction roles are a core feature in Carl-bot, Dyno, and MEE6. A message is posted with emoji-role bindings; users react to get roles and unreact to lose them. Discord gateway fires `MessageReactionAdd` and `MessageReactionRemove` events. The discordgo fork supports both events including `MessageReactionRemoveEmoji` (bulk emoji removal). Requires `IntentsGuildMessageReactions` intent.

**Implemented:** Reaction roles — `/reactionrole` and `/removerrole` commands.
- Created `internal/roles/` package with `Manager`, `Message`, `Binding` types
- `Manager` stores emoji→role bindings per message ID in-memory (concurrency-safe with `sync.RWMutex`)
- `/reactionrole <description> <bindings>` — creates an embed with the description and emoji→role list, posts it, reacts with each emoji, and registers the message with the manager. Parses `emoji:@role` or `emoji:roleID` format. Validates role existence via State. Max 20 bindings per message.
- `/removerrole <message_id>` — deletes the reaction-role message and unregisters it
- `onReactionAdd` handler: looks up the message, finds the role for the emoji, calls `GuildMemberRoleAdd`. Ignores bot's own reactions.
- `onReactionRemove` handler: reverse of add — removes the role
- Both handlers wrapped with `recoverutil.Recover`
- Added `IntentsGuildMessageReactions` to `DefaultIntents`
- Both commands require `ManageRoles` permission (user + bot)
- 4 unit tests including 50-pass concurrent access test

**Verification level:** 50-pass race detector
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test ./…` ✅ (all packages)
- `go test -race ./…` ✅ (full suite)
- `go test -race -count=50 ./internal/roles/` ✅ (0 failures)

---

## Cycle 8 — 2026-08-04

**Researched:** Welcome/goodbye messages are standard in Dyno, Carl-bot, and MEE6. They fire on `GuildMemberAdd` and `GuildMemberRemove` gateway events, which require `IntentsGuildMembers` (a privileged intent). Features: custom message templates with placeholders ({user}, {mention}, {server}), avatar thumbnail, member count, configurable channel.

**Implemented:** Welcome and goodbye messages.
- Created `internal/bot/welcome.go` with `onGuildMemberAdd` and `onGuildMemberRemove` handlers
- **Welcome**: sends an embed with the member's avatar, custom message, and member count footer. Uses `WELCOME_MESSAGE` env var with `{user}`, `{mention}`, `{server}` placeholders.
- **Goodbye**: sends an embed with the leaving member's avatar and custom message. Uses `GOODBYE_MESSAGE` env var with same placeholders.
- Both handlers wrapped with `recoverutil.Recover` for panic safety
- Added `IntentsGuildMembers` to `DefaultIntents` (privileged intent — must be enabled in Discord Developer Portal)
- Config: `WELCOME_ENABLED`, `WELCOME_CHANNEL_ID`, `WELCOME_MESSAGE`, `GOODBYE_ENABLED`, `GOODBYE_MESSAGE`
- Updated `.env.example` with new env vars and placeholder documentation

**Verification level:** Build + vet + test
- `go build ./…` ✅
- `go vet ./…` ✅
- `go test -race ./…` ✅ (all packages)

---

## Backlog (future cycles)

- Cycle 9: Starboard
- Cycle 10: SQLite persistence layer
- Cycle 11: Live Rich Presence
- Cycle 12: Voice channel rename
