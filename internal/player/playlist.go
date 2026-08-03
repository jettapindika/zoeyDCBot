package player

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// PlaylistTrack is a lightweight track entry returned by FetchPlaylist.
type PlaylistTrack struct {
	Title     string
	Artist    string
	URL       string // original URL or search query to pass to Resolve
	Thumbnail string
	Duration  float64 // seconds
}

var (
	youtubePlaylistRegexp = regexp.MustCompile(`[?&]list=([a-zA-Z0-9_-]+)`)
	soundcloudSetRegexp   = regexp.MustCompile(`https?://soundcloud\.com/[^/]+/sets/`)
	spotifyPlaylistRegexp = regexp.MustCompile(`https?://open\.spotify\.com/(album|playlist)/([a-zA-Z0-9]+)`)
)

// IsPlaylist returns true if the query looks like a multi-track playlist URL.
func IsPlaylist(query string) bool {
	return youtubePlaylistRegexp.MatchString(query) ||
		soundcloudSetRegexp.MatchString(query) ||
		spotifyPlaylistRegexp.MatchString(query)
}

// FetchPlaylist expands a playlist URL into individual tracks.
// For Spotify it scrapes the embed page (no auth needed).
// For YouTube/SoundCloud it uses yt-dlp --flat-playlist.
// Returns at most maxTracks entries (0 = no limit).
func (p *Player) FetchPlaylist(ctx context.Context, query string, maxTracks int) ([]PlaylistTrack, string, error) {
	if spotifyPlaylistRegexp.MatchString(query) {
		return fetchSpotifyPlaylist(ctx, query, maxTracks)
	}
	return p.fetchYtdlpPlaylist(ctx, query, maxTracks)
}

// fetchSpotifyPlaylist scrapes the Spotify embed page for track metadata.
// Spotify's embed page works for tracks but returns 404 for albums/playlists.
// For albums/playlists, we get an anonymous access token from any track embed
// page, then use the Spotify Web API to fetch the track listing.
func fetchSpotifyPlaylist(ctx context.Context, spotifyURL string, maxTracks int) ([]PlaylistTrack, string, error) {
	m := spotifyPlaylistRegexp.FindStringSubmatch(spotifyURL)
	if len(m) < 3 {
		return nil, "", fmt.Errorf("unrecognised Spotify URL")
	}
	kind, id := m[1], m[2]

	// Try the embed page first (works for some cases).
	embedURL := fmt.Sprintf("https://open.spotify.com/embed/%s/%s", kind, id)
	tracks, name, err := fetchSpotifyEmbedPlaylist(ctx, embedURL, maxTracks)
	if err == nil && len(tracks) > 0 {
		return tracks, name, nil
	}

	// Embed page failed (likely 404 for albums/playlists). Use the anonymous
	// token from a track embed page + Spotify Web API.
	token, tokenErr := spotifyAnonymousToken(ctx)
	if tokenErr != nil {
		return nil, "", fmt.Errorf("spotify embed page failed (%v) and could not get anonymous token (%v)", err, tokenErr)
	}

	return fetchSpotifyPlaylistViaAPI(ctx, kind, id, token, maxTracks)
}

// fetchSpotifyEmbedPlaylist scrapes the __NEXT_DATA__ JSON from a Spotify embed
// page. Works for track embeds; returns error for albums/playlists (404).
func fetchSpotifyEmbedPlaylist(ctx context.Context, embedURL string, maxTracks int) ([]PlaylistTrack, string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", embedURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	re := regexp.MustCompile(`__NEXT_DATA__[^>]*>(.*?)</script>`)
	match := re.FindSubmatch(body)
	if match == nil {
		return nil, "", fmt.Errorf("could not find Spotify embed data")
	}

	var nextData struct {
		Props struct {
			PageProps struct {
				State struct {
					Data struct {
						Entity struct {
							Name      string `json:"name"`
							TrackList []struct {
								Title    string `json:"title"`
								Subtitle string `json:"subtitle"`
								Duration int    `json:"duration"`
							} `json:"trackList"`
						} `json:"entity"`
					} `json:"data"`
				} `json:"state"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(match[1], &nextData); err != nil {
		return nil, "", fmt.Errorf("parse Spotify embed: %w", err)
	}

	entity := nextData.Props.PageProps.State.Data.Entity
	playlistName := entity.Name
	tracks := make([]PlaylistTrack, 0, len(entity.TrackList))
	for i, t := range entity.TrackList {
		if maxTracks > 0 && i >= maxTracks {
			break
		}
		searchQuery := t.Title
		if t.Subtitle != "" {
			searchQuery = t.Title + " " + t.Subtitle
		}
		tracks = append(tracks, PlaylistTrack{
			Title:    t.Title,
			Artist:   t.Subtitle,
			URL:      searchQuery,
			Duration: float64(t.Duration) / 1000.0,
		})
	}
	if len(tracks) == 0 {
		return nil, "", fmt.Errorf("Spotify playlist is empty or could not be parsed")
	}
	return tracks, playlistName, nil
}

// spotifyAnonymousToken fetches an anonymous access token from any Spotify
// track embed page. The token can be used with the Spotify Web API.
func spotifyAnonymousToken(ctx context.Context) (string, error) {
	// Use a known-good track embed page to get the token.
	embedURL := "https://open.spotify.com/embed/track/5SmXEPnevlRjBPWBG7oKIi"
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", embedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`__NEXT_DATA__[^>]*>(.*?)</script>`)
	match := re.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("no __NEXT_DATA__ in embed page")
	}

	var nextData struct {
		Props struct {
			PageProps struct {
				State struct {
					Settings struct {
						Session struct {
							AccessToken string `json:"accessToken"`
						} `json:"session"`
					} `json:"settings"`
				} `json:"state"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(match[1], &nextData); err != nil {
		return "", fmt.Errorf("parse token json: %w", err)
	}

	token := nextData.Props.PageProps.State.Settings.Session.AccessToken
	if token == "" {
		return "", fmt.Errorf("no access token in embed page")
	}
	return token, nil
}

// fetchSpotifyPlaylistViaAPI uses the Spotify Web API with an anonymous token
// to fetch album or playlist tracks. The anonymous token is rate-limited, so
// we retry with backoff on 429.
func fetchSpotifyPlaylistViaAPI(ctx context.Context, kind, id, token string, maxTracks int) ([]PlaylistTrack, string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	var apiURL string
	switch kind {
	case "album":
		apiURL = fmt.Sprintf("https://api.spotify.com/v1/albums/%s?market=US", id)
	case "playlist":
		apiURL = fmt.Sprintf("https://api.spotify.com/v1/playlists/%s?market=US&fields=name,tracks(items(track(name,artists(name),duration_ms,id)))", id)
	default:
		return nil, "", fmt.Errorf("unsupported Spotify type: %s", kind)
	}

	// Retry with backoff on rate limit (429).
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(time.Duration(attempt*3) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "Mozilla/5.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == 429 {
			resp.Body.Close()
			lastErr = fmt.Errorf("spotify API rate-limited (429)")
			continue
		}
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("spotify API returned HTTP %d: %s", resp.StatusCode, string(body))
			continue
		}

		var result struct {
			Name   string `json:"name"`
			Tracks struct {
				Items []struct {
					Track struct {
						Name       string `json:"name"`
						ID         string `json:"id"`
						DurationMs int    `json:"duration_ms"`
						Artists   []struct {
							Name string `json:"name"`
						} `json:"artists"`
					} `json:"track"`
				} `json:"items"`
			} `json:"tracks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, "", fmt.Errorf("parse spotify API response: %w", err)
		}
		resp.Body.Close()

		playlistName := result.Name
		tracks := make([]PlaylistTrack, 0, len(result.Tracks.Items))
		for i, item := range result.Tracks.Items {
			if maxTracks > 0 && i >= maxTracks {
				break
			}
			t := item.Track
			if t.Name == "" {
				continue
			}
			artist := ""
			if len(t.Artists) > 0 {
				artist = t.Artists[0].Name
			}
			searchQuery := t.Name
			if artist != "" {
				searchQuery = t.Name + " " + artist
			}
			tracks = append(tracks, PlaylistTrack{
				Title:    t.Name,
				Artist:   artist,
				URL:      searchQuery,
				Duration: float64(t.DurationMs) / 1000.0,
			})
		}
		if len(tracks) == 0 {
			return nil, "", fmt.Errorf("spotify %s has no playable tracks", kind)
		}
		return tracks, playlistName, nil
	}

	return nil, "", fmt.Errorf("spotify API failed after retries: %v", lastErr)
}

// fetchYtdlpPlaylist uses yt-dlp --flat-playlist to expand a YouTube or
// SoundCloud playlist into individual track entries.
func (p *Player) fetchYtdlpPlaylist(ctx context.Context, query string, maxTracks int) ([]PlaylistTrack, string, error) {
	args := []string{
		"--flat-playlist",
		"--dump-json",
		"--no-warnings",
		"--skip-download",
	}
	if maxTracks > 0 {
		args = append(args, fmt.Sprintf("--playlist-end=%d", maxTracks))
	}
	args = append(args, query)

	cmd := exec.CommandContext(ctx, p.ytdlpPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("yt-dlp playlist fetch failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	tracks := make([]PlaylistTrack, 0, len(lines))
	playlistName := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Title      string `json:"title"`
			URL        string `json:"url"`
			Duration   int    `json:"duration"`
			Uploader   string `json:"uploader"`
			Thumbnail  string `json:"thumbnails"`
			Playlist   string `json:"playlist"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil {
			if playlistName == "" {
				playlistName = entry.Playlist
			}
			url := entry.URL
			if url == "" {
				url = entry.Title
			}
			tracks = append(tracks, PlaylistTrack{
				Title:    entry.Title,
				Artist:   entry.Uploader,
				URL:      url,
				Duration: float64(entry.Duration),
			})
		}
	}
	if len(tracks) == 0 {
		return nil, "", fmt.Errorf("playlist has no tracks or could not be parsed")
	}
	return tracks, playlistName, nil
}
