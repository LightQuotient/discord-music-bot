// song.go

package musicbot

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// Song holds the metadata for a track
type Song struct {
	Name            string
	StreamURL       string
	Duration        string
	DurationSeconds int
	Thumbnail       string
	OriginalURL     string
}

func fetchSongInfo(url string) (*Song, error) {
	log.Printf("Starting fetchSongInfo for URL: %s", url)

	cmd := exec.Command("yt-dlp", "-f", "bestaudio", "--get-title", "--get-url", "--get-duration", "--get-thumbnail", url)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Log before executing the command
	log.Println("Running yt-dlp command...")
	err := cmd.Run()
	if err != nil {
		log.Printf("yt-dlp command failed: %v\nstderr: %s", err, stderr.String())
		return nil, fmt.Errorf("yt-dlp error: %v\n%s", err, stderr.String())
	}

	// Log raw stdout output
	log.Printf("yt-dlp stdout: %s", stdout.String())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 4 {
		log.Printf("yt-dlp returned insufficient information. Output: %s", stdout.String())
		return nil, fmt.Errorf("could not fetch all song information")
	}

	// Assign fields correctly
	title := lines[0]
	streamURL := lines[1]
	durationStr := lines[3]
	thumbnail := lines[2]

	log.Printf("Extracted Data:\n - Title: %s\n - Stream URL: %s\n - Duration: %s\n - Thumbnail: %s",
		title, streamURL, durationStr, thumbnail)

	// Validate and sanitize the thumbnail URL
	if !strings.HasPrefix(thumbnail, "http") {
		log.Printf("Invalid thumbnail URL from yt-dlp: %s", thumbnail)
		thumbnail = "https://example.com/default-thumbnail.png" // Fallback URL
	}

	// Parse duration into seconds
	durationParts := strings.Split(durationStr, ":")
	var durationSeconds int
	if len(durationParts) == 2 {
		minutes, _ := strconv.Atoi(durationParts[0])
		seconds, _ := strconv.Atoi(durationParts[1])
		durationSeconds = minutes*60 + seconds
	} else if len(durationParts) == 3 {
		hours, _ := strconv.Atoi(durationParts[0])
		minutes, _ := strconv.Atoi(durationParts[1])
		seconds, _ := strconv.Atoi(durationParts[2])
		durationSeconds = hours*3600 + minutes*60 + seconds
	} else {
		log.Printf("Unexpected duration format: %s", durationStr)
		durationSeconds = 0 // Default to 0 if parsing fails
	}

	log.Printf("Parsed Duration: %d seconds (%s)", durationSeconds, formatDuration(durationSeconds))

	return &Song{
		Name:            title,
		StreamURL:       streamURL,
		Duration:        formatDuration(durationSeconds), // Format as HH:MM:SS or MM:SS
		DurationSeconds: durationSeconds,
		Thumbnail:       thumbnail,
		OriginalURL:     url,
	}, nil
}

// Helper function to format duration in seconds into HH:MM:SS or MM:SS
func formatDuration(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%02d:%02d", minutes, secs)
}

func safeFetchSongInfo(url string) (*Song, error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in fetchSongInfo: %v", r)
		}
	}()
	return fetchSongInfo(url)
}
