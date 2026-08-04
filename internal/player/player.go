// Package player resolves audio sources and streams Opus frames to a Discord
// voice connection. It uses yt-dlp to resolve URLs/search queries and ffmpeg
// to transcode to Opus.
//
// Pipeline:
//  1. yt-dlp resolves the query/URL to a direct streamable audio URL + metadata
//  2. ffmpeg reads that URL and transcodes to raw PCM (48kHz stereo s16le)
//  3. PCM frames are encoded to Opus via gopus
//  4. Opus frames are sent to the Discord voice connection
//
// Search priority for plain text queries: SoundCloud first (YouTube search is
// often IP-blocked), then YouTube as fallback. Direct URLs (YouTube, SoundCloud,
// Spotify) are passed through as-is. Spotify links are resolved to track titles
// then searched via SoundCloud.
package player

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
)

const (
	channels    = 2                          // stereo
	frameRate   = 48000                      // 48kHz — Discord's required sample rate
	frameSize   = 960                        // 20ms at 48kHz
	maxBytes    = (frameSize * channels) * 2 // max bytes per PCM frame (s16le)
	opusBitrate = 128000                     // 128 kbps, in bits per second (what Opus expects)
)

// Player wraps an Opus encoder and manages a single playback stream per guild.
type Player struct {
	ytdlpPath  string
	ffmpegPath string

	mu        sync.Mutex
	stopChans map[string]chan struct{}
	playing   map[string]bool
	volume    map[string]float64 // per-guild volume (0.0 to 2.0, default 1.0)
}

// New creates a Player, auto-detecting yt-dlp and ffmpeg from PATH.
func New(ytdlpPath, ffmpegPath string) *Player {
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &Player{
		ytdlpPath:  ytdlpPath,
		ffmpegPath: ffmpegPath,
		stopChans:  make(map[string]chan struct{}),
		playing:    make(map[string]bool),
		volume:     make(map[string]float64),
	}
}

// YtdlpPath returns the configured yt-dlp path (for testing).
func (p *Player) YtdlpPath() string { return p.ytdlpPath }

// FfmpegPath returns the configured ffmpeg path (for testing).
func (p *Player) FfmpegPath() string { return p.ffmpegPath }

// IsPlaying returns whether a guild currently has active playback.
func (p *Player) IsPlaying(guildID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing[guildID]
}

// ResolvedTrack holds metadata + streamable URL for a track.
type ResolvedTrack struct {
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Duration  float64 `json:"duration"`
	Artist    string  `json:"artist"`
	Thumbnail string  `json:"thumbnail"`
	// WebpageURL is the human-visitable page for the track (the YouTube watch
	// page, SoundCloud track page, …) as opposed to URL, which is the raw
	// media stream. Used to make embed titles clickable.
	WebpageURL string `json:"webpage_url"`
	// Source is the yt-dlp extractor key, e.g. "youtube" or "soundcloud".
	Source string `json:"source"`
	// ViewCount and Uploaded are best-effort extras for richer embeds.
	ViewCount int64  `json:"view_count"`
	Uploaded  string `json:"upload_date"`
}

// ytDlpJSON is the subset of yt-dlp --dump-json output we need.
type ytDlpJSON struct {
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Duration     float64 `json:"duration"`
	Uploader     string  `json:"uploader"`
	Thumbnail    string  `json:"thumbnail"`
	WebpageURL   string  `json:"webpage_url"`
	ExtractorKey string  `json:"extractor_key"`
	Extractor    string  `json:"extractor"`
	ViewCount    int64   `json:"view_count"`
	UploadDate   string  `json:"upload_date"`
}

var (
	spotifyRegexp    = regexp.MustCompile(`https?://open\.spotify\.com/(track|album|playlist)/[a-zA-Z0-9]+`)
	youtubeRegexp    = regexp.MustCompile(`https?://(www\.)?(youtube\.com/watch\?v=|youtu\.be/)[a-zA-Z0-9_-]+`)
	soundcloudRegexp = regexp.MustCompile(`https?://soundcloud\.com/`)
)

// isSpotifyLink returns true if the query is a Spotify URL.
func isSpotifyLink(query string) bool {
	return spotifyRegexp.MatchString(query)
}

// isDirectURL returns true if the query is a recognized URL format.
func isDirectURL(query string) bool {
	return youtubeRegexp.MatchString(query) ||
		soundcloudRegexp.MatchString(query) ||
		spotifyRegexp.MatchString(query) ||
		strings.HasPrefix(query, "http://") ||
		strings.HasPrefix(query, "https://")
}

// isYouTubeURL returns true if the query is a YouTube watch or youtu.be link.
func isYouTubeURL(query string) bool {
	return youtubeRegexp.MatchString(query)
}

// isYouTubeBotBlock checks whether yt-dlp output indicates YouTube bot detection.
//
// yt-dlp renders the message with a Unicode right single quote ("you’re"), not
// an ASCII apostrophe, so both variants are normalized before matching.
func isYouTubeBotBlock(output []byte) bool {
	s := strings.ToLower(string(output))
	s = strings.NewReplacer("\u2019", "'", "\u02bc", "'", "\u00b4", "'", "`", "'").Replace(s)
	return strings.Contains(s, "sign in to confirm you're not a bot")
}

// Resolve uses yt-dlp to get the direct streamable URL and metadata for a query.
// Search priority for plain text / Spotify: SoundCloud first (YouTube search is
// often IP-blocked with "Sign in to confirm you're not a bot"), then YouTube as
// fallback. Direct URLs (YouTube, SoundCloud, etc.) are passed through as-is.
// If YouTube blocks yt-dlp with bot detection, the video title is fetched via
// YouTube's public oEmbed API and searched on SoundCloud as a fallback.
func (p *Player) Resolve(ctx context.Context, query string) (*ResolvedTrack, error) {
	log := logging.Component("player")

	resolvedQuery := query
	var spotifyMeta *spotifyEmbedData
	if isSpotifyLink(query) {
		log.Info("resolving spotify link", "query", query)
		// Primary: scrape the Spotify embed page for title + artist + thumbnail + duration.
		if data, err := spotifyEmbedTrack(ctx, query); err == nil && data.Title != "" {
			log.Info("found spotify track via embed page", "title", data.Title, "artist", data.Artist)
			resolvedQuery = data.Title
			if data.Artist != "" {
				resolvedQuery = data.Artist + " - " + data.Title
			}
			spotifyMeta = data
		} else {
			log.Warn("spotify embed page failed, falling back to yt-dlp", "err", err)
			// Fallback: try yt-dlp metadata extraction (will likely fail with DRM, but try)
			metaArgs := []string{
				"--dump-json",
				"--no-warnings",
				"--skip-download",
			}
			metaArgs = append(metaArgs, query)
			metaCmd := exec.CommandContext(ctx, p.ytdlpPath, metaArgs...)
			metaOut, metaErr := metaCmd.CombinedOutput()
			if metaErr == nil {
				var meta ytDlpJSON
				if json.Unmarshal(metaOut, &meta) == nil && meta.Title != "" {
					log.Info("found spotify track title via yt-dlp", "title", meta.Title)
					resolvedQuery = meta.Title
				}
			}
		}
		// If both fail, resolvedQuery stays as the original Spotify URL (will be treated as direct URL)
	}

	// For plain text queries (not direct URLs), search via SoundCloud first,
	// then fall back to YouTube search if SoundCloud fails.
	if !isDirectURL(resolvedQuery) {
		// Try SoundCloud search first
		scQuery := "scsearch1:" + resolvedQuery
		log.Info("resolving search query via SoundCloud", "query", resolvedQuery)

		args := []string{
			"--dump-json",
			"--no-playlist",
			"--no-warnings",
			"--format", "bestaudio/best",
			"--no-check-certificate",
		}
		args = append(args, scQuery)

		log.Debug("running yt-dlp", "path", p.ytdlpPath, "query", scQuery)
		cmd := exec.CommandContext(ctx, p.ytdlpPath, args...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			result, parseErr := parseYtDlpJSON(output)
			if parseErr == nil {
				log.Info("resolved track via SoundCloud", "title", result.Title, "url", result.URL, "duration", result.Duration)
				enrichWithSpotify(result, spotifyMeta, query)
				return result, nil
			}
			log.Warn("soundcloud search returned data but parse failed", "err", parseErr)
		} else {
			log.Warn("soundcloud search failed, trying YouTube", "query", resolvedQuery, "err", err)
		}

		// Fall back to YouTube search
		ytQuery := "ytsearch1:" + resolvedQuery
		log.Info("falling back to YouTube search", "query", resolvedQuery)

		args = []string{
			"--dump-json",
			"--no-playlist",
			"--no-warnings",
			"--format", "bestaudio/best",
			"--no-check-certificate",
		}
		args = append(args, ytQuery)

		log.Debug("running yt-dlp", "path", p.ytdlpPath, "query", ytQuery)
		cmd = exec.CommandContext(ctx, p.ytdlpPath, args...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			if isYouTubeBotBlock(output) {
				log.Error("youtube search bot-blocked and soundcloud search failed", "query", resolvedQuery)
				return nil, fmt.Errorf("could not find %q: SoundCloud had no match and YouTube blocked the request (bot check)", resolvedQuery)
			}
			log.Error("yt-dlp failed (both SoundCloud and YouTube)", "query", resolvedQuery, "err", err, "output", string(output))
			return nil, fmt.Errorf("yt-dlp resolve failed: %w: %s", err, strings.TrimSpace(string(output)))
		}
		result, parseErr := parseYtDlpJSON(output)
		if parseErr != nil {
			return nil, parseErr
		}
		enrichWithSpotify(result, spotifyMeta, query)
		return result, nil
	}

	// Direct URL — pass through to yt-dlp as-is
	args := []string{
		"--dump-json",
		"--no-playlist",
		"--no-warnings",
		"--format", "bestaudio/best",
		"--no-check-certificate",
	}
	args = append(args, resolvedQuery)

	log.Debug("running yt-dlp", "path", p.ytdlpPath, "query", resolvedQuery)
	cmd := exec.CommandContext(ctx, p.ytdlpPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If YouTube blocked us with bot detection, try to fall back to
		// SoundCloud search using the video title from YouTube's oEmbed API.
		if isYouTubeURL(resolvedQuery) && isYouTubeBotBlock(output) {
			log.Warn("youtube bot-blocked, falling back to SoundCloud search", "query", resolvedQuery)
			if title := youtubeOEmbedTitle(ctx, resolvedQuery); title != "" {
				log.Info("found youtube title via oEmbed, searching SoundCloud", "title", title)
				scQuery := "scsearch1:" + title
				scArgs := []string{
					"--dump-json",
					"--no-playlist",
					"--no-warnings",
					"--format", "bestaudio/best",
					"--no-check-certificate",
				}
				scArgs = append(scArgs, scQuery)
				scCmd := exec.CommandContext(ctx, p.ytdlpPath, scArgs...)
				scOutput, scErr := scCmd.CombinedOutput()
				if scErr == nil {
					log.Info("soundcloud fallback succeeded", "title", title)
					result, parseErr := parseYtDlpJSON(scOutput)
					if parseErr != nil {
						return nil, parseErr
					}
					enrichWithSpotify(result, spotifyMeta, query)
					return result, nil
				}
				log.Error("soundcloud fallback also failed", "title", title, "err", scErr, "output", string(scOutput))
			} else {
				log.Warn("could not get youtube title via oEmbed, cannot fall back", "query", resolvedQuery)
			}
		}
		log.Error("yt-dlp failed", "query", resolvedQuery, "err", err, "output", string(output))
		return nil, fmt.Errorf("yt-dlp resolve failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	result, parseErr := parseYtDlpJSON(output)
	if parseErr != nil {
		return nil, parseErr
	}
	enrichWithSpotify(result, spotifyMeta, query)
	return result, nil
}

// enrichWithSpotify overlays Spotify metadata (artist, thumbnail, duration,
// source, webpage URL) onto a resolved track. The yt-dlp result for a
// SoundCloud/YouTube search won't have the Spotify artwork, so we use the
// Spotify embed data for richer embeds.
func enrichWithSpotify(rt *ResolvedTrack, meta *spotifyEmbedData, originalQuery string) {
	if rt == nil || meta == nil {
		return
	}
	// Use the Spotify title as the display title for accuracy.
	if meta.Title != "" {
		rt.Title = meta.Title
	}
	if meta.Artist != "" {
		rt.Artist = meta.Artist
	}
	if meta.Thumbnail != "" {
		rt.Thumbnail = meta.Thumbnail
	}
	if meta.Duration > 0 {
		rt.Duration = meta.Duration
	}
	rt.Source = "Spotify"
	// Keep the original Spotify URL as the webpage link so embeds link to Spotify.
	if isSpotifyLink(originalQuery) {
		rt.WebpageURL = originalQuery
	}
}

// youtubeOEmbedTitle uses the public YouTube oEmbed endpoint to get a video title
// without authentication. Returns empty string on failure.
func youtubeOEmbedTitle(ctx context.Context, youtubeURL string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	oembedURL := "https://www.youtube.com/oembed?url=" + youtubeURL + "&format=json"
	req, err := http.NewRequestWithContext(ctx, "GET", oembedURL, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var result struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	return result.Title
}

// spotifyEmbedData holds the metadata extracted from a Spotify embed page.
type spotifyEmbedData struct {
	Title     string
	Artist    string
	Thumbnail string
	Duration  float64 // seconds
}

// spotifyTrackIDRegexp extracts the track ID from a Spotify track URL.
var spotifyTrackIDRegexp = regexp.MustCompile(`https?://open\.spotify\.com/track/([a-zA-Z0-9]+)`)

// spotifyEmbedTrack fetches the Spotify embed page for a single track and
// parses the __NEXT_DATA__ JSON to extract title, artist, duration, and
// thumbnail. This is the primary resolution path for Spotify track links —
// the old oEmbed endpoint is dead (returns 404) and yt-dlp refuses Spotify
// (DRM). The embed page is the only reliable no-auth source.
func spotifyEmbedTrack(ctx context.Context, spotifyURL string) (*spotifyEmbedData, error) {
	m := spotifyTrackIDRegexp.FindStringSubmatch(spotifyURL)
	if len(m) < 2 {
		return nil, fmt.Errorf("unrecognised Spotify track URL")
	}
	trackID := m[1]
	embedURL := fmt.Sprintf("https://open.spotify.com/embed/track/%s", trackID)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", embedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("spotify embed returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Extract __NEXT_DATA__ JSON from the embed page.
	re := regexp.MustCompile(`__NEXT_DATA__[^>]*>(.*?)</script>`)
	match := re.FindSubmatch(body)
	if match == nil {
		return nil, fmt.Errorf("spotify embed page has no __NEXT_DATA__")
	}

	var nextData struct {
		Props struct {
			PageProps struct {
				State struct {
					Data struct {
						Entity struct {
							Name     string `json:"name"`
							Title    string `json:"title"`
							Duration int    `json:"duration"` // milliseconds
							Artists  []struct {
								Name string `json:"name"`
							} `json:"artists"`
							VisualIdentity struct {
								Image []struct {
									URL       string `json:"url"`
									MaxWidth  int    `json:"maxWidth"`
								} `json:"image"`
							} `json:"visualIdentity"`
						} `json:"entity"`
					} `json:"data"`
				} `json:"state"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(match[1], &nextData); err != nil {
		return nil, fmt.Errorf("parse spotify embed json: %w", err)
	}

	entity := nextData.Props.PageProps.State.Data.Entity
	if entity.Name == "" && entity.Title == "" {
		return nil, fmt.Errorf("spotify embed returned no track data")
	}

	title := entity.Name
	if title == "" {
		title = entity.Title
	}

	var artist string
	if len(entity.Artists) > 0 {
		artist = entity.Artists[0].Name
	}

	// Pick the largest available thumbnail.
	var thumbnail string
	bestWidth := 0
	for _, img := range entity.VisualIdentity.Image {
		if img.MaxWidth > bestWidth {
			bestWidth = img.MaxWidth
			thumbnail = img.URL
		}
	}

	return &spotifyEmbedData{
		Title:     title,
		Artist:    artist,
		Thumbnail: thumbnail,
		Duration:  float64(entity.Duration) / 1000.0,
	}, nil
}

// parseYtDlpJSON parses the first JSON line from yt-dlp --dump-json output.
func parseYtDlpJSON(output []byte) (*ResolvedTrack, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, errors.New("yt-dlp returned empty output")
	}

	var info ytDlpJSON
	if err := json.Unmarshal([]byte(lines[0]), &info); err != nil {
		return nil, fmt.Errorf("parse yt-dlp json: %w", err)
	}
	if info.URL == "" {
		return nil, errors.New("yt-dlp returned empty URL")
	}

	return &ResolvedTrack{
		Title:      info.Title,
		URL:        info.URL,
		Duration:   info.Duration,
		Artist:     info.Uploader,
		Thumbnail:  info.Thumbnail,
		WebpageURL: info.WebpageURL,
		Source:     normalizeSource(info.ExtractorKey, info.Extractor),
		ViewCount:  info.ViewCount,
		Uploaded:   info.UploadDate,
	}, nil
}

// normalizeSource turns a yt-dlp extractor key into a friendly source label.
func normalizeSource(key, fallback string) string {
	s := strings.ToLower(strings.TrimSpace(key))
	if s == "" {
		s = strings.ToLower(strings.TrimSpace(fallback))
	}
	switch {
	case strings.HasPrefix(s, "youtube"):
		return "YouTube"
	case strings.HasPrefix(s, "soundcloud"):
		return "SoundCloud"
	case strings.HasPrefix(s, "spotify"):
		return "Spotify"
	case s == "":
		return ""
	default:
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

// Play resolves the track and streams audio to the voice connection.
// It blocks until playback finishes, is stopped, or encounters an error.
// The onDone callback is called when playback ends (for auto-advancing the queue).
func (p *Player) Play(ctx context.Context, vc *discordgo.VoiceConnection, guildID, query string, onDone func()) error {
	return p.PlayResolved(ctx, vc, guildID, query, nil, onDone)
}

// PlayResolved streams a track that may already be resolved. Passing a non-nil
// pre argument skips the yt-dlp resolve step entirely, which is what removes the
// audible gap between the "now playing" message and the first audio frame.
func (p *Player) PlayResolved(ctx context.Context, vc *discordgo.VoiceConnection, guildID, query string, pre *ResolvedTrack, onDone func()) error {
	log := logging.Component("player")

	if vc == nil {
		return errors.New("voice connection is nil")
	}
	if !vc.Ready {
		log.Warn("voice connection not ready, waiting", "guild", guildID)
		for i := 0; i < 100; i++ {
			if vc.Ready {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		if !vc.Ready {
			return errors.New("voice connection never became ready")
		}
	}

	// Stop any existing playback for this guild
	p.Stop(guildID)

	// Use the pre-resolved track when the caller already did the work.
	track := pre
	if track == nil || track.URL == "" {
		resolveCtx, resolveCancel := context.WithTimeout(ctx, 60*time.Second)
		defer resolveCancel()

		var err error
		track, err = p.Resolve(resolveCtx, query)
		if err != nil {
			return fmt.Errorf("resolve: %w", err)
		}
	}

	// Create stop channel
	stopChan := make(chan struct{}, 1)
	p.mu.Lock()
	p.stopChans[guildID] = stopChan
	p.playing[guildID] = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.stopChans, guildID)
		p.playing[guildID] = false
		p.mu.Unlock()
		if onDone != nil {
			onDone()
		}
	}()

	log.Info("starting playback", "guild", guildID, "track", track.Title, "url", track.URL)

	// Build ffmpeg command to transcode to PCM.
	// No -re flag: let ffmpeg decode as fast as possible so audio starts immediately.
	// -reconnect: retry on network hiccups
	ffmpegArgs := []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", track.URL,
		"-f", "s16le",
		"-ac", fmt.Sprintf("%d", channels),
		"-ar", fmt.Sprintf("%d", frameRate),
		"-loglevel", "warning",
		"pipe:1",
	}

	ffmpegCmd := exec.CommandContext(ctx, p.ffmpegPath, ffmpegArgs...)
	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	stderrBuf := &threadSafeBuffer{}
	ffmpegCmd.Stderr = stderrBuf

	if err := ffmpegCmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	defer func() {
		_ = ffmpegCmd.Process.Kill()
		_ = ffmpegCmd.Wait()
	}()

	// Create Opus encoder
	encoder, err := gopus.NewEncoder(frameRate, channels, gopus.Audio)
	if err != nil {
		return fmt.Errorf("opus encoder: %w", err)
	}
	// gopus passes this straight to opus_encoder_ctl(OPUS_SET_BITRATE), which
	// takes bits per second — not kbps. Passing 128 here yields 128 bps, which
	// Opus clamps to its floor and renders as inaudible silence.
	encoder.SetBitrate(opusBitrate)

	// Speaking flag
	vc.Speaking(true)
	defer vc.Speaking(false)

	// Frame pacing: Discord expects 50 Opus frames per second (20ms each).
	// We use a ticker to ensure we send frames at the correct rate even if
	// ffmpeg's -re flag doesn't perfectly pace the output.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	// Read PCM frames, encode to Opus, send to Discord
	pcmBuf := make([]byte, maxBytes)
	frameCount := 0
	startTime := time.Now()

	for {
		select {
		case <-stopChan:
			log.Info("playback stopped", "guild", guildID, "track", track.Title, "frames", frameCount)
			return nil
		case <-ctx.Done():
			log.Info("playback cancelled", "guild", guildID, "track", track.Title)
			return ctx.Err()
		case <-ticker.C:
			// Read exactly maxBytes bytes (one PCM frame = 20ms)
			n, err := io.ReadFull(stdout, pcmBuf)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					if n > 0 {
						p.sendOpusFrame(vc, encoder, pcmBuf[:n], guildID)
						frameCount++
					}
					log.Info("playback finished naturally", "guild", guildID, "track", track.Title,
						"frames", frameCount, "duration", time.Since(startTime).Round(time.Second))
					return nil
				}
				log.Error("read pcm error", "guild", guildID, "err", err)
				return fmt.Errorf("read pcm: %w", err)
			}

			// Encode PCM to Opus and send
			if err := p.sendOpusFrame(vc, encoder, pcmBuf[:n], guildID); err != nil {
				log.Error("opus send error", "guild", guildID, "err", err, "frame", frameCount)
				return fmt.Errorf("opus send: %w", err)
			}
			frameCount++

			if frameCount%50 == 0 { // log every ~1 second
				log.Debug("playback progress", "guild", guildID, "frames", frameCount,
					"elapsed", time.Since(startTime).Round(time.Second))
			}
		}
	}
}

// sendOpusFrame encodes a PCM frame and sends it to the Discord voice connection.
func (p *Player) sendOpusFrame(vc *discordgo.VoiceConnection, encoder *gopus.Encoder, pcm []byte, guildID string) error {
	log := logging.Component("player")
	// Convert s16le bytes to int16 samples for gopus
	if len(pcm)%2 != 0 {
		return errors.New("odd PCM byte count")
	}
	samples := make([]int16, len(pcm)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
	}

	// Apply volume gain
	vol := p.GetVolume(guildID)
	if vol != 1.0 {
		for i := range samples {
			scaled := float64(samples[i]) * vol
			if scaled > 32767 {
				scaled = 32767
			} else if scaled < -32768 {
				scaled = -32768
			}
			samples[i] = int16(scaled)
		}
	}

	opus, err := encoder.Encode(samples, frameSize, maxBytes)
	if err != nil {
		return fmt.Errorf("opus encode: %w", err)
	}
	if !vc.Ready || vc.OpusSend == nil {
		// Voice connection dropped (disconnect, server move, etc.).
		// Signal the stop channel so the playback loop exits cleanly
		// via the stopChan case (returns nil, not an error), and the
		// onDone callback fires so advanceOrFinish handles it.
		log.Warn("voice connection gone during playback, stopping gracefully", "guild", guildID)
		p.mu.Lock()
		if ch, ok := p.stopChans[guildID]; ok {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		p.mu.Unlock()
		return nil
	}
	vc.OpusSend <- opus
	return nil
}

// Stop stops playback for the given guild.
func (p *Player) Stop(guildID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ch, ok := p.stopChans[guildID]; ok {
		select {
		case ch <- struct{}{}:
		default:
		}
		delete(p.stopChans, guildID)
	}
	p.playing[guildID] = false
}

// threadSafeBuffer is a simple io.Writer that is safe for concurrent reads/writes.
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// SetVolume sets the playback volume for a guild (0.0 to 2.0, default 1.0).
// Values above 1.0 amplify, below 1.0 attenuate.
func (p *Player) SetVolume(guildID string, vol float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if vol < 0.0 {
		vol = 0.0
	}
	if vol > 2.0 {
		vol = 2.0
	}
	p.volume[guildID] = vol
}

// GetVolume returns the current volume for a guild (default 1.0).
func (p *Player) GetVolume(guildID string) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	vol, ok := p.volume[guildID]
	if !ok {
		return 1.0
	}
	return vol
}
