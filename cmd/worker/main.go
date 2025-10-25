// Package main implements the background worker for async job processing.
package main

import "github.com/rs/zerolog/log"

func main() {
	log.Info().Msg("worker started")
	select {}
}
