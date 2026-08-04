// Package bot wires the Discord gateway to the AI pipeline.
//
// Message flow mirrors Antares: the gateway handler never blocks on the LLM.
// On a mention/DM we fire the typing indicator immediately, enqueue the
// request into a worker pool, and the worker streams the reply back — the
// first message is sent as soon as the first token arrives, then edited with
// each following chunk (~1 edit/sec to stay under Discord's edit rate limit).
package bot

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/ai"
	"github.com/jettapindika/zoeyDCBot/internal/automod"
	"github.com/jettapindika/zoeyDCBot/internal/config"
	"github.com/jettapindika/zoeyDCBot/internal/logging"
	"github.com/jettapindika/zoeyDCBot/internal/lyrics"
	"github.com/jettapindika/zoeyDCBot/internal/memory"
	"github.com/jettapindika/zoeyDCBot/internal/music"
	"github.com/jettapindika/zoeyDCBot/internal/player"
	"github.com/jettapindika/zoeyDCBot/internal/roles"
	"github.com/jettapindika/zoeyDCBot/internal/starboard"
	"github.com/jettapindika/zoeyDCBot/internal/store"
	"github.com/jettapindika/zoeyDCBot/internal/recoverutil"
)

// DefaultIntents mirrors Antares: guild messages + message content + DMs.
const DefaultIntents = discordgo.IntentsGuildMessages |
	discordgo.IntentsMessageContent |
	discordgo.IntentsDirectMessages |
	discordgo.IntentsGuildVoiceStates |
	discordgo.IntentsGuildMessageReactions |
	discordgo.IntentsGuildMembers

// Bot is the top-level Discord+AI application.
type Bot struct {
	cfg     *config.Config
	sess    *discordgo.Session
	ai      *ai.Client
	history *memory.Store
	music   *music.Manager
	player  *player.Player
	lyrics  *lyrics.Client
	automod *automod.Engine
	roles     *roles.Manager
	starboard *starboard.Engine
	store    *store.Store

	queue    chan job
	shutdown chan struct{}
	wg       sync.WaitGroup

	voiceMu sync.Mutex
	voice   map[string]*discordgo.VoiceConnection // guildID -> voice connection
	startedAt time.Time
}

type job struct {
	guildID   string
	channelID string
	userName  string
	content   string
}

// New builds the bot and configures handlers. It does not open the gateway
// yet — slash commands are registered in Run() once the gateway is connected
// (State.User is only populated after Ready).
func New(cfg *config.Config) (*Bot, error) {
	sess, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	sess.Identify.Intents = DefaultIntents

	// Open SQLite store for persistence (optional — bot works without it).
	var db *store.Store
	if cfg.DBPath != "" {
		if s, err := store.Open(cfg.DBPath); err != nil {
			logging.Component("bot").Warn("sqlite store failed to open, continuing without persistence", "err", err)
		} else {
			db = s
		}
	}

	b := &Bot{
		cfg:      cfg,
		sess:     sess,
		ai:       ai.New(ai.Config{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, MaxRetries: cfg.LLMMaxRetries, Timeout: time.Duration(cfg.LLMTimeoutSeconds) * time.Second}),
		history:  memory.NewStore(cfg.ContextTurns, time.Duration(cfg.HistoryTTLMin)*time.Minute),
		music:    music.NewManager(cfg.MusicMaxQueue),
		player:   player.New(cfg.YtdlpPath, cfg.FfmpegPath),
		lyrics:   lyrics.New(),
		roles:     roles.New(),
		starboard: starboard.New(cfg.StarboardThreshold, "⭐"),
		store:    db,
		automod: automod.New(automod.Rules{
			SpamEnabled:       cfg.AutoModEnabled && cfg.AutoModSpamMax > 0,
			SpamMaxMessages:   cfg.AutoModSpamMax,
			SpamWindowSeconds: cfg.AutoModSpamWindow,
			SpamAction:        parseAction(cfg.AutoModSpamAction, automod.ActionWarn),
			SpamMuteMinutes:   cfg.AutoModSpamMuteMin,
			LinkEnabled:       cfg.AutoModEnabled && cfg.AutoModLinkFilter,
			LinkAllowedDomains: cfg.AutoModLinkAllow,
			LinkAction:        parseAction(cfg.AutoModLinkAction, automod.ActionWarn),
			WordFilterEnabled: cfg.AutoModEnabled && cfg.AutoModWordFilter,
			BannedWords:       cfg.AutoModBannedWords,
			WordAction:        parseAction(cfg.AutoModWordAction, automod.ActionWarn),
			ExemptRoles:       cfg.AutoModExemptRoles,
		}),
		queue:    make(chan job, cfg.QueueSize),
		shutdown: make(chan struct{}),
		voice:    make(map[string]*discordgo.VoiceConnection),
		startedAt: time.Now(),
	}

	sess.AddHandler(b.onReady)
	sess.AddHandler(b.onMessageCreate)
	sess.AddHandler(b.onInteractionCreate)
	sess.AddHandler(b.onMessageDelete)
	sess.AddHandler(b.onMessageUpdate)
	sess.AddHandler(b.onReactionAdd)
	sess.AddHandler(b.onReactionRemove)
	sess.AddHandler(b.onGuildMemberAdd)
	sess.AddHandler(b.onGuildMemberRemove)

	return b, nil
}

// Run opens the gateway, spawns the worker pool, and blocks until SIGINT or
// SIGTERM, then shuts down gracefully.
func (b *Bot) Run() error {
	log := logging.Component("bot")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for i := 0; i < b.cfg.MaxWorkers; i++ {
		b.wg.Add(1)
		recoverutil.GuardGo("worker", b.worker)
	}

	if err := b.sess.Open(); err != nil {
		return fmt.Errorf("gateway open: %w", err)
	}
	// registerCommands needs the bot's own ID, which only exists after the
	// gateway connects — so it must run after Open().
	if err := b.registerCommands(); err != nil {
		return fmt.Errorf("register commands: %w", err)
	}
	log.Info("bot online", "workers", b.cfg.MaxWorkers, "queue_size", b.cfg.QueueSize, "music_enabled", b.cfg.MusicEnabled)

	<-ctx.Done()
	log.Info("shutting down…")
	close(b.shutdown)
	b.wg.Wait()
	b.disconnectAllVoice()
	if b.store != nil {
		_ = b.store.Close()
	}
	_ = b.sess.Close()
	log.Info("bye")
	return nil
}

// worker consumes jobs and streams replies. It never touches the gateway
// event loop beyond REST calls, so a slow LLM cannot stall message delivery.
func (b *Bot) worker() {
	defer b.wg.Done()
	for {
		select {
		case <-b.shutdown:
			return
		case j := <-b.queue:
			// Guard each job so a panic in one job doesn't kill the
			// worker permanently.
			recoverutil.Guard("worker", func() {
				b.handleJob(j)
			})
		}
	}
}

// onReady logs gateway info once connected.
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	defer recoverutil.Recover("onReady")
	logging.Component("gateway").Info("gateway ready", "user", r.User.Username, "guilds", len(r.Guilds))
}

// onMessageCreate is the fast path: filter, typing indicator, enqueue. All
// heavy work happens in the worker pool.
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	defer recoverutil.Recover("onMessageCreate")
	if m.Author == nil || m.Author.Bot || m.Author.ID == s.State.User.ID {
		return
	}

	// AutoMod: check message before any processing.
	if b.automod != nil {
		var roles []string
		if m.Member != nil {
			roles = make([]string, len(m.Member.Roles))
			copy(roles, m.Member.Roles)
		}
		if v := b.automod.Check(m.Author.ID, m.Content, roles); v != nil {
			b.handleAutoModViolation(s, m, v)
			return
		}
	}

	// x! prefix triggers text commands, NOT AI chat.
	if strings.HasPrefix(m.Content, "x!") {
		b.handlePrefixCommand(s, m)
		return
	}

	if !b.wantsReply(s, m.Message) {
		return
	}
	if !b.cfg.Allowed(m.ChannelID) {
		return
	}

	content := b.cleanPrompt(s, m.Content)
	if strings.TrimSpace(content) == "" {
		return
	}

	// Fire typing immediately so the user gets feedback while the LLM works.
	_ = s.ChannelTyping(m.ChannelID)

	select {
	case b.queue <- job{guildID: m.GuildID, channelID: m.ChannelID, userName: m.Author.Username, content: content}:
		logging.Component("ai").Debug("ai job queued", "channel", m.ChannelID, "queue_depth", len(b.queue))
	default:
		logging.Component("ai").Warn("queue full, dropping message", "channel", m.ChannelID, "queue_depth", len(b.queue))
		_, _ = s.ChannelMessageSend(m.ChannelID, "⚠️ I'm busy right now; try again in a moment.")
	}
}

// handleAutoModViolation deletes the offending message and takes the
// configured action (warn or mute). Logs to the mod-log channel if set.
func (b *Bot) handleAutoModViolation(s *discordgo.Session, m *discordgo.MessageCreate, v *automod.Violation) {
	log := logging.Component("automod")

	// Delete the offending message.
	if err := s.ChannelMessageDelete(m.ChannelID, m.ID); err != nil {
		log.Warn("failed to delete automod message", "err", err)
	}

	// Send a warning embed to the channel.
	warnMsg := fmt.Sprintf("**AutoMod:** %s — %s\nUser: <@%s>", v.Rule, v.Detail, m.Author.ID)
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Title:       "🛡️ AutoMod",
		Description: warnMsg,
		Color:       colorRed,
	})

	// Take action.
	switch v.Action {
	case automod.ActionMute:
		if m.GuildID != "" && m.Member != nil {
			dur := 5 * time.Minute
			muteMin := int64(b.cfg.AutoModSpamMuteMin)
			if v.Rule == "spam" && muteMin > 0 {
				dur = time.Duration(muteMin) * time.Minute
			}
			until := time.Now().Add(dur)
			if err := s.GuildMemberTimeout(m.GuildID, m.Author.ID, &until); err != nil {
				log.Warn("failed to timeout user", "err", err, "user", m.Author.ID)
			} else {
				log.Info("automod muted user", "rule", v.Rule, "user", m.Author.ID, "duration", dur)
			}
		}
	case automod.ActionWarn:
		log.Info("automod warned user", "rule", v.Rule, "user", m.Author.ID)
	}

	// Log to mod-log channel if configured.
	if b.cfg.ModLogChannel != "" {
		embed := &discordgo.MessageEmbed{
			Title:       "🛡️ AutoMod Action",
			Description: fmt.Sprintf("**Rule:** %s\n**Action:** %s\n**Detail:** %s\n**User:** <@%s> (%s)\n**Channel:** <#%s>", v.Rule, v.Action, v.Detail, m.Author.ID, m.Author.Username, m.ChannelID),
			Color:       colorRed,
			Timestamp:   time.Now().Format(time.RFC3339),
		}
		_, _ = s.ChannelMessageSendEmbed(b.cfg.ModLogChannel, embed)
	}
}

// handlePrefixCommand parses and executes a text command prefixed with "x!".
// This is an alternative to slash commands for users who prefer typing.
// Example: "x!play despacito" → same as /play query:despacito
func (b *Bot) handlePrefixCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	log := logging.Component("prefix")
	raw := strings.TrimPrefix(m.Content, "x!")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}

	// Split into command + args.
	parts := strings.Fields(raw)
	cmd := strings.ToLower(parts[0])
	args := strings.TrimSpace(strings.TrimPrefix(raw, parts[0]))

	log.Info("prefix command", "cmd", cmd, "args", args, "user", m.Author.ID)

	switch cmd {
	case "ping":
		latency := s.HeartbeatLatency().Round(time.Millisecond)
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Title:       "🏓 Pong!",
			Description: fmt.Sprintf("Latency: **%s**", latency),
			Color:       colorGreen,
		})
	case "help":
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, infoEmbed("📖 ZoeyDCBot Help", helpText(b.cfg.MusicEnabled)))
	case "version":
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Title:       "🤖 ZoeyDCBot",
			Description: versionText(),
			Color:       ColorInfo,
			Footer:      &discordgo.MessageEmbedFooter{Text: "made with ❤️ by jettapindika"},
		})
	case "clear":
		n := b.history.Clear(m.ChannelID)
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("🧹 Context Reset",
			fmt.Sprintf("Cleared %d conversation turns from this channel.", n)))
	case "play", "p":
		if !b.cfg.MusicEnabled {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Music Disabled", "Music commands are disabled."))
			return
		}
		if args == "" {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Empty Query", "Please provide a URL or search query."))
			return
		}
		b.handlePrefixPlay(s, m, args)
	case "skip":
		b.runPrefixMusicAction(s, m, "skip")
	case "stop":
		b.runPrefixMusicAction(s, m, "stop")
	case "pause":
		b.runPrefixMusicAction(s, m, "pause")
	case "resume":
		b.runPrefixMusicAction(s, m, "resume")
	case "queue", "q":
		b.runPrefixMusicAction(s, m, "queue")
	case "nowplaying", "np":
		b.runPrefixMusicAction(s, m, "nowplaying")
	case "leave":
		b.runPrefixMusicAction(s, m, "leave")
	case "volume", "vol":
		b.runPrefixMusicAction(s, m, "volume "+args)
	default:
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Unknown Command",
			fmt.Sprintf("`x!%s` is not a recognised command. Try `x!help`.", cmd)))
	}
}

// runPrefixMusicAction dispatches a prefix command to the same handler used by
// slash commands. It synthesises a fake InteractionCreate so the existing
// embed-based handlers can be reused.
func (b *Bot) runPrefixMusicAction(s *discordgo.Session, m *discordgo.MessageCreate, action string) {
	if !b.cfg.MusicEnabled {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	// For music actions that need a guild context, validate we're in one.
	if m.GuildID == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Not In Server", "Music can only be used in a server."))
		return
	}

	parts := strings.Fields(action)
	cmdName := parts[0]

	switch cmdName {
	case "skip":
		if !b.music.IsPlaying(m.GuildID) {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Nothing Playing", "Nothing is playing right now."))
			return
		}
		b.player.Stop(m.GuildID)
		if b.music.HasNext(m.GuildID) {
			next, ok := b.music.PeekNext(m.GuildID)
			desc := "Skipping to the next track…"
			if ok && next.Title != "" {
				desc = fmt.Sprintf("Skipping to **%s**", next.Display())
			}
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("⏭️ Skipped", desc))
		} else {
			b.music.ClearNowPlaying(m.GuildID)
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, neutralEmbed("⏭️ Skipped", "Skipped. Queue is now empty."))
		}
	case "stop":
		b.player.Stop(m.GuildID)
		b.music.Stop(m.GuildID)
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("⏹️ Stopped", "Stopped playback and cleared the queue."))
	case "pause":
		if !b.music.IsPlaying(m.GuildID) {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Nothing Playing", "Nothing is playing right now."))
			return
		}
		if b.music.IsPaused(m.GuildID) {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Already Paused", "Playback is already paused."))
			return
		}
		b.player.Stop(m.GuildID)
		b.music.Pause(m.GuildID)
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("⏸️ Paused", "Playback paused. Use `x!resume` to continue."))
	case "resume":
		if !b.music.IsPaused(m.GuildID) {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Not Paused", "Nothing is paused right now."))
			return
		}
		_, now, _ := b.music.Queue(m.GuildID)
		if now == nil {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("No Track", "No track to resume."))
			return
		}
		b.music.Resume(m.GuildID)
		b.voiceMu.Lock()
		vc := b.voice[m.GuildID]
		b.voiceMu.Unlock()
		if vc == nil || !vc.Ready {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Voice Lost", "Voice connection lost. Please use `x!play` again."))
			return
		}
		desc := "Resuming playback."
		if now.Title != "" {
			desc = fmt.Sprintf("Resuming **%s**", now.Display())
		}
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("▶️ Resumed", desc))
		recoverutil.GuardGo("replayCurrent", func() { b.replayCurrent(m.GuildID, m.ChannelID, vc) })
	case "queue":
		q, now, paused := b.music.Queue(m.GuildID)
		if now == nil && len(q) == 0 {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, neutralEmbed("📭 Queue Empty", "Nothing is playing and the queue is empty."))
			return
		}
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Title:       "🎵 Music Queue",
			Description: music.FormatQueue(q, now, paused, 1),
			Color:       colorBlue,
		})
	case "nowplaying":
		_, now, paused := b.music.Queue(m.GuildID)
		if now == nil {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, neutralEmbed("📭 Nothing Playing", "Nothing is playing right now."))
			return
		}
		status := "▶️ Playing"
		if paused {
			status = "⏸️ Paused"
		}
		fields := []*discordgo.MessageEmbedField{inlineField("Status", status)}
		if now.Duration > 0 {
			fields = append(fields, inlineField("Duration", music.FormatDuration(now.Duration)))
		}
		if now.Source != "" {
			fields = append(fields, inlineField("Source", now.Source))
		}
		embed := &discordgo.MessageEmbed{
			Title:       "🎶 Now Playing",
			Description: now.MarkdownLink(),
			Color:       colorGreen,
			Fields:      fields,
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Requested by %s", now.RequestedByName),
			},
		}
		if now.Thumbnail != "" {
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: now.Thumbnail}
		}
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
	case "leave":
		b.player.Stop(m.GuildID)
		b.music.Stop(m.GuildID)
		b.disconnectVoice(m.GuildID)
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, successEmbed("👋 Left Voice", "Left the voice channel and cleared the queue."))
	}
}

// handlePrefixPlay handles x!play by reusing the slash command logic.
func (b *Bot) handlePrefixPlay(s *discordgo.Session, m *discordgo.MessageCreate, query string) {
	if m.GuildID == "" {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Not In Server", "Music can only be used in a server."))
		return
	}
	voiceChID, err := b.userVoiceChannel(m.GuildID, m.Author.ID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Voice Error", err.Error()))
		return
	}

	musicLog.Info("prefix play command", "guild", m.GuildID, "user", m.Author.ID, "query", query)
	requesterName := b.requesterDisplayName(m.GuildID, m.Author.ID)

	// Join voice.
	vc, err := b.joinVoice(m.GuildID, voiceChID)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Voice Join Failed", err.Error()))
		return
	}

	// Playlist expansion.
	if player.IsPlaylist(query) {
		b.handlePrefixPlaylistPlay(s, m, query, requesterName, vc)
		return
	}

	// Single track — resolve metadata first.
	rt, err := b.resolveTrack(context.Background(), query)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Resolve Failed",
			fmt.Sprintf("Could not resolve **%s**\n\n```\n%v\n```", query, err)))
		return
	}

	track := trackFromResolved(rt, query, m.Author.ID, requesterName)

	if b.music.IsPlaying(m.GuildID) {
		pos, err := b.music.Add(m.GuildID, track)
		if err != nil {
			_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Queue Full", err.Error()))
			return
		}
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, queuedTrackEmbed(&track, pos, b.music.Remaining(m.GuildID)))
		return
	}

	pos, err := b.music.Add(m.GuildID, track)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Queue Full", err.Error()))
		return
	}
	musicLog.Info("added first track, starting playback", "pos", pos, "track", track.Title)

	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, queuedTrackEmbed(&track, pos, b.music.Remaining(m.GuildID)))
	recoverutil.GuardGo("tryStartPlayback", func() { b.tryStartPlayback(m.GuildID, m.ChannelID, vc) })
}

// handlePrefixPlaylistPlay handles x!play with a playlist URL.
func (b *Bot) handlePrefixPlaylistPlay(s *discordgo.Session, m *discordgo.MessageCreate, query, requesterName string, vc *discordgo.VoiceConnection) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tracks, playlistTitle, err := b.player.FetchPlaylist(ctx, query, b.cfg.MusicMaxQueue)
	if err != nil {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, errEmbed("Playlist Fetch Failed",
			fmt.Sprintf("Could not expand playlist **%s**\n\n```\n%v\n```", query, err)))
		return
	}
	if len(tracks) == 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, warnEmbed("Empty Playlist", "No tracks found in this playlist."))
		return
	}

	musicTracks := make([]music.Track, 0, len(tracks))
	for _, t := range tracks {
		mt := music.Track{
			Title:           t.Title,
			Query:           t.URL,
			Artist:          t.Artist,
			Thumbnail:       t.Thumbnail,
			Duration:        t.Duration,
			RequestedBy:     m.Author.ID,
			RequestedByName: requesterName,
			PlaylistName:    playlistTitle,
		}
		if mt.Query == "" {
			mt.Query = t.Title
		}
		musicTracks = append(musicTracks, mt)
	}

	added, firstPos := b.music.AddMany(m.GuildID, musicTracks)
	remaining := b.music.Remaining(m.GuildID)

	var totalDur float64
	for _, t := range musicTracks[:added] {
		totalDur += t.Duration
	}
	desc := fmt.Sprintf("**%s**\nAdded **%d** track(s) at position %d",
		playlistTitle, added, firstPos)
	if added < len(musicTracks) {
		desc += fmt.Sprintf(" (of %d — queue cap reached)", len(musicTracks))
	}
	desc += fmt.Sprintf("\nTotal duration: **%s**", music.FormatDuration(totalDur))

	embed := &discordgo.MessageEmbed{
		Title:       "📜 Playlist Added",
		Description: desc,
		Color:       colorBlue,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s · %d in queue", requesterName, remaining+added),
		},
	}

	if !b.music.IsPlaying(m.GuildID) && added > 0 {
		_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
		recoverutil.GuardGo("tryStartPlayback", func() { b.tryStartPlayback(m.GuildID, m.ChannelID, vc) })
		return
	}
	_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

// wantsReply decides whether this message triggers the bot: DMs always do,
// guild messages when the bot is mentioned or replied-to.
// The x! prefix is handled separately in onMessageCreate (triggers commands, not AI chat).
func (b *Bot) wantsReply(s *discordgo.Session, m *discordgo.Message) bool {
	if m.GuildID == "" {
		return true // DM
	}
	if m.Mentions != nil {
		for _, u := range m.Mentions {
			if u.ID == s.State.User.ID {
				return true
			}
		}
	}
	if m.ReferencedMessage != nil && m.ReferencedMessage.Author != nil &&
		m.ReferencedMessage.Author.ID == s.State.User.ID {
		return true
	}
	return false
}

func (b *Bot) cleanPrompt(s *discordgo.Session, content string) string {
	if s.State == nil || s.State.User == nil {
		return strings.TrimSpace(content)
	}
	id := s.State.User.ID
	content = strings.ReplaceAll(content, "<@"+id+">", "")
	content = strings.ReplaceAll(content, "<@!"+id+">", "")
	return strings.TrimSpace(content)
}

// handleJob runs in a worker: build context, stream from the LLM, and edit the
// reply message as chunks arrive. It instruments TTFT and total latency.
func (b *Bot) handleJob(j job) {
	log := logging.Component("ai")
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(b.cfg.LLMTimeoutSeconds)*time.Second)
	defer cancel()

	history := b.history.Append(j.channelID, "user", j.content)

	var (
		msgID      string
		buf        strings.Builder
		firstToken time.Duration
		edited     time.Time
		thinking   bool // true once we've shown a thinking placeholder
	)

	err := b.ai.StreamChat(ctx, b.cfg.SystemPrompt, history, func(ch ai.Chunk) {
		if ch.Latency > 0 && firstToken == 0 {
			firstToken = ch.Latency
			log.Info("first token", "channel", j.channelID, "ttft", ch.Latency.Round(time.Millisecond).String())
		}

		// Reasoning models stream reasoning_content before content.
		// Show a thinking placeholder so the user knows the bot is working.
		if ch.Reasoning != "" && msgID == "" {
			msg, err := b.sess.ChannelMessageSend(j.channelID, "🤔 *Thinking…*")
			if err != nil {
				log.Error("send thinking placeholder", "err", err)
				msgID = "failed"
				return
			}
			msgID = msg.ID
			thinking = true
			edited = time.Now()
			return
		}
		// Ignore further reasoning chunks once the placeholder is up;
		// we only surface the final answer to the user.
		if ch.Reasoning != "" {
			return
		}

		buf.WriteString(ch.Content)
		content := discordSafe(buf.String())
		if strings.TrimSpace(content) == "" {
			return
		}

		if msgID == "" {
			// Send the first message as soon as we have something to show.
			msg, err := b.sess.ChannelMessageSend(j.channelID, content)
			if err != nil {
				log.Error("send first message", "err", err)
				msgID = "failed"
				return
			}
			msgID = msg.ID
			edited = time.Now()
			return
		}
		if msgID == "failed" {
			return
		}
		// Replace the thinking placeholder with the first real content.
		if thinking {
			thinking = false
			if _, err := b.sess.ChannelMessageEdit(j.channelID, msgID, content); err != nil {
				log.Warn("edit thinking placeholder failed, sending fresh", "err", err)
				msg, err2 := b.sess.ChannelMessageSend(j.channelID, content)
				if err2 != nil {
					log.Error("fallback send failed", "err", err2)
					msgID = "failed"
					return
				}
				msgID = msg.ID
			}
			edited = time.Now()
			return
		}

		// Throttle edits: Discord rate-limits message edits hard (~1/sec).
		if time.Since(edited) < time.Duration(b.cfg.EditInterval)*time.Millisecond && !ch.Finish {
			return
		}
		if _, err := b.sess.ChannelMessageEdit(j.channelID, msgID, content); err != nil {
			log.Warn("edit failed, will send fresh message", "err", err)
			msg, err2 := b.sess.ChannelMessageSend(j.channelID, content)
			if err2 != nil {
				log.Error("fallback send failed", "err", err2)
				msgID = "failed"
				return
			}
			msgID = msg.ID
		}
		edited = time.Now()
	})

	if err != nil {
		// If we already posted something, edit it with the error note rather than
		// leaving a half answer.
		note := "\n\n_(⚠️ error: " + discordSafe(err.Error()) + ")_"
		if msgID != "" && msgID != "failed" {
			_, _ = b.sess.ChannelMessageEdit(j.channelID, msgID, discordSafe(buf.String()+note))
		} else {
			_, _ = b.sess.ChannelMessageSend(j.channelID, "⚠️ "+discordSafe(err.Error()))
		}
		log.Error("ai stream failed", "channel", j.channelID, "err", err)
		return
	}

	answer := strings.TrimSpace(buf.String())
	if answer == "" {
		_, _ = b.sess.ChannelMessageSend(j.channelID, "⚠️ I didn't get a response from the model.")
		return
	}
	if msgID == "" {
		_, _ = b.sess.ChannelMessageSend(j.channelID, discordSafe(answer))
	} else if msgID != "failed" {
		_, _ = b.sess.ChannelMessageEdit(j.channelID, msgID, discordSafe(answer))
	}

	// Record the assistant turn for future context.
	b.history.Append(j.channelID, "assistant", answer)

	total := time.Since(start)
	log.Info("reply done", "channel", j.channelID, "chars", len(answer),
		"total", total.Round(time.Millisecond).String(),
		"ttft", firstToken.Round(time.Millisecond).String())
}

func discordSafe(s string) string {
	s = strings.TrimSpace(s)
	const limit = 1900
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n…"
}

// onInteractionCreate handles slash commands and button interactions.
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	defer recoverutil.Recover("onInteractionCreate")
	if i.Type == discordgo.InteractionApplicationCommand {
		switch i.ApplicationCommandData().Name {
		case "ping":
			b.cmdPing(s, i)
		case "clear":
			b.cmdClear(s, i)
		case "version":
			b.cmdVersion(s, i)
		case "help":
			b.cmdHelp(s, i)
		case "purge":
			b.cmdPurge(s, i)
		case "kick":
			b.cmdKick(s, i)
		case "ban":
			b.cmdBan(s, i)
		case "timeout":
			b.cmdTimeout(s, i)
		case "untimeout":
			b.cmdUntimeout(s, i)
		case "slowmode":
			b.cmdSlowmode(s, i)
		case "lock":
			b.cmdLock(s, i)
		case "unlock":
			b.cmdUnlock(s, i)
		case "userinfo":
			b.cmdUserInfo(s, i)
		case "serverinfo":
			b.cmdServerInfo(s, i)
		case "play":
			b.cmdPlay(s, i)
		case "queue":
			b.cmdQueue(s, i)
		case "skip":
			b.cmdSkip(s, i)
		case "stop":
			b.cmdStop(s, i)
		case "pause":
			b.cmdPause(s, i)
		case "resume":
			b.cmdResume(s, i)
		case "leave":
			b.cmdLeave(s, i)
		case "nowplaying":
			b.cmdNowPlaying(s, i)
		case "volume":
			b.cmdVolume(s, i)
		case "loop":
			b.cmdLoop(s, i)
		case "shuffle":
			b.cmdShuffle(s, i)
		case "remove":
			b.cmdRemove(s, i)
		case "lyrics":
			b.cmdLyrics(s, i)
		case "move":
			b.cmdMove(s, i)
		case "reactionrole":
			b.cmdReactionRole(s, i)
		case "removerrole":
			b.cmdRemoveReactionRole(s, i)
		case "starboard":
			b.cmdStarboard(s, i)
		default:
			b.respondEphemeralEmbed(s, i, warnEmbed("Unknown Command", "This command is not recognised."))
		}
	} else if i.Type == discordgo.InteractionMessageComponent {
		// Handle button clicks
		data := i.MessageComponentData()
		switch data.CustomID {
		case "music_pause":
			b.cmdPause(s, i)
		case "music_skip":
			b.cmdSkip(s, i)
		case "music_stop":
			b.cmdStop(s, i)
		default:
			b.respondEphemeralEmbed(s, i, warnEmbed("Unknown Action", "This button action is not recognised."))
		}
	}
}

// parseAction converts a string config value to an automod.Action.
func parseAction(s string, def automod.Action) automod.Action {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "warn":
		return automod.ActionWarn
	case "mute", "timeout":
		return automod.ActionMute
	default:
		return def
	}
}
