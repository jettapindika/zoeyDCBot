package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
)

// registerCommands installs the bot's slash commands. If COMMAND_GUILD_ID is
// configured, commands are guild-scoped for fast iteration; otherwise global.
func (b *Bot) registerCommands() error {
	log := logging.Component("commands")
	cmds := b.commandDefinitions()
	appID := b.sess.State.User.ID
	guildID := b.cfg.CommandGuildID

	existing, err := b.sess.ApplicationCommands(appID, guildID)
	if err != nil {
		return fmt.Errorf("list commands: %w", err)
	}
	byName := make(map[string]*discordgo.ApplicationCommand, len(existing))
	for _, c := range existing {
		byName[c.Name] = c
	}
	wanted := make(map[string]struct{}, len(cmds))
	for _, c := range cmds {
		wanted[c.Name] = struct{}{}
		if old := byName[c.Name]; old != nil {
			if _, err := b.sess.ApplicationCommandEdit(appID, guildID, old.ID, c); err != nil {
				return fmt.Errorf("edit /%s: %w", c.Name, err)
			}
			continue
		}
		if _, err := b.sess.ApplicationCommandCreate(appID, guildID, c); err != nil {
			return fmt.Errorf("create /%s: %w", c.Name, err)
		}
	}
	for _, old := range existing {
		if _, ok := wanted[old.Name]; !ok {
			if err := b.sess.ApplicationCommandDelete(appID, guildID, old.ID); err != nil {
				log.Warn("delete stale command failed", "command", old.Name, "err", err)
			}
		}
	}
	scope := "global"
	if guildID != "" {
		scope = "guild:" + guildID
	}
	log.Info("slash commands synced", "count", len(cmds), "scope", scope)
	return nil
}

func (b *Bot) commandDefinitions() []*discordgo.ApplicationCommand {
	cmds := []*discordgo.ApplicationCommand{
		{Name: "ping", Description: "Bot latency check"},
		{Name: "clear", Description: "Reset this channel's AI conversation context"},
		{Name: "help", Description: "Show ZoeyDCBot commands"},
		{Name: "version", Description: "Show bot version and build info"},
		{Name: "userinfo", Description: "Show information about a user", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "User to inspect", Required: false}}},
		{Name: "serverinfo", Description: "Show information about this server"},
		{Name: "purge", Description: "Delete recent messages from this channel", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "amount", Description: "Number of messages to delete (1-100)", Required: true, MinValue: floatPtr(1), MaxValue: 100}}},
		{Name: "kick", Description: "Kick a member", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Member to kick", Required: true}, {Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason", Required: false}}},
		{Name: "ban", Description: "Ban a member", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Member to ban", Required: true}, {Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason", Required: false}, {Type: discordgo.ApplicationCommandOptionInteger, Name: "delete_days", Description: "Delete message history days (0-7)", Required: false, MinValue: floatPtr(0), MaxValue: 7}}},
		{Name: "timeout", Description: "Timeout a member", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Member to timeout", Required: true}, {Type: discordgo.ApplicationCommandOptionInteger, Name: "minutes", Description: "Timeout length in minutes (1-40320)", Required: true, MinValue: floatPtr(1), MaxValue: 40320}, {Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason", Required: false}}},
		{Name: "untimeout", Description: "Remove timeout from a member", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Member to untimeout", Required: true}, {Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason", Required: false}}},
		{Name: "slowmode", Description: "Set channel slowmode", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "seconds", Description: "Slowmode seconds (0-21600)", Required: true, MinValue: floatPtr(0), MaxValue: 21600}}},
		{Name: "lock", Description: "Lock this channel for @everyone"},
		{Name: "unlock", Description: "Unlock this channel for @everyone"},
	}
	if b.cfg.MusicEnabled {
		cmds = append(cmds,
			&discordgo.ApplicationCommand{Name: "play", Description: "Queue a song/query and join your voice channel", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "query", Description: "URL or search query (YouTube, Spotify, SoundCloud)", Required: true}}},
			&discordgo.ApplicationCommand{Name: "queue", Description: "Show the music queue"},
			&discordgo.ApplicationCommand{Name: "skip", Description: "Skip the current track"},
			&discordgo.ApplicationCommand{Name: "stop", Description: "Stop playback and clear the queue"},
			&discordgo.ApplicationCommand{Name: "pause", Description: "Mark playback paused"},
			&discordgo.ApplicationCommand{Name: "resume", Description: "Resume playback"},
			&discordgo.ApplicationCommand{Name: "leave", Description: "Leave voice and clear the queue"},
			&discordgo.ApplicationCommand{Name: "nowplaying", Description: "Show current track"},
			&discordgo.ApplicationCommand{Name: "volume", Description: "Set playback volume", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "level", Description: "Volume level (0-200, default 100)", Required: true, MinValue: floatPtr(0), MaxValue: 200}}},
		)
	}
	return cmds
}

func (b *Bot) cmdPing(s *discordgo.Session, i *discordgo.InteractionCreate) {
	start := time.Now()
	b.respondEphemeralEmbed(s, i, infoEmbed("🏓 Ping", "Measuring latency…"))
	lat := time.Since(start)
	embed := infoEmbed("🏓 Pong", fmt.Sprintf("Latency: **%s** (Discord REST RTT)", lat.Round(time.Microsecond).String()))
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Embeds: &[]*discordgo.MessageEmbed{embed}})
}

func (b *Bot) cmdClear(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	if channelID == "" {
		channelID = i.Interaction.ChannelID
	}
	n := b.history.Clear(channelID)
	b.respondEphemeralEmbed(s, i, successEmbed("🧹 Context Reset", fmt.Sprintf("Cleared %d conversation turns from this channel.", n)))
}

func (b *Bot) cmdHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.respondEphemeralEmbed(s, i, infoEmbed("📖 ZoeyDCBot Help", helpText(b.cfg.MusicEnabled)))
}

func (b *Bot) logMod(action, channelID, actorID, targetID, reason string) {
	if b.cfg.ModLogChannel == "" {
		return
	}
	msg := fmt.Sprintf("**%s** by <@%s>", action, actorID)
	if targetID != "" {
		msg += fmt.Sprintf(" on <@%s>", targetID)
	}
	if reason != "" {
		msg += "\nReason: " + reason
	}
	if channelID != "" {
		msg += "\nChannel: <#" + channelID + ">"
	}
	_, _ = b.sess.ChannelMessageSend(b.cfg.ModLogChannel, msg)
}

func floatPtr(f float64) *float64 { return &f }

func helpText(musicEnabled bool) string {
	var b strings.Builder
	b.WriteString("**ZoeyDCBot** — AI, administrator, and music assistant.\n\n")
	b.WriteString("**AI**\n• Mention me, reply to me, or DM me to chat.\n• /clear resets channel context. /ping checks latency. /version shows build info.\n\n")
	b.WriteString("**Admin**\n• /userinfo, /serverinfo\n• /purge, /kick, /ban, /timeout, /untimeout, /slowmode, /lock, /unlock\n\n")
	if musicEnabled {
		b.WriteString("**Music**\n• /play <query> — play from YouTube, Spotify, or SoundCloud (supports playlists!)\n• /queue, /nowplaying, /skip, /stop, /pause, /resume, /leave, /volume\n")
	} else {
		b.WriteString("Music commands are disabled by MUSIC_ENABLED=false.\n")
	}
	return b.String()
}
