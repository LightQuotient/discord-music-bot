// embed.go
package musicbot

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

func (bot *MusicBot) updateNowPlayingEmbed(s *discordgo.Session) {
	// Only proceed if the Now Playing embed is initialized
	if !bot.EmbedInitialized {
		log.Println("Cannot update Now Playing embed: Embed not initialized.")
		return
	}
	if bot.CurrentSongMessageID == "" || bot.CurrentSongChannelID == "" {
		log.Println("Cannot update Now Playing embed: Missing or invalid message/channel ID")
		return
	}

	if bot.CurrentSong == nil {
		log.Println("Cannot update Now Playing embed: No current song")
		return
	}

	// Lock PauseState for thread safety
	bot.PauseState.Mutex.Lock()
	elapsed := int(bot.PauseState.Pos)
	totalDuration := bot.CurrentSong.DurationSeconds
	bot.PauseState.Mutex.Unlock()

	embed := &discordgo.MessageEmbed{
		Title:       "Now Playing:",
		Description: fmt.Sprintf("🎵 **[%s](%s)**", bot.CurrentSong.Name, bot.CurrentSong.OriginalURL),
		Color:       0x00FF00,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Duration",
				Value: fmt.Sprintf("[%02d:%02d] / [%02d:%02d]",
					elapsed/60, elapsed%60,
					totalDuration/60, totalDuration%60),
				Inline: true,
			},
			{
				Name: "State",
				Value: func() string {
					if bot.PauseState.Paused {
						return "⏸ Paused"
					}
					return "▶️ Playing"
				}(),
				Inline: true,
			},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: bot.CurrentSong.Thumbnail,
		},
	}

	edit := &discordgo.MessageEdit{
		Channel: bot.CurrentSongChannelID,
		ID:      bot.CurrentSongMessageID,
		Embed:   embed,
	}

	_, err := s.ChannelMessageEditComplex(edit)
	if err != nil {
		log.Printf("Failed to update Now Playing embed: %v", err)
		return
	}

	log.Println("Now Playing embed updated successfully.")
}

// listQueue sends an embed with the current queue
func (bot *MusicBot) listQueueSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Println("listQueueSlash command called")

	bot.QueueMutex.Lock()
	defer bot.QueueMutex.Unlock()

	if len(bot.Queue) == 0 && bot.CurrentSong == nil {
		embed := &discordgo.MessageEmbed{
			Title:       "Queue is Empty!",
			Description: "Add songs to the queue with `/play <url>`.",
			Color:       0xFF0000,
		}
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})
		if err != nil {
			log.Printf("Failed to respond to slash command: %v", err)
		}
		return
	}

	var description string
	var thumbURL string

	if bot.CurrentSong != nil {
		description += fmt.Sprintf("🎵 **Now Playing**: [%s](%s)\nDuration: %s\n\n",
			bot.CurrentSong.Name, bot.CurrentSong.OriginalURL, bot.CurrentSong.Duration)
		thumbURL = bot.CurrentSong.Thumbnail // Use the thumbnail of the current song
	}

	if len(bot.Queue) > 0 {
		description += "**Up Next:**\n"
		for i, song := range bot.Queue {
			description += fmt.Sprintf("%d. [%s](%s) (%s)\n", i+1, song.Name, song.OriginalURL, song.Duration)
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Music Queue",
		Description: description,
		Color:       0x00FF00,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: thumbURL, // Use the current song thumbnail if available
		},
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})
	if err != nil {
		log.Printf("Failed to respond to slash command: %v", err)
	}
}

// handleComponentInteraction processes button clicks for pause, resume, restart, and stop
func (bot *MusicBot) handleComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Properly acknowledge the button interaction
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage, // Use a valid type for updating the message
	})
	if err != nil {
		log.Printf("Error acknowledging button interaction: %v", err)
		return
	}

	// Process the button interaction based on CustomID
	switch i.MessageComponentData().CustomID {
	case "pause_button":
		bot.pauseSlash(s, i)
	case "resume_button":
		bot.resumeSlash(s, i)
	case "restart_button":
		bot.restartSlash(s, i)
	case "stop_button":
		bot.stopSlash(s, i)
	default:
		log.Printf("Unhandled button ID: %s", i.MessageComponentData().CustomID)
	}

	// Update the "Now Playing" embed if initialized
	if bot.CurrentSongMessageID != "" && bot.CurrentSongChannelID != "" {
		bot.updateNowPlayingEmbed(s)
	} else {
		log.Println("Cannot update Now Playing embed: Embed not yet initialized.")
	}
}

// nowPlaying displays the current song with its duration and elapsed time
func (bot *MusicBot) nowPlayingSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if bot.CurrentSong == nil {
		embed := &discordgo.MessageEmbed{
			Title:       "Nothing is currently playing.",
			Description: "Add a song to the queue with `/play <url>`!",
			Color:       0xFF0000,
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})
		return
	}

	bot.PauseState.Mutex.Lock()
	elapsed := int(bot.PauseState.Pos)
	bot.PauseState.Mutex.Unlock()

	embed := &discordgo.MessageEmbed{
		Title:       "Now Playing:",
		Description: fmt.Sprintf("🎵 **[%s](%s)**", bot.CurrentSong.Name, bot.CurrentSong.OriginalURL),
		Color:       0x00FF00,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Duration",
				Value: fmt.Sprintf("[%02d:%02d] / [%02d:%02d]",
					elapsed/60, elapsed%60,
					bot.CurrentSong.DurationSeconds/60, bot.CurrentSong.DurationSeconds%60),
				Inline: true,
			},
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: bot.CurrentSong.Thumbnail,
		},
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Style: discordgo.PrimaryButton, Label: "Pause", CustomID: "pause_button"},
				discordgo.Button{Style: discordgo.SuccessButton, Label: "Resume", CustomID: "resume_button"},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Restart", CustomID: "restart_button"},
				discordgo.Button{Style: discordgo.DangerButton, Label: "Stop", CustomID: "stop_button"},
			},
		},
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		},
	})
	if err != nil {
		log.Printf("Error sending Now Playing embed: %v", err)
		return
	}

	// Retrieve message details after responding
	msg, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: " ",
	})
	if followErr != nil {
		log.Printf("Error retrieving follow-up message: %v", followErr)
		return
	}

	bot.CurrentSongMessageID = msg.ID
	bot.CurrentSongChannelID = i.ChannelID
	log.Println("Now Playing embed created successfully.")

}
