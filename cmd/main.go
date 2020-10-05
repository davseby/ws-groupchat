package main

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

func main() {
	log := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.Stamp,
	}).With().Timestamp().Logger()

	// if len(os.Args) != 1 {

	// }

}
