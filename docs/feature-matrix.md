# ZoeyDCBot — Feature Matrix

Last updated: 2026-08-04 (Cycle 10)

## Status legend
- ✅ Implemented and tested
- ⚠️ Implemented but incomplete / unreliable / untested
- ❌ Not implemented (planned or proposed)
- 🚫 Not applicable / rejected by architecture decision

## AI / Chat

| Feature | Status | Notes |
|---------|--------|-------|
| Mention-to-chat (guild channels) | ✅ | `onMessageCreate` filters by mention |
| DM chat | ✅ | DMs treated as always-relevant |
| Streaming replies (SSE) | ✅ | First token → message, then ~1 edit/sec |
| Per-channel rolling context | ✅ | `memory.Store` with TTL eviction |
| `/clear` context reset | ✅ | Slash + prefix |
| System prompt (env configurable) | ✅ | `SYSTEM_PROMPT` |
| OpenAI-compatible backend | ✅ | OpenAI / DeepSeek / vLLM / Ollama |
| Reasoning token support | ✅ | `ai.Chunk.Reasoning` for DeepSeek R1 / o1 |
| Retry on transient (5xx) errors | ✅ | `LLM_MAX_RETRIES` |
| Request timeout | ✅ | `LLM_TIMEOUT_SECONDS` |
| Worker pool concurrency limit | ✅ | `MAX_WORKERS` + `QUEUE_SIZE` |
| `x!` prefix commands | ✅ | Routes to command handlers, not AI |
| Conversation persistence across restarts | ✅ | `internal/store/` SQLite — `DB_PATH` env var; optional, in-memory if empty |
| Per-user context (not per-channel) | ❌ | Not planned |
| Image/multimodal input | ❌ | Not planned |
| Tool calling / function calling | ❌ | Not planned |

## Moderation / Admin

| Feature | Status | Notes |
|---------|--------|-------|
| `/userinfo` | ✅ | Embed with join date, roles, etc. |
| `/serverinfo` | ✅ | Embed with member count, channels, etc. |
| `/purge <amount>` | ✅ | 1–100, permission-checked |
| `/kick` | ✅ | With reason, mod-log |
| `/ban` | ✅ | With reason + delete_days, mod-log |
| `/timeout` | ✅ | Up to 28 days, mod-log |
| `/untimeout` | ✅ | Mod-log |
| `/slowmode` | ✅ | Channel slowmode |
| `/lock` | ✅ | Lock @everyone send permission |
| `/unlock` | ✅ | Restore @everyone send permission |
| Permission checks (Discord perms + admin roles) | ✅ | `admin.HasPermission` |
| Bot permission checks | ✅ | `admin.BotHasPermission` |
| Mod-log channel | ✅ | `MOD_LOG_CHANNEL_ID` |
| `/warn` | ❌ | Not implemented |
| `/mute` (role-based, not timeout) | ❌ | Not implemented |
| `/unban` | ❌ | Not implemented |
| `/nuke` (channel recreate) | ❌ | Not implemented |
| Anti-spam / AutoMod | ✅ | `internal/automod/` — spam, link filter, word filter, role exemptions |
| Mod-log case numbering | ❌ | Not implemented |
| Message delete logging | ✅ | `onMessageDelete` — logs author, channel, content from State cache |
| Message edit logging | ✅ | `onMessageUpdate` — logs before/after content, jump link |

## Music

| Feature | Status | Notes |
|---------|--------|-------|
| `/play <query>` | ✅ | Resolves via yt-dlp, joins voice |
| `/queue` | ✅ | Rich embed, per-track ETA, total runtime, paginated (10/page) |
| `/nowplaying` | ✅ | Rich embed with thumbnail, progress bar |
| `/skip` | ✅ | Skips current track |
| `/stop` | ✅ | Stops + clears queue |
| `/pause` | ✅ | Fixed: no longer triggers auto-advance |
| `/resume` | ✅ | Fixed: no longer pops next track |
| `/leave` | ✅ | Disconnects + clears queue |
| `/volume` | ✅ | Per-guild volume |
| `/seek` | ❌ | Not implemented |
| `/shuffle` | ✅ | Fisher-Yates shuffle of queued tracks |
| `/loop <mode>` | ✅ | off / track (🔂) / queue (🔁); loop-aware `Advance()` |
| `/remove <position>` | ✅ | 1-based position, validated against queue bounds |
| `/move <from> <to>` | ✅ | Move track to a different queue position |
| `/jump <position>` | ❌ | Not implemented |
| `/forward` / `/rewind` | ❌ | Not implemented |
| `/lyrics` | ✅ | lrclib.net API, auto-uses current track, instrumental detection |
| `/history` (recently played) | ❌ | Not implemented |
| YouTube source | ✅ | Via yt-dlp |
| SoundCloud source | ✅ | Via yt-dlp (search priority: SC first) |
| Spotify track links | ✅ | Embed page scraping → YouTube search |
| Spotify album/playlist | ⚠️ | Code exists but unreliable (429 rate limits) |
| YouTube playlists | ✅ | Via `yt-dlp --flat-playlist` |
| SoundCloud sets | ✅ | Via `yt-dlp --flat-playlist` |
| Playlist expansion → individual queue entries | ✅ | `FetchPlaylist` → `AddMany` |
| Gap-less playback (pre-resolve next track) | ✅ | `PlayResolved` |
| All-music-commands-use-embeds | ✅ | Zero plain-text responses |
| Rich embeds (thumbnail, artist, duration, source icon) | ✅ | `embed.go` |
| Queue duration / ETA | ✅ | `FormatQueue` |
| Progress bar on Now Playing | ✅ | `embed.go` |
| Live Rich Presence (bot status = current track) | ❌ | `internal/presence/` not created |
| Voice channel auto-rename to track name | ❌ | `internal/channelrename/` not created |
| Now Playing selection UI (ambiguous matches) | ❌ | Not implemented |
| Lyrics sync | ❌ | Not planned |
| DJ role / music permission | ❌ | Not implemented |
| Vote skip | ❌ | Not implemented |
| Queue saving across restarts | ❌ | In-memory only |

## Infrastructure / Reliability

| Feature | Status | Notes |
|---------|--------|-------|
| Graceful shutdown (SIGINT/SIGTERM) | ✅ | `signal.NotifyContext` |
| Worker pool shutdown | ✅ | `shutdown` chan + `wg.Wait()` |
| Config fail-fast validation | ✅ | `config.Load` |
| Structured logging (slog) | ✅ | `internal/logging` |
| Log level configurable | ✅ | `LOG_LEVEL` |
| systemd unit | ✅ | `deploy/zoeydcbot.service` |
| Deploy script | ✅ | `deploy_zoey.sh` |
| Reconnect/resume on gateway disconnect | ⚠️ | discordgo handles reconnect, but no custom resume logic |
| Rate-limit-aware request handling | ⚠️ | discordgo has internal rate limiter, but no custom backoff |
| SQLite persistence | ✅ | `internal/store/` — conversations, music queue, settings; `modernc.org/sqlite` (pure Go) |
| Health check / readiness endpoint | ❌ | Not implemented |
| Metrics / observability | ❌ | Not implemented |
| Bot package tests | ❌ | `internal/bot/` has zero test files |
| Panic recovery in handlers | ✅ | `internal/recoverutil/` — Guard, GuardGo, Recover; all handlers + goroutines wrapped |
| Context deadline propagation | ⚠️ | AI uses context; music pipeline partially |

## UI / UX

| Feature | Status | Notes |
|---------|--------|-------|
| All embeds use context colors | ✅ | `embed.go` |
| Source icons in music embeds | ✅ | `SourceIcon()` |
| `/help` with all commands | ✅ | `commands.go` |
| `/version` | ✅ | `version.go` |
| `/ping` latency check | ✅ | `commands.go` |
| Ephemeral vs public responses | ✅ | Configured per command |
| Button components | ⚠️ | Fork supports types; only used in version footer |
| Select menu components | ⚠️ | Fork supports types; not used |
| Pagination (queue, help) | ⚠️ | Queue pagination implemented (10/page); help not paginated |
| Localization (multi-language) | ❌ | Not planned |
| Welcome/goodbye messages | ✅ | `onGuildMemberAdd`/`onGuildMemberRemove` — custom templates with {user}/{mention}/{server} placeholders, avatar, member count |
| Reaction roles | ✅ | `/reactionrole` creates emoji→role message; `/removerrole` removes it; `internal/roles/` Manager |
| Custom status / activity | ❌ | Fork supports `UpdateCustomStatus`; not used |
| Starboard | ✅ | `/starboard` configures channel+threshold; ⭐ reactions repost messages; `internal/starboard/` Engine |

## Open Items from Prior Work (Backlog)

These are known gaps from the prior build cycles that have NOT been completed:

1. **Live Rich Presence** — bot status shows current track; multi-guild rotation; debounce; idle cycling; custom status. `internal/presence/` package never created.
2. **Voice Channel Rename** — opt-in per-guild; debounce; rate-limit queue; crash-safe restore. `internal/channelrename/` package never created.
3. **SQLite persistence** — decided but not implemented. Would persist: queue, conversation context, mod-log, settings.
4. **Bot package tests** — `internal/bot/` has zero test files. Needs unit tests for interaction routing, prefix commands, embed helpers.
5. **Spotify album/playlist expansion** — code has fallback but doesn't work reliably (429 rate limits from Web API).
6. **Now Playing selection UI** — for ambiguous search matches, show a select menu to let user pick the right track.
7. ~~Panic recovery in handlers~~ — ✅ Done (Cycle 1). `internal/recoverutil/` wraps all gateway handlers and goroutines.
8. **Reconnect/resume robustness** — discordgo reconnects, but no custom logic for voice reconnection after gateway resume.
