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
		{Name: "timeout", Description: "Timeout a member", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Member to timeout", Required: true}, {Type: discordgo.ApplicationCommandOptionInteger, Name: "minutes", Description: "Timeout duration in minutes (up to 43200)", Required: true, MinValue: floatPtr(1), MaxValue: 43200}, {Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason", Required: false}}},
		{Name: "untimeout", Description: "Remove a timeout from a member", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Member to remove timeout from", Required: true}, {Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Reason", Required: false}}},
		{Name: "slowmode", Description: "Set channel slowmode", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "seconds", Description: "Slowmode in seconds (0-21600)", Required: true, MinValue: floatPtr(0), MaxValue: 21600}}},
		{Name: "lock", Description: "Lock this channel for @everyone"},
		{Name: "unlock", Description: "Unlock this channel for @everyone"},
	}

	if b.cfg.MusicEnabled {
		musicCmds := []*discordgo.ApplicationCommand{
			{Name: "play", Description: "Play a song from YouTube, Spotify, or SoundCloud", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "query", Description: "URL or search query", Required: true}}},
			{Name: "queue", Description: "Show the music queue", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "page", Description: "Page number (default: 1)", Required: false, MinValue: floatPtr(1)}}},
			{Name: "skip", Description: "Skip the current track"},
			{Name: "stop", Description: "Stop playback and clear the queue"},
			{Name: "pause", Description: "Pause playback"},
			{Name: "resume", Description: "Resume playback"},
			{Name: "leave", Description: "Leave voice and clear the queue"},
			{Name: "nowplaying", Description: "Show the current track"},
			{Name: "volume", Description: "Set or check volume (0-200)", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "level", Description: "Volume level 0-200 (omit to view current)", Required: false, MinValue: floatPtr(0), MaxValue: 200}}},
			{Name: "loop", Description: "Set loop mode (off, track, queue)", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "mode", Description: "Loop mode: off, track, or queue", Required: true}}},
			{Name: "shuffle", Description: "Shuffle the queue"},
			{Name: "remove", Description: "Remove a track from the queue", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "position", Description: "Position in queue (1 = next up)", Required: true, MinValue: floatPtr(1)}}},
			{Name: "lyrics", Description: "Show lyrics for the current track"},
			{Name: "move", Description: "Move a track to a new position in the queue", Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionInteger, Name: "from", Description: "Current position", Required: true, MinValue: floatPtr(1)}, {Type: discordgo.ApplicationCommandOptionInteger, Name: "to", Description: "New position", Required: true, MinValue: floatPtr(1)}}},
		}
		cmds = append(cmds, musicCmds...)
	}

	// Utility / engagement commands — always available.
	cmds = append(cmds,
		&discordgo.ApplicationCommand{Name: "reactionrole", Description: "Create a reaction role message", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "title", Description: "Embed title", Required: true},
			{Type: discordgo.ApplicationCommandOptionString, Name: "description", Description: "Embed description", Required: false},
		}},
		&discordgo.ApplicationCommand{Name: "removerrole", Description: "Remove a reaction role message", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "message_id", Description: "Message ID to remove", Required: true},
		}},
		&discordgo.ApplicationCommand{Name: "starboard", Description: "Configure the starboard", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "set", Description: "Set the starboard channel and threshold", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Starboard channel", Required: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "threshold", Description: "Stars required (default 3)", Required: false, MinValue: floatPtr(1)},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "disable", Description: "Disable the starboard"},
		}},
	)

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
	b.WriteString("**ZoeyDCBot** — AI, administrator, and music assistant.\n")
	b.WriteString("Use slash commands (`/`) or text commands (`x!`). Example: `x!play despacito`.\n\n")

	b.WriteString("**🤖 AI**\n")
	b.WriteString("• Mention me, reply to me, or DM me to chat.\n")
	b.WriteString("• `/ping` — latency check  •  `/clear` — reset channel context  •  `/version` — build info\n\n")

	b.WriteString("**🛡️ Admin / Moderation** *(requires permissions or admin role)*\n")
	b.WriteString("• `/userinfo` `[user]` — user info  •  `/serverinfo` — server info\n")
	b.WriteString("• `/purge` `<amount>` — delete messages  •  `/slowmode` `<seconds>` — set slowmode\n")
	b.WriteString("• `/kick` `<user>` `[reason]`  •  `/ban` `<user>` `[reason]` `[delete_days]`\n")
	b.WriteString("• `/timeout` `<user>` `<minutes>` `[reason]`  •  `/untimeout` `<user>`\n")
	b.WriteString("• `/lock` / `/unlock` — lock/unlock channel for @everyone\n\n")

	if musicEnabled {
		b.WriteString("**🎵 Music**\n")
		b.WriteString("• `/play` `<query>` — play from YouTube, Spotify, or SoundCloud (supports playlists)\n")
		b.WriteString("• `/queue` `[page]` — view queue  •  `/nowplaying` — current track\n")
		b.WriteString("• `/skip` — skip  •  `/stop` — stop & clear  •  `/pause` / `/resume`  •  `/leave`\n")
		b.WriteString("• `/volume` `[level]` — set/view volume (0–200)\n")
		b.WriteString("• `/loop` `<off|track|queue>` — loop mode\n")
		b.WriteString("• `/shuffle` — shuffle queue  •  `/remove` `<pos>` — remove track  •  `/move` `<from>` `<to>`\n")
		b.WriteString("• `/lyrics` — lyrics for current track\n\n")
	} else {
		b.WriteString("Music commands are disabled (`MUSIC_ENABLED=false`).\n\n")
	}

	b.WriteString("**✨ Engagement**\n")
	b.WriteString("• `/reactionrole` — create a reaction role message\n")
	b.WriteString("• `/removerrole` — remove a reaction role message\n")
	b.WriteString("• `/starboard` `set|disable` — configure starboard\n\n")

	b.WriteString("**Prefix Commands** (`x!`)\n")
	b.WriteString("Most commands also work with `x!` prefix: `x!ping`, `x!play`, `x!skip`, `x!queue`, `x!nowplaying`, `x!volume`, `x!loop`, `x!shuffle`, `x!remove`, `x!lyrics`, `x!move`, `x!pause`, `x!resume`, `x!stop`, `x!leave`, `x!help`, `x!version`, `x!clear`.\n")

	return b.String()
}
