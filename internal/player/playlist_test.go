package player

import "testing"

func TestIsPlaylist(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"youtube playlist", "https://www.youtube.com/watch?v=abc&list=PLxyz123", true},
		{"youtube playlist only", "https://www.youtube.com/playlist?list=PLxyz123", true},
		{"soundcloud set", "https://soundcloud.com/artist/sets/my-set", true},
		{"spotify album", "https://open.spotify.com/album/03NBHdtrGXlm3VHkjFAiSY", true},
		{"spotify playlist", "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", true},
		// A single track is NOT a playlist — it must stay a one-track queue entry.
		{"spotify track", "https://open.spotify.com/track/0eGsygTp906e18RXMuhluY", false},
		{"plain youtube video", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"soundcloud track", "https://soundcloud.com/artist/song", false},
		{"search query", "bintang 5", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := IsPlaylist(tc.query); got != tc.want {
			t.Errorf("%s: IsPlaylist(%q) = %v, want %v", tc.name, tc.query, got, tc.want)
		}
	}
}

func TestFetchSpotifyPlaylistRejectsBadURL(t *testing.T) {
	p := New("", "", "", "")
	if _, _, err := p.fetchSpotifyPlaylist(nil, "https://example.com/nope", 0); err == nil {
		t.Error("expected error for non-Spotify URL")
	}
}
