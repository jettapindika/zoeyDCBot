package player

import (
	"testing"

	"layeh.com/gopus"
)

func TestNewDefaultsPaths(t *testing.T) {
	p := New("", "")
	if p.YtdlpPath() != "yt-dlp" {
		t.Errorf("expected default ytdlp path 'yt-dlp', got %q", p.YtdlpPath())
	}
	if p.FfmpegPath() != "ffmpeg" {
		t.Errorf("expected default ffmpeg path 'ffmpeg', got %q", p.FfmpegPath())
	}
}

func TestNewCustomPaths(t *testing.T) {
	p := New("/usr/local/bin/yt-dlp", "/usr/bin/ffmpeg")
	if p.YtdlpPath() != "/usr/local/bin/yt-dlp" {
		t.Errorf("expected custom ytdlp path, got %q", p.YtdlpPath())
	}
	if p.FfmpegPath() != "/usr/bin/ffmpeg" {
		t.Errorf("expected custom ffmpeg path, got %q", p.FfmpegPath())
	}
}

func TestIsSpotifyLink(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"https://open.spotify.com/track/abc123", true},
		{"http://open.spotify.com/track/abc123", true},
		{"https://open.spotify.com/album/abc123", true},
		{"https://open.spotify.com/playlist/abc123", true},
		{"https://open.spotify.com/track/0eGsygTp906e18RXMuhluY", true},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"just a search query", false},
		{"", false},
		{"https://example.com", false},
	}
	for _, tc := range cases {
		got := isSpotifyLink(tc.query)
		if got != tc.want {
			t.Errorf("isSpotifyLink(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestIsDirectURL(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://youtu.be/dQw4w9WgXcQ", true},
		{"https://soundcloud.com/artist/song", true},
		{"https://open.spotify.com/track/abc123", true},
		{"http://example.com/audio.mp3", true},
		{"https://example.com/stream", true},
		{"just some text", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isDirectURL(tc.query)
		if got != tc.want {
			t.Errorf("isDirectURL(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestIsPlaying(t *testing.T) {
	p := New("yt-dlp", "ffmpeg")
	if p.IsPlaying("g1") {
		t.Error("expected not playing initially")
	}
}

func TestStopWhenNotPlaying(t *testing.T) {
	// Stop should be safe to call even when nothing is playing
	p := New("yt-dlp", "ffmpeg")
	p.Stop("g1") // should not panic
	p.Stop("")   // should not panic
}

func TestStopIdempotent(t *testing.T) {
	p := New("yt-dlp", "ffmpeg")
	p.Stop("g1")
	p.Stop("g1") // should not panic or block
}

func TestIsYouTubeBotBlock(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			// Regression: yt-dlp emits a Unicode right single quote (U+2019),
			// which the original ASCII-only match missed, so the SoundCloud
			// fallback never fired.
			name:   "unicode right single quote",
			output: "ERROR: [youtube] ABTTUtKtfqQ: Sign in to confirm you\u2019re not a bot. This helps protect our community.",
			want:   true,
		},
		{
			name:   "ascii apostrophe",
			output: "ERROR: [youtube] abc: Sign in to confirm you're not a bot.",
			want:   true,
		},
		{"unrelated error", "ERROR: [soundcloud] track not found", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := isYouTubeBotBlock([]byte(tc.output)); got != tc.want {
			t.Errorf("%s: isYouTubeBotBlock() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOpusBitrateIsBitsPerSecond(t *testing.T) {
	// Regression: SetBitrate goes straight to opus_encoder_ctl(OPUS_SET_BITRATE),
	// which takes bits per second. The code once passed 128 (meaning kbps), which
	// Opus clamped to its 500 bps floor — the bot joined voice and played silence.
	enc, err := gopus.NewEncoder(frameRate, channels, gopus.Audio)
	if err != nil {
		t.Fatalf("new encoder: %v", err)
	}
	enc.SetBitrate(opusBitrate)
	if got := enc.Bitrate(); got != opusBitrate {
		t.Errorf("encoder bitrate = %d, want %d (value must be bits/sec, not kbps)", got, opusBitrate)
	}
	if opusBitrate < 32000 {
		t.Errorf("opusBitrate = %d is implausibly low; it must be in bits per second", opusBitrate)
	}
}
