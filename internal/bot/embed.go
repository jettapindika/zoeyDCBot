package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Context colors — one consistent color per feature area. These are the
// canonical names; the lowercase aliases below are kept for backward
// compatibility with existing code.
const (
	ColorMusic      = 0x1DB954 // green — now playing, queue, music success
	ColorModeration = 0xED4245 // red — admin actions, mod log
	ColorError      = 0xED4245 // red — errors (same as moderation, intentional)
	ColorAI         = 0x5865F2 // blurple — AI responses
	ColorInfo       = 0x5865F2 // blurple — general info
	ColorWarning    = 0xFEE75C // yellow — warnings
	ColorNeutral    = 0x99AAB5 // gray — neutral/empty states
)

// Backward-compatible aliases. Existing code uses these lowercase names; they
// must keep compiling and resolve to the same values as the new context colors.
const (
	colorGreen  = ColorMusic
	colorBlue   = ColorInfo
	colorRed    = ColorError
	colorYellow = ColorWarning
	colorGray   = ColorNeutral
)

// respondEphemeralEmbed sends an ephemeral embed interaction response.
func (b *Bot) respondEphemeralEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

// respondPublicEmbed sends a public embed interaction response.
func (b *Bot) respondPublicEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
}

// followUpEmbed sends a follow-up embed to an already-acknowledged interaction.
func (b *Bot) followUpEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
}

// deferEphemeral acknowledges an interaction without content so the handler can
// do slow work (resolving a URL) and then edit the real embed in.
func (b *Bot) deferEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

// editEmbed replaces a deferred interaction response with an embed.
func (b *Bot) editEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

// errEmbed returns a red error embed.
func errEmbed(title, desc string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "⚠️ " + title, Description: desc, Color: ColorError}
}

// infoEmbed returns a blurple info embed.
func infoEmbed(title, desc string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: title, Description: desc, Color: ColorInfo}
}

// successEmbed returns a green success embed.
func successEmbed(title, desc string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: title, Description: desc, Color: ColorMusic}
}

// warnEmbed returns a yellow warning embed.
func warnEmbed(title, desc string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: "⚠️ " + title, Description: desc, Color: ColorWarning}
}

// neutralEmbed returns a gray neutral embed.
func neutralEmbed(title, desc string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: title, Description: desc, Color: ColorNeutral}
}

// followUpEphemeralEmbed sends an ephemeral follow-up embed.
func (b *Bot) followUpEphemeralEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
		Flags:  discordgo.MessageFlagsEphemeral,
	})
}

// inlineField is a small helper for building embed fields.
func inlineField(name, value string) *discordgo.MessageEmbedField {
	if value == "" {
		value = "—"
	}
	return &discordgo.MessageEmbedField{Name: name, Value: value, Inline: true}
}

// SourceIcon returns an emoji representing the audio source.
func SourceIcon(source string) string {
	switch strings.ToLower(source) {
	case "youtube":
		return "🔴"
	case "soundcloud":
		return "🟠"
	case "spotify":
		return "🟢"
	default:
		return "🎵"
	}
}

// SourceLabel returns "Source" field text with icon prefix.
func SourceLabel(source string) string {
	icon := SourceIcon(source)
	if source == "" {
		return icon + " Unknown"
	}
	return icon + " " + source
}

// ProgressBar renders a text-based progress bar.
//
// elapsed and total are in seconds. width is the number of block characters.
// Returns something like "▶ ▓▓▓▓▓░░░░░ 02:15 / 04:32".
//
// Edge cases:
//   - total == 0 → shows "▶ 🔴 LIVE" (a live stream has no duration).
//   - elapsed > total → clamps to 100%.
//   - elapsed < 0 → treated as 0.
//   - paused == true → prefix is "⏸" instead of "▶".
func ProgressBar(elapsed, total float64, width int, paused bool) string {
	if paused {
		return "⏸ Paused"
	}

	// Live stream — no duration to measure against.
	if total <= 0 {
		return "▶ 🔴 LIVE"
	}

	if width < 1 {
		width = 1
	}

	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > total {
		elapsed = total
	}

	filled := int((elapsed / total) * float64(width))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
	indicator := "▶"

	return fmt.Sprintf("%s %s %s / %s", indicator, bar, formatBarTime(elapsed), formatBarTime(total))
}

// formatBarTime formats seconds as M:SS (e.g. 135 → "2:15").
func formatBarTime(seconds float64) string {
	total := int(seconds)
	if total < 0 {
		total = 0
	}
	m := total / 60
	s := total % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
