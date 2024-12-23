// bot.go
package musicbot

import (
	"fmt"
	"log"
	"os/exec"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// MusicBot is your main bot struct with all necessary fields
type MusicBot struct {
	Session              *discordgo.Session
	VoiceConn            *discordgo.VoiceConnection
	Queue                []*Song
	QueueMutex           sync.Mutex
	CurrentlyPlaying     bool
	EmbedInitialized     bool
	PlaybackMutex        sync.Mutex
	CurrentSong          *Song
	CurrentSongMessageID string // Add this field to store the embed message ID
	CurrentSongChannelID string // Add this field to store the channel ID
	PauseState           struct {
		Paused        bool
		Mutex         sync.Mutex
		Pos           float64
		TotalPlayTime float64
		SkipReq       bool
		Cmd           *exec.Cmd
	}
}

// NewMusicBot constructs the MusicBot and initializes values
func NewMusicBot(session *discordgo.Session) *MusicBot {
	bot := &MusicBot{
		Session: session,
		Queue:   make([]*Song, 0),
	}

	bot.PauseState.Paused = false
	bot.PauseState.Pos = 0
	bot.CurrentlyPlaying = false
	bot.CurrentSong = nil
	bot.PauseState.Cmd = nil
	bot.PauseState.SkipReq = false

	return bot
}

// handleMessages looks for bot commands (!play, !stop, etc.) and routes them
func (bot *MusicBot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Route based on interaction type
	if i.Type == discordgo.InteractionApplicationCommand {
		// Handle slash commands
		bot.handleApplicationCommand(s, i)
	} else if i.Type == discordgo.InteractionMessageComponent {
		// Handle button interactions
		bot.handleComponentInteraction(s, i)
	} else {
		log.Printf("Unhandled interaction type: %v", i.Type)
	}
}

func (bot *MusicBot) handleApplicationCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.ApplicationCommandData().Name {
	case "play":
		url := i.ApplicationCommandData().Options[0].StringValue()
		go bot.handlePlayCommandSlash(s, i, url)
	case "queue":
		bot.listQueueSlash(s, i)
	case "stop":
		bot.stopSlash(s, i)
	case "pause":
		bot.pauseSlash(s, i)
	case "resume":
		bot.resumeSlash(s, i)
	case "next":
		bot.nextSlash(s, i)
	case "nowplaying":
		bot.nowPlayingSlash(s, i)
	case "restart":
		bot.restartSlash(s, i)
	default:
		log.Printf("Unknown slash command: %v", i.ApplicationCommandData().Name)
	}
}

// Update Start to Register Interaction Handler
func (bot *MusicBot) Start() {
	log.Println("Starting Music Bot...")

	if err := bot.Session.Open(); err != nil {
		log.Fatalf("Failed to open Discord session: %v", err)
	}

	if err := bot.registerSlashCommands(bot.Session); err != nil {
		log.Fatalf("Failed to register slash commands: %v", err)
	}

	bot.Session.AddHandler(bot.handleInteraction)
	bot.Session.AddHandler(bot.handleComponentInteraction)
	log.Println("Music Bot is now running!")
}

// stop clears the queue, kills ffmpeg, and disconnects from voice
func (bot *MusicBot) stopSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Println("stopSlash command called")

	// Clear the queue
	bot.QueueMutex.Lock()
	bot.Queue = nil
	bot.QueueMutex.Unlock()

	// Kill the ffmpeg process if it's running
	bot.PauseState.Mutex.Lock()
	if bot.PauseState.Cmd != nil {
		log.Println("Stopping FFmpeg process...")
		_ = bot.PauseState.Cmd.Process.Kill()
		bot.PauseState.Cmd = nil
	}
	bot.PauseState.Mutex.Unlock()

	// Disconnect from the voice channel
	if bot.VoiceConn != nil {
		log.Println("Disconnecting from the voice channel...")
		bot.VoiceConn.Disconnect()
		bot.VoiceConn = nil
	}

	// Delete the current embed message
	if bot.CurrentSongMessageID != "" && bot.CurrentSongChannelID != "" {
		log.Println("Deleting current song embed...")
		err := s.ChannelMessageDelete(bot.CurrentSongChannelID, bot.CurrentSongMessageID)
		if err != nil {
			log.Printf("Failed to delete embed message: %v", err)
		}
		bot.CurrentSongMessageID = "" // Clear after deletion attempt
		bot.CurrentSongChannelID = ""
	}

	// Reset playback state
	bot.PlaybackMutex.Lock()
	bot.CurrentlyPlaying = false
	bot.CurrentSong = nil
	bot.PlaybackMutex.Unlock()

	// Send response to the slash command
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Playback stopped, queue cleared, and disconnected from the voice channel.",
		},
	})
	if err != nil {
		log.Printf("Error responding to stopSlash: %v", err)
	}
}

// pause toggles the paused state
func (bot *MusicBot) pauseSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	bot.PauseState.Mutex.Lock()
	defer bot.PauseState.Mutex.Unlock()

	if bot.PauseState.Paused {
		// Respond to the slash command indicating playback is already paused
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Playback is already paused.",
			},
		})
		if err != nil {
			log.Printf("Error responding to pauseSlash: %v", err)
		}
		return
	}

	// Set Paused to true
	bot.PauseState.Paused = true

	log.Println("Playback paused (audio is still being read, but not sent).")

	// Respond to the slash command indicating playback has been paused
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Playback paused.",
		},
	})
	if err != nil {
		log.Printf("Error responding to pauseSlash: %v", err)
	}
}

func (bot *MusicBot) resumeSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	bot.PauseState.Mutex.Lock()
	defer bot.PauseState.Mutex.Unlock()

	if !bot.PauseState.Paused {
		// Respond to the slash command indicating playback is not paused
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Playback is not paused.",
			},
		})
		if err != nil {
			log.Printf("Error responding to resumeSlash: %v", err)
		}
		return
	}

	// Unset the pause flag
	bot.PauseState.Paused = false

	log.Println("Playback resumed (frames will be sent again).")

	// Respond to the slash command indicating playback has been resumed
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Playback resumed.",
		},
	})
	if err != nil {
		log.Printf("Error responding to resumeSlash: %v", err)
	}
}

// next requests the skip for the current track
func (bot *MusicBot) nextSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Println("nextSlash command called")

	// Lock the PauseState to safely update it
	bot.PauseState.Mutex.Lock()
	bot.PauseState.SkipReq = true
	if bot.PauseState.Cmd != nil {
		_ = bot.PauseState.Cmd.Process.Kill()
	}
	bot.PauseState.Mutex.Unlock()

	// Respond to the slash command indicating the current track was skipped
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Skipped current track. Moving to the next...",
		},
	})
	if err != nil {
		log.Printf("Error responding to nextSlash: %v", err)
	}
}

func (bot *MusicBot) restartSlash(s *discordgo.Session, i *discordgo.InteractionCreate) {
	log.Println("RestartSlash command called")

	bot.PauseState.Mutex.Lock()
	// Stop the current FFmpeg process if it is running
	if bot.PauseState.Cmd != nil {
		log.Println("Stopping the current FFmpeg process before restarting...")
		_ = bot.PauseState.Cmd.Process.Kill()
		bot.PauseState.Cmd = nil
	}

	// Reset playback state
	bot.PauseState.Paused = false
	bot.PauseState.Pos = 0
	bot.PauseState.TotalPlayTime = 0
	bot.PauseState.SkipReq = false
	bot.PauseState.Mutex.Unlock()

	if bot.CurrentSong != nil {
		log.Printf("Restarting song: %s", bot.CurrentSong.Name)
		go func() {
			// Treat restart as a new session by re-fetching song info
			song, err := safeFetchSongInfo(bot.CurrentSong.OriginalURL)
			if err != nil {
				log.Printf("Error re-fetching song info during restart: %v", err)

				// Respond to the slash command with the error
				err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("Error restarting playback: %v", err),
					},
				})
				if err != nil {
					log.Printf("Error responding to restartSlash: %v", err)
				}
				return
			}

			bot.QueueMutex.Lock()
			bot.CurrentSong = song // Set the re-fetched song as the current song
			bot.QueueMutex.Unlock()

			err = bot.playSong(song)
			if err != nil {
				log.Printf("Error restarting playback: %v", err)

				// Respond to the slash command with the error
				err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("Error restarting playback: %v", err),
					},
				})
				if err != nil {
					log.Printf("Error responding to restartSlash: %v", err)
				}
			} else {
				// Respond to the slash command indicating success
				err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: fmt.Sprintf("Restarted song: %s", bot.CurrentSong.Name),
					},
				})
				if err != nil {
					log.Printf("Error responding to restartSlash: %v", err)
				}
			}
		}()
	} else {
		log.Println("No song is currently playing to restart.")

		// Respond to the slash command indicating no song to restart
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "No song is currently playing to restart.",
			},
		})
		if err != nil {
			log.Printf("Error responding to restartSlash: %v", err)
		}
	}
}

func (bot *MusicBot) registerSlashCommands(s *discordgo.Session) error {
	if s.State.User == nil {
		return fmt.Errorf("session user is not initialized; ensure the session is open before registering commands")
	}

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "play",
			Description: "Play a song in the voice channel",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "url",
					Description: "The URL of the song to play",
					Required:    true,
				},
			},
		},
		{
			Name:        "queue",
			Description: "Show the current music queue",
		},
		{
			Name:        "stop",
			Description: "Stop the music and clear the queue",
		},
		{
			Name:        "pause",
			Description: "Pause the current song",
		},
		{
			Name:        "resume",
			Description: "Resume the paused song",
		},
		{
			Name:        "next",
			Description: "Skip to the next song in the queue",
		},
		{
			Name:        "nowplaying",
			Description: "Show the currently playing song",
		},
		{
			Name:        "restart",
			Description: "Restart the current song",
		},
	}

	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
		if err != nil {
			return fmt.Errorf("cannot create '%s' command: %v", cmd.Name, err)
		}
	}
	return nil
}
