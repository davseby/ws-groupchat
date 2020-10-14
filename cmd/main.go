package main

import (
	"net/http"
	"os"
	"time"
	chatroom "ws-chatroom"

	"github.com/rs/zerolog"
)

func main() {
	log := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.Stamp,
	}).With().Timestamp().Logger()

	s := chatroom.NewServer(log)

	log.Info().Msg("server started on port 8080")

	err := http.ListenAndServe(":8080", s)
	if err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server terminated")
	}
}
