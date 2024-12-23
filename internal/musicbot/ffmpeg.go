// ffmpeg.go
package musicbot

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"
)

// playSong handles spawning FFmpeg, reading PCM, encoding to Opus, and sending it to Discord
func (bot *MusicBot) playSong(song *Song) error {
	log.Printf("Starting new song: %s", song.Name)

	// Reset start position on restart
	bot.PauseState.Mutex.Lock()
	startPos := bot.PauseState.TotalPlayTime + bot.PauseState.Pos
	if bot.PauseState.Pos == 0 {
		startPos = 0
	}
	log.Printf("Starting position for playback: %.2f seconds", startPos)
	bot.PauseState.Mutex.Unlock()

	cmdArgs := []string{
		"-ss", fmt.Sprintf("%.2f", startPos),
		"-i", song.StreamURL,
		"-ac", "2",
		"-f", "s16le",
		"-ar", "48000",
		"pipe:1",
		"-progress", "pipe:2",
	}

	cmd := exec.Command("ffmpeg", cmdArgs...)
	ffmpegOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdout pipe error: %v", err)
	}
	ffmpegErr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stderr pipe error: %v", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting ffmpeg: %v", err)
	}

	bot.PauseState.Mutex.Lock()
	bot.PauseState.Cmd = cmd
	bot.PauseState.Mutex.Unlock()

	bot.VoiceConn.Speaking(true)

	progressChan := make(chan float64)
	doneChan := make(chan error)
	go bot.parseFFmpegProgress(ffmpegErr, progressChan, doneChan)

	opusEncoder, err := newOpusEncoder()
	if err != nil {
		return fmt.Errorf("error creating opus encoder: %v", err)
	}
	defer opusEncoder.Close()

	// Periodic embed update goroutine
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			if bot.PauseState.Paused || bot.CurrentSong == nil || bot.CurrentSongMessageID == "" || bot.CurrentSongChannelID == "" {
				log.Println("Ticker skipped: Embed not yet initialized.")
				continue
			}
			bot.updateNowPlayingEmbed(bot.Session)
		}
	}()

	rawBuf := make([]byte, 960*4)
	for {
		select {
		case err := <-doneChan:
			ticker.Stop()
			if err != nil {
				log.Printf("Error parsing FFmpeg progress: %v", err)
			}
			goto cleanup

		case progress := <-progressChan:
			bot.PauseState.Mutex.Lock()
			bot.PauseState.Pos = progress
			bot.PauseState.Mutex.Unlock()

		default:
			bot.PauseState.Mutex.Lock()
			paused := bot.PauseState.Paused
			skip := bot.PauseState.SkipReq
			bot.PauseState.Mutex.Unlock()

			if paused || skip {
				break
			}

			n, err := ffmpegOut.Read(rawBuf)
			if err != nil {
				if err == io.EOF {
					break
				}
				log.Printf("Error reading ffmpeg output: %v", err)
				break
			}
			if n == 0 {
				continue
			}

			opusBuf, err := opusEncoder.Encode(rawBuf[:n])
			if err != nil {
				log.Printf("Error encoding to Opus: %v", err)
				break
			}
			bot.VoiceConn.OpusSend <- opusBuf
		}
	}

cleanup:
	ticker.Stop()
	bot.VoiceConn.Speaking(false)
	_ = cmd.Process.Kill()

	bot.PauseState.Mutex.Lock()
	bot.PauseState.Cmd = nil
	bot.PauseState.Mutex.Unlock()

	return nil
}

// parseFFmpegProgress continuously reads FFmpeg stderr to update the playback position
func (bot *MusicBot) parseFFmpegProgress(reader io.Reader, progressChan chan<- float64, done chan<- error) {
	defer close(progressChan)
	defer close(done)

	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err == io.EOF {
				done <- nil
				return
			}
			done <- err
			return
		}

		output := string(buf[:n])
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "out_time=") {
				timeStr := strings.TrimPrefix(line, "out_time=")
				parts := strings.Split(timeStr, ":")
				if len(parts) == 3 {
					var hours, minutes, seconds float64
					fmt.Sscanf(parts[0], "%f", &hours)
					fmt.Sscanf(parts[1], "%f", &minutes)
					fmt.Sscanf(parts[2], "%f", &seconds)

					progress := hours*3600 + minutes*60 + seconds
					progressChan <- progress
				}
			}
		}
	}
}
