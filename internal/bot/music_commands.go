package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/admin"
	"github.com/jettapindika/zoeyDCBot/internal/logging"
	"github.com/jettapindika/zoeyDCBot/internal/music"
	"github.com/jettapindika/zoeyDCBot/internal/player"
	"github.com/jettapindika/zoeyDCBot/internal/recoverutil"
)

var musicLog = logging.Component("music")

// userVoiceChannel returns the voice channel ID the interaction user is in.
func (b *Bot) userVoiceChannel(guildID, userID string) (string, error) {
	if guildID == "" {
		return "", fmt.Errorf("this command can only be used in a server")
	}
	vs, err := b.sess.State.VoiceState(guildID, userID)
	if err != nil {
		return "", fmt.Errorf("you need to be in a voice channel first")
	}
	return vs.ChannelID, nil
}

// joinVoice connects the bot to the given voice channel and stores the connection.
func (b *Bot) joinVoice(guildID, channelID string) (*discordgo.VoiceConnection, error) {
	b.voiceMu.Lock()
	defer b.voiceMu.Unlock()
	if vc := b.voice[guildID]; vc != nil {
		return vc, nil
	}
	musicLog.Info("joining voice channel", "guild", guildID, "channel", channelID)
	vc, err := b.sess.ChannelVoiceJoin(guildID, channelID, false, true)
	if err != nil {
		musicLog.Error("failed to join voice", "guild", guildID, "channel", channelID, "err", err)
		return nil, fmt.Errorf("failed to join voice: %w", err)
	}
	b.voice[guildID] = vc
	musicLog.Info("joined voice channel", "guild", guildID, "channel", channelID)
	return vc, nil
}

func (b *Bot) disconnectVoice(guildID string) {
	b.voiceMu.Lock()
	defer b.voiceMu.Unlock()
	if vc := b.voice[guildID]; vc != nil {
		musicLog.Info("disconnecting from voice", "guild", guildID)
		_ = vc.Disconnect()
		delete(b.voice, guildID)
	}
}

func (b *Bot) disconnectAllVoice() {
	b.voiceMu.Lock()
	defer b.voiceMu.Unlock()
	for gid, vc := range b.voice {
		musicLog.Info("disconnecting from voice (shutdown)", "guild", gid)
		_ = vc.Disconnect()
		delete(b.voice, gid)
	}
}

// resolveTrack wraps player.Resolve with a timeout and logging.
func (b *Bot) resolveTrack(ctx context.Context, query string) (*player.ResolvedTrack, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	rt, err := b.player.Resolve(resolveCtx, query)
	if err != nil {
		musicLog.Error("resolve failed", "query", query, "err", err)
		return nil, err
	}
	return rt, nil
}

// trackFromResolved builds a music.Track from a resolved track + requester info.
func trackFromResolved(rt *player.ResolvedTrack, query, requesterID, requesterName string) music.Track {
	return music.Track{
		Title:           rt.Title,
		Query:           query,
		StreamURL:       rt.URL,
		Artist:          rt.Artist,
		Thumbnail:       rt.Thumbnail,
		Duration:        rt.Duration,
		RequestedBy:     requesterID,
		RequestedByName: requesterName,
		Source:          rt.Source,
		WebpageURL:      rt.WebpageURL,
	}
}

// requesterDisplayName resolves a user ID to the best display name via the guild.
func (b *Bot) requesterDisplayName(guildID, userID string) string {
	if member, err := b.sess.GuildMember(guildID, userID); err == nil && member.User != nil {
		if member.Nick != "" {
			return member.Nick
		}
		if member.User.GlobalName != "" {
			return member.User.GlobalName
		}
		return member.User.Username
	}
	return userID
}

// cmdPlay handles /play with link metadata fetching, playlist expansion,
// and gap-free playback (pre-resolves before starting the player).
func (b *Bot) cmdPlay(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	if i.GuildID == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Not In Server", "Music can only be used in a server."))
		return
	}
	data := i.ApplicationCommandData()
	query := strings.TrimSpace(admin.StringOption(data.Options, "query"))
	if query == "" {
		b.respondEphemeralEmbed(s, i, warnEmbed("Empty Query", "Please provide a URL or search query."))
		return
	}
	voiceChID, err := b.userVoiceChannel(i.GuildID, i.Member.User.ID)
	if err != nil {
		b.respondEphemeralEmbed(s, i, warnEmbed("Voice Error", err.Error()))
		return
	}

	musicLog.Info("play command", "guild", i.GuildID, "user", i.Member.User.ID, "query", query)
	requesterName := b.requesterDisplayName(i.GuildID, i.Member.User.ID)

	// Defer so we can do slow resolution work.
	b.deferEphemeral(s, i)

	// Join voice.
	vc, err := b.joinVoice(i.GuildID, voiceChID)
	if err != nil {
		b.followUpEphemeralEmbed(s, i, errEmbed("Voice Join Failed", err.Error()))
		return
	}

	// ── Playlist expansion ──────────────────────────────────────────
	if player.IsPlaylist(query) {
		b.handlePlaylistPlay(s, i, query, requesterName, vc)
		return
	}

	// ── Single track ────────────────────────────────────────────────
	// Resolve metadata first so the embed is rich and playback is gap-free.
	rt, err := b.resolveTrack(context.Background(), query)
	if err != nil {
		b.followUpEphemeralEmbed(s, i, errEmbed("Resolve Failed",
			fmt.Sprintf("Could not resolve **%s**\n\n```\n%v\n```", query, err)))
		return
	}

	track := trackFromResolved(rt, query, i.Member.User.ID, requesterName)

	// If something is already playing, just add to queue.
	if b.music.IsPlaying(i.GuildID) {
		pos, err := b.music.Add(i.GuildID, track)
		if err != nil {
			b.followUpEphemeralEmbed(s, i, errEmbed("Queue Full", err.Error()))
			return
		}
		b.followUpEmbed(s, i, queuedTrackEmbed(&track, pos, b.music.Remaining(i.GuildID)))
		return
	}

	// Nothing playing — add to queue then start immediately.
	pos, err := b.music.Add(i.GuildID, track)
	if err != nil {
		b.followUpEphemeralEmbed(s, i, errEmbed("Queue Full", err.Error()))
		return
	}
	musicLog.Info("added first track, starting playback", "pos", pos, "track", track.Title)

	// Follow up the deferred interaction so Discord doesn't show "thinking…".
	b.followUpEmbed(s, i, queuedTrackEmbed(&track, pos, b.music.Remaining(i.GuildID)))

	// Start playback in goroutine — tryStartPlayback will use the pre-resolved
	// StreamURL via PlayResolved, so there is no resolve gap.
	recoverutil.GuardGo("tryStartPlayback", func() { b.tryStartPlayback(i.GuildID, i.ChannelID, vc) })
}

// handlePlaylistPlay fetches all tracks from a playlist URL, adds them to the
// queue, and starts playback if nothing is currently playing.
func (b *Bot) handlePlaylistPlay(s *discordgo.Session, i *discordgo.InteractionCreate, query, requesterName string, vc *discordgo.VoiceConnection) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tracks, playlistTitle, err := b.player.FetchPlaylist(ctx, query, b.cfg.MusicMaxQueue)
	if err != nil {
		b.followUpEphemeralEmbed(s, i, errEmbed("Playlist Fetch Failed",
			fmt.Sprintf("Could not expand playlist **%s**\n\n```\n%v\n```", query, err)))
		return
	}
	if len(tracks) == 0 {
		b.followUpEphemeralEmbed(s, i, warnEmbed("Empty Playlist", "No tracks found in this playlist."))
		return
	}

	// Convert to music.Track entries.
	musicTracks := make([]music.Track, 0, len(tracks))
	for _, t := range tracks {
		mt := music.Track{
			Title:           t.Title,
			Query:           t.URL,
			Artist:          t.Artist,
			Thumbnail:       t.Thumbnail,
			Duration:        t.Duration,
			RequestedBy:     i.Member.User.ID,
			RequestedByName: requesterName,
			PlaylistName:    playlistTitle,
			// StreamURL is empty — will be resolved when the track comes up.
		}
		// Use the track URL as query if available, otherwise the title for search.
		if mt.Query == "" {
			mt.Query = t.Title
		}
		musicTracks = append(musicTracks, mt)
	}

	added, firstPos := b.music.AddMany(i.GuildID, musicTracks)
	remaining := b.music.Remaining(i.GuildID)

	// Build embed showing playlist info + total duration.
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

	// If nothing is playing, start playback with the first track.
	if !b.music.IsPlaying(i.GuildID) && added > 0 {
		b.followUpEmbed(s, i, embed)
		recoverutil.GuardGo("tryStartPlayback", func() { b.tryStartPlayback(i.GuildID, i.ChannelID, vc) })
		return
	}
	b.followUpEmbed(s, i, embed)
}

// tryStartPlayback pops the next track from the queue and plays it.
// When playback finishes, it auto-advances to the next track.
//
// Gap-free: if the track was pre-resolved (StreamURL set), it is passed
// directly to PlayResolved so the player skips yt-dlp entirely.
func (b *Bot) tryStartPlayback(guildID, textChannelID string, vc *discordgo.VoiceConnection) {
	log := musicLog.With("guild", guildID)

	// Don't start if already playing
	if b.player.IsPlaying(guildID) {
		log.Debug("already playing, not starting new playback")
		return
	}

	// Pop next track (Advance handles loop modes)
	track, ok := b.music.Advance(guildID)
	if !ok {
		log.Debug("queue empty, nothing to play")
		return
	}

	log.Info("starting playback", "track", track.Title, "query", track.Query, "requestedBy", track.RequestedBy)

	requesterName := track.RequestedBy
	if track.RequestedByName != "" {
		requesterName = track.RequestedByName
	} else {
		requesterName = b.requesterDisplayName(guildID, track.RequestedBy)
	}

	// Pre-resolve if the track doesn't have a stream URL yet.
	// This is the key to gap-free playback: resolve BEFORE sending the
	// "now playing" message and starting ffmpeg.
	var pre *player.ResolvedTrack
	if track.StreamURL != "" {
		// Already resolved (e.g. single-track /play). Build from stored metadata.
		pre = &player.ResolvedTrack{
			Title:      track.Title,
			URL:        track.StreamURL,
			Duration:   track.Duration,
			Artist:     track.Artist,
			Thumbnail:  track.Thumbnail,
			WebpageURL: track.WebpageURL,
			Source:     track.Source,
		}
	} else {
		rt, err := b.resolveTrack(context.Background(), track.Query)
		if err != nil {
			log.Error("resolve failed for queued track", "track", track.Title, "err", err)
			b.sendPlaybackError(textChannelID, *track, err)
			b.advanceOrFinish(guildID, textChannelID, vc)
			return
		}
		pre = rt
		// Write back resolved metadata so /queue and /nowplaying are rich.
		b.music.UpdateNowPlaying(guildID, func(t *music.Track) {
			t.StreamURL = rt.URL
			t.Artist = rt.Artist
			t.Thumbnail = rt.Thumbnail
			t.Duration = rt.Duration
			t.Source = rt.Source
			t.WebpageURL = rt.WebpageURL
			if t.Title == "" || t.Title == t.Query {
				t.Title = rt.Title
			}
		})
	}

	// Send "now playing" embed with rich metadata.
	embed := nowPlayingEmbed(pre, requesterName, b.player.GetVolume(guildID), track.PlaylistName)
	components := musicControlButtons()

	_, _ = b.sess.ChannelMessageSendComplex(textChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})

	// Update bot presence to show current track.
	if b.presence != nil {
		b.presence.SetNowPlaying(track.Title, pre.Artist)
	}

	// Play the track (blocks until done).  Pre-resolved → no resolve gap.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := b.player.PlayResolved(ctx, vc, guildID, track.Query, pre, func() {
		log.Info("track finished, checking for next", "track", track.Title)
		b.advanceOrFinish(guildID, textChannelID, vc)
	})

	if err != nil {
		log.Error("playback error", "track", track.Title, "err", err)
		b.sendPlaybackError(textChannelID, *track, err)
		b.advanceOrFinish(guildID, textChannelID, vc)
	}
}

// advanceOrFinish auto-advances to the next track or clears now-playing.
// It is called from the onDone callback when PlayResolved returns.
// If the guild is paused, it does NOT advance — the track will be replayed
// by cmdResume instead.
func (b *Bot) advanceOrFinish(guildID, textChannelID string, vc *discordgo.VoiceConnection) {
	log := musicLog.With("guild", guildID)

	// Don't auto-advance if the user paused. The track is still in
	// NowPlaying; cmdResume will replay it.
	if b.music.IsPaused(guildID) {
		log.Debug("guild is paused, not auto-advancing")
		return
	}

	// For LoopTrack, Advance returns the same track to replay.
	// For LoopQueue, it rotates the queue and returns the next track.
	// For LoopOff, it pops the next track or returns false if empty.
	// tryStartPlayback calls Advance internally, so we just need to check
	// whether there's anything to advance to.
	hasNext := b.music.HasNext(guildID)
	loopMode := b.music.GetLoopMode(guildID)
	if !hasNext && loopMode == music.LoopOff {
		log.Info("queue empty after track finished")
		b.music.ClearNowPlaying(guildID)
		if b.presence != nil {
			b.presence.SetIdle()
		}
		return
	}

	time.Sleep(300 * time.Millisecond)
	b.voiceMu.Lock()
	currentVC := b.voice[guildID]
	b.voiceMu.Unlock()
	if currentVC != nil && currentVC.Ready {
		recoverutil.GuardGo("tryStartPlayback", func() { b.tryStartPlayback(guildID, textChannelID, currentVC) })
	} else {
		log.Warn("voice connection gone, stopping auto-advance")
		b.music.ClearNowPlaying(guildID)
	}
}

// sendPlaybackError posts a red error embed to the text channel.
func (b *Bot) sendPlaybackError(channelID string, track music.Track, err error) {
	embed := &discordgo.MessageEmbed{
		Title:       "⚠️ Playback Error",
		Description: fmt.Sprintf("Failed to play **%s**\n\n```\n%v\n```", track.Display(), err),
		Color:       colorRed,
	}
	_, _ = b.sess.ChannelMessageSendEmbed(channelID, embed)
}

// musicControlButtons returns the pause/skip/stop button row.
func musicControlButtons() []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "⏸️ Pause",
					Style:    discordgo.SecondaryButton,
					CustomID: "music_pause",
				},
				discordgo.Button{
					Label:    "⏭️ Skip",
					Style:    discordgo.SecondaryButton,
					CustomID: "music_skip",
				},
				discordgo.Button{
					Label:    "⏹️ Stop",
					Style:    discordgo.DangerButton,
					CustomID: "music_stop",
				},
			},
		},
	}
}

// nowPlayingEmbed builds a rich "Now Playing" embed from resolved metadata.
func nowPlayingEmbed(rt *player.ResolvedTrack, requesterName string, volume float64, playlistName string) *discordgo.MessageEmbed {
	title := rt.Title
	if title == "" {
		title = "Unknown Track"
	}
	desc := title
	if rt.WebpageURL != "" {
		// Make the title clickable.
		safe := strings.NewReplacer("[", "(", "]", ")").Replace(title)
		desc = "[" + safe + "](" + rt.WebpageURL + ")"
	}

	fields := []*discordgo.MessageEmbedField{
		inlineField("Duration", music.FormatDuration(rt.Duration)),
		inlineField("Volume", fmt.Sprintf("%.0f%%", volume*100)),
	}
	if rt.Artist != "" {
		fields = append(fields, inlineField("Artist", rt.Artist))
	}
	if rt.Source != "" {
		fields = append(fields, inlineField("Source", rt.Source))
	}
	if playlistName != "" {
		fields = append(fields, inlineField("From Playlist", playlistName))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎶 Now Playing",
		Description: desc,
		Color:       colorGreen,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s", requesterName),
		},
	}
	if rt.Thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: rt.Thumbnail}
	}
	return embed
}

// queuedTrackEmbed builds an embed for a track added to the queue.
func queuedTrackEmbed(track *music.Track, position, remaining int) *discordgo.MessageEmbed {
	desc := track.MarkdownLink()
	fields := []*discordgo.MessageEmbedField{
		inlineField("Position", fmt.Sprintf("%d", position)),
		inlineField("In Queue", fmt.Sprintf("%d", remaining+1)),
	}
	if track.Duration > 0 {
		fields = append(fields, inlineField("Duration", music.FormatDuration(track.Duration)))
	}
	if track.Source != "" {
		fields = append(fields, inlineField("Source", track.Source))
	}
	embed := &discordgo.MessageEmbed{
		Title:       "🎵 Added to Queue",
		Description: desc,
		Color:       colorBlue,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Requested by %s", track.RequestedByName),
		},
	}
	if track.Thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: track.Thumbnail}
	}
	return embed
}

func (b *Bot) cmdQueue(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	q, now, paused := b.music.Queue(i.GuildID)
	if now == nil && len(q) == 0 {
		b.respondEphemeralEmbed(s, i, neutralEmbed("📭 Queue Empty", "Nothing is playing and the queue is empty."))
		return
	}
	data := i.ApplicationCommandData()
	page := int(admin.IntOption(data.Options, "page", 1))
	if page < 1 {
		page = 1
	}
	embed := &discordgo.MessageEmbed{
		Title:       "🎵 Music Queue",
		Description: music.FormatQueue(q, now, paused, page),
		Color:       colorBlue,
	}
	b.respondEphemeralEmbed(s, i, embed)
}

func (b *Bot) cmdSkip(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	if !b.music.IsPlaying(i.GuildID) {
		b.respondEphemeralEmbed(s, i, warnEmbed("Nothing Playing", "Nothing is playing right now."))
		return
	}

	musicLog.Info("skip command", "guild", i.GuildID, "user", i.Member.User.ID)

	// Stop current playback (this triggers the onDone callback which auto-advances)
	b.player.Stop(i.GuildID)

	// Check if there's a next track
	if b.music.HasNext(i.GuildID) {
		// Peek the next track for a richer embed.
		next, ok := b.music.PeekNext(i.GuildID)
		desc := "Skipping to the next track…"
		if ok && next.Title != "" {
			desc = fmt.Sprintf("Skipping to **%s**", next.Display())
		}
		b.respondEphemeralEmbed(s, i, successEmbed("⏭️ Skipped", desc))
	} else {
		b.music.ClearNowPlaying(i.GuildID)
		b.respondEphemeralEmbed(s, i, neutralEmbed("⏭️ Skipped", "Skipped. Queue is now empty."))
	}
}

func (b *Bot) cmdStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	musicLog.Info("stop command", "guild", i.GuildID, "user", i.Member.User.ID)
	b.player.Stop(i.GuildID)
	b.music.Stop(i.GuildID)
	if b.presence != nil {
		b.presence.SetIdle()
	}
	b.respondEphemeralEmbed(s, i, successEmbed("⏹️ Stopped", "Stopped playback and cleared the queue."))
}

func (b *Bot) cmdPause(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	if !b.music.IsPlaying(i.GuildID) {
		b.respondEphemeralEmbed(s, i, warnEmbed("Nothing Playing", "Nothing is playing right now."))
		return
	}
	if b.music.IsPaused(i.GuildID) {
		b.respondEphemeralEmbed(s, i, warnEmbed("Already Paused", "Playback is already paused."))
		return
	}
	b.player.Stop(i.GuildID)
	b.music.Pause(i.GuildID)
	b.respondEphemeralEmbed(s, i, successEmbed("⏸️ Paused", "Playback paused. Use /resume to continue."))
}

func (b *Bot) cmdResume(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	if !b.music.IsPaused(i.GuildID) {
		b.respondEphemeralEmbed(s, i, warnEmbed("Not Paused", "Nothing is paused right now."))
		return
	}

	musicLog.Info("resume command", "guild", i.GuildID, "user", i.Member.User.ID)

	_, now, _ := b.music.Queue(i.GuildID)
	if now == nil {
		b.respondEphemeralEmbed(s, i, warnEmbed("No Track", "No track to resume."))
		return
	}

	b.music.Resume(i.GuildID)

	b.voiceMu.Lock()
	vc := b.voice[i.GuildID]
	b.voiceMu.Unlock()

	if vc == nil || !vc.Ready {
		b.respondEphemeralEmbed(s, i, warnEmbed("Voice Lost", "Voice connection lost. Please use /play again."))
		return
	}

	desc := "Resuming playback."
	if now.Title != "" {
		desc = fmt.Sprintf("Resuming **%s**", now.Display())
	}
	b.respondEphemeralEmbed(s, i, successEmbed("▶️ Resumed", desc))
	recoverutil.GuardGo("replayCurrent", func() { b.replayCurrent(i.GuildID, i.ChannelID, vc) })
}

// replayCurrent replays the NowPlaying track from the beginning without
// popping a new track from the queue. Used by /resume after a pause.
func (b *Bot) replayCurrent(guildID, textChannelID string, vc *discordgo.VoiceConnection) {
	log := musicLog.With("guild", guildID)

	if b.player.IsPlaying(guildID) {
		log.Debug("already playing, not replaying")
		return
	}

	// Get the current track from NowPlaying (don't pop from queue).
	_, track, _ := b.music.Queue(guildID)
	if track == nil {
		log.Warn("no now-playing track to replay")
		return
	}

	log.Info("replaying paused track", "track", track.Title, "query", track.Query)

	requesterName := track.RequestedByName
	if requesterName == "" {
		requesterName = b.requesterDisplayName(guildID, track.RequestedBy)
	}

	// Build pre-resolved track from stored metadata if available.
	var pre *player.ResolvedTrack
	if track.StreamURL != "" {
		pre = &player.ResolvedTrack{
			Title:      track.Title,
			URL:        track.StreamURL,
			Duration:   track.Duration,
			Artist:     track.Artist,
			Thumbnail:  track.Thumbnail,
			WebpageURL: track.WebpageURL,
			Source:     track.Source,
		}
	} else {
		rt, err := b.resolveTrack(context.Background(), track.Query)
		if err != nil {
			log.Error("resolve failed on resume", "track", track.Title, "err", err)
			b.sendPlaybackError(textChannelID, *track, err)
			return
		}
		pre = rt
		b.music.UpdateNowPlaying(guildID, func(t *music.Track) {
			t.StreamURL = rt.URL
			t.Artist = rt.Artist
			t.Thumbnail = rt.Thumbnail
			t.Duration = rt.Duration
			t.Source = rt.Source
			t.WebpageURL = rt.WebpageURL
		})
	}

	embed := nowPlayingEmbed(pre, requesterName, b.player.GetVolume(guildID), track.PlaylistName)
	components := musicControlButtons()
	_, _ = b.sess.ChannelMessageSendComplex(textChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})

	// Update bot presence to show current track.
	if b.presence != nil {
		b.presence.SetNowPlaying(track.Title, pre.Artist)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := b.player.PlayResolved(ctx, vc, guildID, track.Query, pre, func() {
		log.Info("replayed track finished, checking for next", "track", track.Title)
		b.advanceOrFinish(guildID, textChannelID, vc)
	})

	if err != nil {
		log.Error("replay playback error", "track", track.Title, "err", err)
		b.sendPlaybackError(textChannelID, *track, err)
		b.advanceOrFinish(guildID, textChannelID, vc)
	}
}

func (b *Bot) cmdNowPlaying(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	_, now, paused := b.music.Queue(i.GuildID)
	if now == nil {
		b.respondEphemeralEmbed(s, i, neutralEmbed("📭 Nothing Playing", "Nothing is playing right now."))
		return
	}

	status := "▶️ Playing"
	if paused {
		status = "⏸️ Paused"
	}

	fields := []*discordgo.MessageEmbedField{
		inlineField("Status", status),
	}
	if now.Duration > 0 {
		fields = append(fields, inlineField("Duration", music.FormatDuration(now.Duration)))
	}
	if now.Source != "" {
		fields = append(fields, inlineField("Source", now.Source))
	}
	if now.Artist != "" {
		fields = append(fields, inlineField("Artist", now.Artist))
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
	b.respondEphemeralEmbed(s, i, embed)
}

func (b *Bot) cmdLeave(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	musicLog.Info("leave command", "guild", i.GuildID, "user", i.Member.User.ID)
	b.player.Stop(i.GuildID)
	b.music.Stop(i.GuildID)
	b.disconnectVoice(i.GuildID)
	b.respondEphemeralEmbed(s, i, successEmbed("👋 Left Voice", "Left the voice channel and cleared the queue."))
}

func (b *Bot) cmdVolume(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	data := i.ApplicationCommandData()
	vol := admin.IntOption(data.Options, "level", 100)

	if vol < 0 || vol > 200 {
		b.respondEphemeralEmbed(s, i, warnEmbed("Invalid Volume", "Volume must be between 0 and 200."))
		return
	}

	volFloat := float64(vol) / 100.0
	b.player.SetVolume(i.GuildID, volFloat)

	b.respondEphemeralEmbed(s, i, successEmbed("🔊 Volume Set", fmt.Sprintf("Volume set to **%d%%**", vol)))
}

func (b *Bot) cmdLoop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	data := i.ApplicationCommandData()
	modeStr := admin.StringOption(data.Options, "mode")
	if modeStr == "" {
		modeStr = "off"
	}

	var mode music.LoopMode
	var label, emoji string
	switch modeStr {
	case "track":
		mode = music.LoopTrack
		label = "Track"
		emoji = "🔂"
	case "queue":
		mode = music.LoopQueue
		label = "Queue"
		emoji = "🔁"
	default:
		mode = music.LoopOff
		label = "Off"
		emoji = "➡️"
	}

	b.music.SetLoopMode(i.GuildID, mode)

	desc := fmt.Sprintf("Repeat mode set to **%s**.", label)
	if mode == music.LoopOff {
		desc = "Repeat mode **disabled**."
	}
	b.respondEphemeralEmbed(s, i, successEmbed(fmt.Sprintf("%s Loop %s", emoji, label), desc))
}

func (b *Bot) cmdShuffle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}

	n := b.music.Shuffle(i.GuildID)
	if n < 2 {
		b.respondEphemeralEmbed(s, i, warnEmbed("🔀 Nothing to Shuffle", "Need at least 2 tracks in the queue to shuffle."))
		return
	}

	b.respondEphemeralEmbed(s, i, successEmbed("🔀 Shuffled", fmt.Sprintf("Shuffled **%d** tracks in the queue.", n)))
}

func (b *Bot) cmdRemove(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	data := i.ApplicationCommandData()
	pos := admin.IntOption(data.Options, "position", 0)

	if pos < 1 {
		b.respondEphemeralEmbed(s, i, warnEmbed("Invalid Position", "Position must be at least 1. Use `/queue` to see positions."))
		return
	}

	track, ok := b.music.Remove(i.GuildID, int(pos))
	if !ok {
		b.respondEphemeralEmbed(s, i, warnEmbed("Invalid Position", fmt.Sprintf("Position %d is not in the queue. Use `/queue` to see valid positions.", pos)))
		return
	}

	b.respondEphemeralEmbed(s, i, successEmbed("🗑️ Removed", fmt.Sprintf("Removed **%s** from position %d.", track.Display(), pos)))
}

func (b *Bot) cmdLyrics(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}

	data := i.ApplicationCommandData()
	query := admin.StringOption(data.Options, "query")

	// If no query provided, use the currently playing track.
	if query == "" {
		_, now, _ := b.music.Queue(i.GuildID)
		if now == nil {
			b.respondEphemeralEmbed(s, i, warnEmbed("No Track", "Nothing is playing. Provide a search query with `/lyrics <song name>`."))
			return
		}
		query = now.Title
		if now.Artist != "" {
			query = now.Artist + " " + now.Title
		}
	}

	// Acknowledge immediately — the API call may take a second.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				infoEmbed("🎵 Searching…", fmt.Sprintf("Looking up lyrics for **%s**", query)),
			},
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		musicLog.Warn("failed to acknowledge lyrics command", "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := b.lyrics.Search(ctx, query, "")
	if err != nil {
		b.followUpEmbed(s, i, warnEmbed("No Lyrics Found", fmt.Sprintf("Couldn't find lyrics for **%s**.\n\n`%v`", query, err)))
		return
	}

	if result.Instrumental {
		title := fmt.Sprintf("🎵 %s — %s", result.ArtistName, result.TrackName)
		b.followUpEmbed(s, i, infoEmbed(title, "🎼 This track is **instrumental** — no lyrics."))
		return
	}

	lyricsText := strings.TrimSpace(result.PlainLyrics)
	if lyricsText == "" {
		b.followUpEmbed(s, i, warnEmbed("No Lyrics Found", fmt.Sprintf("Found **%s — %s** but no plain lyrics are available.", result.ArtistName, result.TrackName)))
		return
	}

	// Discord embed description limit is 4096 chars. Leave room for title and footer.
	const maxLen = 4000
	if len(lyricsText) > maxLen {
		lyricsText = lyricsText[:maxLen-10] + "\n\n…"
	}

	title := fmt.Sprintf("🎵 %s — %s", result.ArtistName, result.TrackName)
	footer := ""
	if result.AlbumName != "" {
		footer = "Album: " + result.AlbumName
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: lyricsText,
		Color:       colorGreen,
	}
	if footer != "" {
		embed.Footer = &discordgo.MessageEmbedFooter{Text: footer}
	}

	b.followUpEmbed(s, i, embed)
}

func (b *Bot) cmdMove(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.cfg.MusicEnabled {
		b.respondEphemeralEmbed(s, i, warnEmbed("Music Disabled", "Music commands are disabled."))
		return
	}
	data := i.ApplicationCommandData()
	from := admin.IntOption(data.Options, "from", 0)
	to := admin.IntOption(data.Options, "to", 0)

	if from < 1 || to < 1 {
		b.respondEphemeralEmbed(s, i, warnEmbed("Invalid Position", "Positions must be at least 1. Use `/queue` to see positions."))
		return
	}

	track, ok := b.music.Move(i.GuildID, int(from), int(to))
	if !ok {
		queueLen := b.music.Len(i.GuildID)
		b.respondEphemeralEmbed(s, i, warnEmbed("Invalid Move", fmt.Sprintf("Check positions (queue has %d tracks). Use `/queue` to see valid positions.", queueLen)))
		return
	}

	b.respondEphemeralEmbed(s, i, successEmbed("📝 Moved", fmt.Sprintf("Moved **%s** from position %d to position %d.", track.Display(), from, to)))
}
