# ZoeyDCBot

Discord AI bot with fast, streaming replies — now a full-featured **AI assistant + administrator + music bot**.

## Stack

- Go 1.26 + [discordgo](https://github.com/bwmarrin/discordgo)
- OpenAI-compatible streaming chat completions (OpenAI / DeepSeek / vLLM /
  Ollama — switch via env)
- In-memory per-channel rolling context (configurable TTL)
- Moderation commands with permission checks and mod-log
- Music queue management with voice-channel join

## Layout

```
cmd/zoeydcbot/        entry point
internal/config/      env config, fail-fast validation
internal/bot/         gateway wiring, pipeline, slash commands, streaming
  bot.go              core bot, AI pipeline, interaction router
  commands.go         command registration + utility commands (/ping /help /clear)
  admin_commands.go   moderation commands (/kick /ban /purge /timeout …)
  music_commands.go   music commands (/play /queue /skip /stop …)
internal/ai/          OpenAI-compatible SSE streaming client
internal/memory/      per-channel rolling history with TTL
internal/admin/       permission-check helpers, option parsers, safe clamps
internal/music/       per-guild music queue manager (concurrency-safe)
internal/player/     yt-dlp resolution + ffmpeg→Opus streaming to Discord voice
deploy/               systemd unit
```

## Setup

1. Create a bot at <https://discord.com/developers/applications>.
   - Enable **Message Content Intent** (Bot → Privileged Gateway Intents).
   - Enable **Server Members Intent** (recommended for admin commands).
   - Invite it to your server with `applications.commands` and `bot` scopes.
2. `cp .env.example .env` and fill in `DISCORD_BOT_TOKEN`, `LLM_BASE_URL`,
   `LLM_API_KEY`, `LLM_MODEL`.
3. Build & run:

```sh
go build -o zoeydcbot ./cmd/zoeydcbot
./zoeydcbot
```

4. In Discord: `/ping` for latency, `/help` for usage, `/clear` to reset a
   channel's context. Mention the bot in a channel or DM it to chat.

## Slash Commands

### AI
| Command | Description |
|---------|-------------|
| `/ping` | Bot latency check |
| `/help` | Show all commands |
| `/clear` | Reset this channel's AI conversation context |

Just **mention** the bot or **DM** it to chat. Replies stream in — first token fast, then edits ~1/sec.

### Admin / Moderation
| Command | Description |
|---------|-------------|
| `/userinfo [user]` | Show information about a user |
| `/serverinfo` | Show information about this server |
| `/purge <amount>` | Delete recent messages (1–100) |
| `/kick <user> [reason]` | Kick a member |
| `/ban <user> [reason] [delete_days]` | Ban a member |
| `/timeout <user> <minutes> [reason]` | Timeout a member (up to 28 days) |
| `/untimeout <user> [reason]` | Remove timeout |
| `/slowmode <seconds>` | Set channel slowmode |
| `/lock` | Lock channel for @everyone |
| `/unlock` | Unlock channel for @everyone |

Admin commands require the corresponding Discord permission (e.g. Manage Messages for `/purge`) **or** a role listed in `ADMIN_ROLE_IDS`. All actions are logged to `MOD_LOG_CHANNEL_ID` if set.

### Music
| Command | Description |
|---------|-------------|
| `/play <query>` | Queue a song and join your voice channel |
| `/queue` | Show the music queue |
| `/nowplaying` | Show the current track |
| `/skip` | Skip the current track |
| `/stop` | Stop playback and clear the queue |
| `/pause` | Pause playback |
| `/resume` | Resume playback |
| `/leave` | Leave voice and clear the queue |

Music commands require `MUSIC_ENABLED=true`. The bot joins the voice channel you're in when you use `/play`. Queue is capped at `MUSIC_MAX_QUEUE` tracks per guild.

> **Audio playback** is powered by `yt-dlp` (source resolution) + `ffmpeg` (transcoding to Opus) + `gopus` (Opus encoding). Both `yt-dlp` and `ffmpeg` must be installed on the host. Spotify links are supported — the bot scrapes track metadata and searches YouTube for an equivalent stream. The `internal/player` package handles resolution and streaming.
>
> **All music responses use rich embeds** — track titles are clickable links to the source page, with thumbnail, artist, duration, and source (YouTube/SoundCloud/Spotify) shown inline. Playlists (YouTube, SoundCloud sets, Spotify albums/playlists) are expanded into individual queue entries, each with its own duration. The queue embed shows a per-track ETA and total expected runtime.
>
> **Gap-free playback** — when you `/play` a single track, the bot resolves the stream URL *before* starting the player, then passes it directly to ffmpeg via `PlayResolved`. This eliminates the audible gap between the "Now Playing" message and the first audio frame.

## Configuration

All settings come from environment variables (or `.env`):

| Variable | Default | Description |
|----------|---------|-------------|
| `DISCORD_BOT_TOKEN` | — | **Required.** Bot token. |
| `COMMAND_GUILD_ID` | — | Register commands to one guild (fast iteration). |
| `LLM_BASE_URL` | — | **Required.** OpenAI-compatible API base URL. |
| `LLM_API_KEY` | — | **Required.** API key. |
| `LLM_MODEL` | — | **Required.** Model name. |
| `SYSTEM_PROMPT` | built-in | System prompt for the AI. |
| `ALLOWED_CHANNEL_IDS` | all | Comma-separated channel IDs to respond in. |
| `CONTEXT_TURNS` | 20 | Rolling history turns per channel. |
| `HISTORY_TTL_MINUTES` | 30 | Minutes before inactive channel history is evicted. |
| `MAX_WORKERS` | 4 | Concurrent LLM generations. |
| `QUEUE_SIZE` | 128 | Pending LLM requests. |
| `EDIT_INTERVAL_MS` | 1000 | Min ms between streaming message edits. |
| `LLM_TIMEOUT_SECONDS` | 120 | LLM request timeout. |
| `LLM_MAX_RETRIES` | 1 | Retries for transient (5xx) errors. |
| `ADMIN_ROLE_IDS` | — | Comma-separated role IDs for admin commands. |
| `MOD_LOG_CHANNEL_ID` | — | Channel for moderation audit log. |
| `MUSIC_ENABLED` | true | Enable music commands. |
| `MUSIC_MAX_QUEUE` | 50 | Max queued tracks per guild. |

## Deploy

```sh
sudo cp deploy/zoeydcbot.service /etc/systemd/system/
sudo cp .env /etc/zoeydcbot.env   # or fill a fresh copy
sudo systemctl daemon-reload && sudo systemctl enable --now zoeydcbot
journalctl -u zoeydcbot -f        # logs
```

## Test

```sh
go test ./...
go vet ./...
```

Unit tests cover: SSE streaming (TTFT timing, retry on 5xx, no-retry on 4xx,
context cancellation), config validation, history trimming/eviction, and
music queue lifecycle (add, max-queue enforcement, skip, stop, guild isolation).
