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

## Backlog (future cycles)

- Cycle 5: AutoMod (spam protection, link filter, word filter)
- Cycle 6: Message logging (deleted/edited/purged)
- Cycle 7: Reaction roles
- Cycle 8: Welcome/goodbye messages
- Cycle 9: Starboard
- Cycle 10: SQLite persistence layer
- Cycle 11: Live Rich Presence
- Cycle 12: Voice channel rename
