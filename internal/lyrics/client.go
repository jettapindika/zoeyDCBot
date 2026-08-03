// Package lyrics fetches song lyrics from the keyless lrclib.net API.
//
// Endpoint: GET https://lrclib.net/api/search?track_name=<track>&artist_name=<artist>
// Returns a JSON array of matches, each with plainLyrics, syncedLyrics,
// artistName, trackName, albumName, duration, and instrumental fields.
package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result is a single lyrics match from lrclib.
type Result struct {
	TrackName     string `json:"trackName"`
	ArtistName    string `json:"artistName"`
	AlbumName     string `json:"albumName"`
	Duration      float64 `json:"duration"`
	Instrumental  bool   `json:"instrumental"`
	PlainLyrics   string `json:"plainLyrics"`
	SyncedLyrics  string `json:"syncedLyrics"`
}

// Client queries lrclib.net for lyrics.
type Client struct {
	http    *http.Client
	baseURL string
}

// New creates a lyrics client with a 10-second timeout.
func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: "https://lrclib.net/api",
	}
}

// Search fetches lyrics for a track. If artist is non-empty, it is included
// in the query for better matching. Returns the best match (first result)
// or an error if no lyrics are found.
func (c *Client) Search(ctx context.Context, track, artist string) (*Result, error) {
	track = strings.TrimSpace(track)
	if track == "" {
		return nil, fmt.Errorf("track name is required")
	}

	params := url.Values{}
	params.Set("track_name", track)
	if artist = strings.TrimSpace(artist); artist != "" {
		params.Set("artist_name", artist)
	}

	reqURL := c.baseURL + "/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lyrics request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("lrclib returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var results []Result
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode lyrics response: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no lyrics found for %q", track)
	}

	// Return the first result (lrclib ranks by relevance).
	return &results[0], nil
}
