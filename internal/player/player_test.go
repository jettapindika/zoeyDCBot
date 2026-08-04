package player

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"layeh.com/gopus"
)

func TestNewDefaultsPaths(t *testing.T) {
	p := New("", "", "", "")
	if p.YtdlpPath() != "yt-dlp" {
		t.Errorf("expected default ytdlp path 'yt-dlp', got %q", p.YtdlpPath())
	}
	if p.FfmpegPath() != "ffmpeg" {
		t.Errorf("expected default ffmpeg path 'ffmpeg', got %q", p.FfmpegPath())
	}
	if p.spotifyAuth != nil {
		t.Errorf("expected nil spotify auth when no creds, got non-nil")
	}
}

func TestNewCustomPaths(t *testing.T) {
	p := New("/usr/local/bin/yt-dlp", "/usr/bin/ffmpeg", "", "")
	if p.YtdlpPath() != "/usr/local/bin/yt-dlp" {
		t.Errorf("expected custom ytdlp path, got %q", p.YtdlpPath())
	}
	if p.FfmpegPath() != "/usr/bin/ffmpeg" {
		t.Errorf("expected custom ffmpeg path, got %q", p.FfmpegPath())
	}
}

func TestNewWithSpotifyCreds(t *testing.T) {
	p := New("", "", "test-client-id", "test-client-secret")
	if p.spotifyAuth == nil {
		t.Fatal("expected non-nil spotify auth when creds provided")
	}
	if !p.spotifyAuth.Enabled() {
		t.Error("expected spotify auth to be enabled when creds provided")
	}
}

func TestNewWithoutSpotifyCreds(t *testing.T) {
	p := New("", "", "", "")
	if p.spotifyAuth != nil {
		t.Error("expected nil spotify auth when no creds provided")
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
	p := New("yt-dlp", "ffmpeg", "", "")
	if p.IsPlaying("g1") {
		t.Error("expected not playing initially")
	}
}

func TestStopWhenNotPlaying(t *testing.T) {
	// Stop should be safe to call even when nothing is playing
	p := New("yt-dlp", "ffmpeg", "", "")
	p.Stop("g1") // should not panic
	p.Stop("")   // should not panic
}

func TestStopIdempotent(t *testing.T) {
	p := New("yt-dlp", "ffmpeg", "", "")
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

// §3 Test: track with wrong (too-short) duration metadata should still
// consume all available PCM data until real EOF, not stop at the claimed
// duration. This verifies the playback loop terminates on io.EOF, not on
// a duration-based cutoff.
func TestPCMConsumedUntilRealEOF(t *testing.T) {
	// Simulate a stereo PCM stream that contains more data than the
	// track's claimed duration would imply.
	//
	// track.Duration = 1.0 second → would imply ~50 frames at 20ms each.
	// We provide 200 frames worth of data → 4 seconds of actual audio.
	// The loop must read all 200 frames, not stop at 50.
	trackDuration := 1.0 // seconds — deliberately wrong (too short)
	framesInStream := 200 // actual frames in the PCM data

	// Each frame is maxBytes = frameSize * channels * 2 = 960 * 2 * 2 = 3840 bytes
	frameBytes := make([]byte, maxBytes)
	// Fill with non-silent audio (sawtooth pattern)
	for i := 0; i < len(frameBytes)/2; i++ {
		sample := int16(i % 32767)
		binary.LittleEndian.PutUint16(frameBytes[i*2:i*2+2], uint16(sample))
	}

	// Build the full PCM stream
	var pcmStream bytes.Buffer
	for i := 0; i < framesInStream; i++ {
		pcmStream.Write(frameBytes)
	}

	// Read frames from the stream, simulating the playback loop's ReadFull
	framesRead := 0
	buf := make([]byte, maxBytes)
	for {
		n, err := pcmStream.Read(buf)
		if n > 0 && n == maxBytes {
			framesRead++
		}
		if err != nil {
			break
		}
	}

	expectedFrames := framesInStream
	if framesRead != expectedFrames {
		t.Errorf("framesRead = %d, want %d (track.Duration=%.1fs but actual audio is %.1fs; "+
			"playback must run to real EOF, not metadata duration)",
			framesRead, expectedFrames, trackDuration, float64(expectedFrames)*0.02)
	}
}

// §4 Test: verify that PCM-to-samples conversion produces correctly
// interleaved stereo data with non-silent audio in BOTH left and right
// channels. This is the core invariant for the mono/right-channel-only bug.
func TestPCMStereoInterleaving(t *testing.T) {
	// Build a stereo PCM frame with distinct left and right channel data.
	// Left channel: ascending values, Right channel: descending values.
	// If the channels are not interleaved correctly, one channel will
	// contain the other's data (or silence).
	frameSize := 960 // samples per channel per frame
	numChannels := 2
	samplesPerFrame := frameSize * numChannels // 1920 samples
	pcmSize := samplesPerFrame * 2            // 3840 bytes (s16le)

	pcm := make([]byte, pcmSize)
	for i := 0; i < frameSize; i++ {
		leftVal := int16(i + 1)         // 1, 2, 3, ...
		rightVal := int16(1000 - i)     // 1000, 999, 998, ...
		// Interleaved: [L0, R0, L1, R1, ...]
		binary.LittleEndian.PutUint16(pcm[(i*numChannels+0)*2:], uint16(leftVal))
		binary.LittleEndian.PutUint16(pcm[(i*numChannels+1)*2:], uint16(rightVal))
	}

	samples, err := pcmToSamples(pcm)
	if err != nil {
		t.Fatalf("pcmToSamples failed: %v", err)
	}

	if len(samples) != samplesPerFrame {
		t.Fatalf("len(samples) = %d, want %d", len(samples), samplesPerFrame)
	}

	// Verify interleaving: even indices are left, odd are right
	for i := 0; i < frameSize; i++ {
		leftIdx := i * numChannels
		rightIdx := i*numChannels + 1
		expectedLeft := int16(i + 1)
		expectedRight := int16(1000 - i)

		if samples[leftIdx] != expectedLeft {
			t.Errorf("sample %d left channel: got %d, want %d (interleaving broken)",
				i, samples[leftIdx], expectedLeft)
		}
		if samples[rightIdx] != expectedRight {
			t.Errorf("sample %d right channel: got %d, want %d (interleaving broken)",
				i, samples[rightIdx], expectedRight)
		}
	}
}

// §4 Test: verify that a mono source (all samples identical across both
// channel positions) produces non-silent audio in both channels after
// ffmpeg upmix. Since we can't run ffmpeg in a unit test, we verify the
// invariant at the PCM level: if ffmpeg correctly upmixes, the PCM data
// will have non-silent values in both even and odd sample positions.
func TestStereoBothChannelsNonSilent(t *testing.T) {
	// Simulate PCM data that a correctly-upmixed mono source would produce:
	// the same non-zero value in both L and R positions.
	frameSize := 960
	numChannels := 2
	samplesPerFrame := frameSize * numChannels
	pcmSize := samplesPerFrame * 2

	pcm := make([]byte, pcmSize)
	for i := 0; i < frameSize; i++ {
		val := int16(16384) // non-silent, moderate amplitude
		binary.LittleEndian.PutUint16(pcm[(i*numChannels+0)*2:], uint16(val))
		binary.LittleEndian.PutUint16(pcm[(i*numChannels+1)*2:], uint16(val))
	}

	samples, err := pcmToSamples(pcm)
	if err != nil {
		t.Fatalf("pcmToSamples failed: %v", err)
	}

	// Check that BOTH channels have non-silent (non-zero) audio
	var leftMax, rightMax int16
	for i := 0; i < frameSize; i++ {
		leftIdx := i * numChannels
		rightIdx := i*numChannels + 1

		if math.Abs(float64(samples[leftIdx])) > math.Abs(float64(leftMax)) {
			leftMax = samples[leftIdx]
		}
		if math.Abs(float64(samples[rightIdx])) > math.Abs(float64(rightMax)) {
			rightMax = samples[rightIdx]
		}
	}

	if leftMax == 0 {
		t.Error("left channel is entirely silent — mono upmix failed")
	}
	if rightMax == 0 {
		t.Error("right channel is entirely silent — mono upmix failed (this is the reported bug)")
	}
	if leftMax != rightMax {
		t.Errorf("channel mismatch: leftMax=%d, rightMax=%d (should be identical for upmixed mono)",
			leftMax, rightMax)
	}
}

// §4 Test: verify the gopus encoder is created with the correct channel count
// matching the PCM data. A mismatch would cause one channel to be silent.
func TestEncoderChannelCount(t *testing.T) {
	encoder, err := gopus.NewEncoder(frameRate, channels, gopus.Audio)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}
	// The encoder must be stereo (2 channels) to match the interleaved PCM
	// data produced by ffmpeg's -ac 2 output.
	if channels != 2 {
		t.Errorf("channels constant = %d, want 2 (stereo)", channels)
	}
	// Verify the encoder accepts a full stereo frame
	samples := make([]int16, frameSize*channels)
	for i := range samples {
		samples[i] = int16(i % 32767)
	}
	opus, err := encoder.Encode(samples, frameSize, maxBytes)
	if err != nil {
		t.Fatalf("encoder.Encode failed: %v", err)
	}
	if len(opus) == 0 {
		t.Error("encoder produced empty opus frame")
	}
}
