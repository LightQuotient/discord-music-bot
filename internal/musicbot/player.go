package musicbot

import (
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

// handlePlayCommand joins the voice channel, fetches song info, appends to queue
func (bot *MusicBot) handlePlayCommandSlash(s *discordgo.Session, i *discordgo.InteractionCreate, url string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error acknowledging interaction: %v", err)
		return
	}

	log.Printf("handlePlayCommand called with URL: %s", url)

	// Join the user's voice channel
	log.Println("Attempting to join voice channel...")
	vc, err := bot.joinVoiceChannelSlash(s, i)
	if err != nil {
		log.Printf("Error joining voice channel: %v", err)
		_, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("Error joining voice channel: %v", err),
		})
		if followErr != nil {
			log.Printf("Error sending follow-up message: %v", followErr)
		}
		return
	}
	bot.VoiceConn = vc
	log.Println("Successfully joined voice channel.")

	// Fetch song info
	song, err := safeFetchSongInfo(url)
	if err != nil {
		log.Printf("Error fetching song info: %v", err)
		_, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("Error fetching song info: %v", err),
		})
		if followErr != nil {
			log.Printf("Error sending follow-up message: %v", followErr)
		}
		return
	}

	// Add to the queue
	bot.QueueMutex.Lock()
	bot.Queue = append(bot.Queue, song)
	bot.QueueMutex.Unlock()
	log.Printf("Added song to queue: %s", song.Name)

	_, followErr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("Added **%s** to the queue.", song.Name),
	})
	if followErr != nil {
		log.Printf("Error sending follow-up message: %v", followErr)
	}

	// Start playback if not already playing
	bot.PlaybackMutex.Lock()
	if !bot.CurrentlyPlaying {
		log.Println("Starting playback as no song is currently playing.")
		bot.CurrentlyPlaying = true
		bot.PlaybackMutex.Unlock()
		go bot.playQueue()
	} else {
		log.Println("Playback already in progress, song added to the queue.")
		bot.PlaybackMutex.Unlock()
	}
}

// joinVoiceChannel finds which voice channel the user is in and joins it
func (bot *MusicBot) joinVoiceChannelSlash(s *discordgo.Session, i *discordgo.InteractionCreate) (*discordgo.VoiceConnection, error) {
	guildID := i.GuildID
	userID := i.Member.User.ID

	// Get the user's voice state to find their current voice channel
	vs, err := s.State.VoiceState(guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("could not find user's voice state: %v", err)
	}

	// Join the user's voice channel
	vc, err := s.ChannelVoiceJoin(vs.GuildID, vs.ChannelID, false, true)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %v", err)
	}

	return vc, nil
}

// playQueue handles iterating through the queue, playing each song
func (bot *MusicBot) playQueue() {
	log.Println("playQueue called")

	for {
		bot.QueueMutex.Lock()
		// If no songs left and no current song, we're done
		if len(bot.Queue) == 0 && bot.CurrentSong == nil {
			bot.QueueMutex.Unlock()
			log.Println("Queue is empty and no song is currently playing. Stopping playback.")
			break
		}

		// If there's no current song, pop from the queue
		var song *Song
		if bot.CurrentSong == nil {
			song = bot.Queue[0]
			bot.Queue = bot.Queue[1:]
			bot.CurrentSong = song
		} else {
			song = bot.CurrentSong
		}
		bot.QueueMutex.Unlock()

		log.Printf("Playing song: %+v", song)

		// Actually play the song
		bot.PlaybackMutex.Lock()
		err := bot.playSong(song)
		bot.PlaybackMutex.Unlock()

		if err != nil {
			log.Printf("Error playing song: %v", err)
		}

		// Handle skipping or finishing
		bot.PauseState.Mutex.Lock()
		if bot.PauseState.SkipReq {
			// Reset skip state
			bot.PauseState.SkipReq = false
			bot.PauseState.Paused = false
			bot.PauseState.Pos = 0
			bot.PauseState.TotalPlayTime = 0
			bot.CurrentSong = nil
		} else if !bot.PauseState.Paused {
			// Song finished naturally, move to the next one
			bot.CurrentSong = nil
		}
		bot.PauseState.Mutex.Unlock()

		// Wait while paused
		for bot.PauseState.Paused {
			log.Println("Playback is paused. Waiting to resume...")
			time.Sleep(500 * time.Millisecond)
		}
	}

	bot.PlaybackMutex.Lock()
	bot.CurrentlyPlaying = false
	bot.PlaybackMutex.Unlock()

	log.Println("Playback finished for all songs in the queue.")
}
