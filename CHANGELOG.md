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

**Deferred / proposed:** (future cycles)
- Cycle 2: `/loop` `/repeat` (track + queue), `/shuffle`, `/remove <position>`
- Cycle 3: `/lyrics`
- Cycle 4: Queue pagination
- Cycle 5: AutoMod (spam protection, link filter, word filter)
- Cycle 6: Message logging (deleted/edited/purged)
- Cycle 7: Reaction roles
- Cycle 8: Welcome/goodbye messages
- Cycle 9: Starboard
- Cycle 10: SQLite persistence layer
- Cycle 11: Live Rich Presence
- Cycle 12: Voice channel rename
