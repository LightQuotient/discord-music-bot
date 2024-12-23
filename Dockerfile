# Use the official Go image with Alpine for a lightweight build
FROM golang:1.23.4-alpine

# Install necessary dependencies
RUN apk add --no-cache bash yt-dlp ffmpeg opus-dev gcc musl-dev

# Set the working directory inside the container
WORKDIR /app

# Copy Go module files and download dependencies
COPY go.mod go.sum ./ 
RUN go mod download

# Copy the entire project into the container
COPY . .

# Change working directory to the cmd/musicbot folder where main.go resides
WORKDIR /app/cmd/musicbot

# Build the Go application
ENV CGO_ENABLED=1
RUN go build -o /app/bot .

# Expose the port (optional, for webhooks or monitoring)
EXPOSE 8080

# Command to run the bot binary
CMD ["/app/bot"]
