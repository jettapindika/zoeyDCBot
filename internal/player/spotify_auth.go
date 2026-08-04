package player

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SpotifyAuth manages OAuth2 Client Credentials tokens for the Spotify Web
// API. The token is cached and refreshed automatically before it expires.
// All methods are safe for concurrent use.
type SpotifyAuth struct {
	clientID     string
	clientSecret string

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
}

// NewSpotifyAuth creates a token manager for the given credentials. The token
// is fetched lazily on the first call to Token().
func NewSpotifyAuth(clientID, clientSecret string) *SpotifyAuth {
	return &SpotifyAuth{
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// Enabled reports whether credentials were provided.
func (a *SpotifyAuth) Enabled() bool {
	return a != nil && a.clientID != "" && a.clientSecret != ""
}

// Token returns a valid access token, fetching a new one (or refreshing) if
// the cached token is missing or within the refresh margin of expiry.
func (a *SpotifyAuth) Token(ctx context.Context) (string, error) {
	if !a.Enabled() {
		return "", fmt.Errorf("spotify credentials not configured")
	}

	a.mu.Lock()
	// Refresh 60s before actual expiry to avoid edge races.
	if a.cachedToken != "" && time.Now().Before(a.expiresAt.Add(-60*time.Second)) {
		token := a.cachedToken
		a.mu.Unlock()
		return token, nil
	}
	a.mu.Unlock()

	return a.refresh(ctx)
}

// refresh fetches a new client-credentials token from Spotify's token endpoint
// and caches it.
func (a *SpotifyAuth) refresh(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring the lock — another goroutine may have
	// refreshed while we were waiting.
	if a.cachedToken != "" && time.Now().Before(a.expiresAt.Add(-60*time.Second)) {
		return a.cachedToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://accounts.spotify.com/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build spotify token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("spotify token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		_, _ = fmt.Fscanf(resp.Body, "")
		body.ReadFrom(resp.Body)
		return "", fmt.Errorf("spotify token endpoint returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(body.String()))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // seconds
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse spotify token response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("spotify token response had no access_token")
	}

	a.cachedToken = result.AccessToken
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	a.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	slog.Info("spotify access token refreshed", "expires_in_s", expiresIn)
	return a.cachedToken, nil
}
