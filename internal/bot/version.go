package bot

import (
	"fmt"
	"runtime"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Version is the current release version of ZoeyDCBot.
const Version = "0.1.3"

// cmdVersion shows the bot's version, build info, and useful links.
func (b *Bot) cmdVersion(s *discordgo.Session, i *discordgo.InteractionCreate) {
	uptime := time.Since(b.startedAt).Round(time.Second)

	embed := &discordgo.MessageEmbed{
		Title:  "🤖 ZoeyDCBot",
		Color:  ColorInfo,
		Fields: []*discordgo.MessageEmbedField{
			inlineField("Version", Version),
			inlineField("Go", runtime.Version()),
			inlineField("Uptime", uptime.String()),
			inlineField("Latency", s.HeartbeatLatency().Round(time.Millisecond).String()),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "ZoeyDCBot • made with ❤️ by jettapindika",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	b.respondEphemeralEmbed(s, i, embed)
}

// versionText returns a compact version string for prefix-command output.
func versionText() string {
	return fmt.Sprintf("**ZoeyDCBot** v%s (Go %s)", Version, runtime.Version())
}
