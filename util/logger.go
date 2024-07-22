package util

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	"github.com/spf13/viper"
	"io"
	"os"
	"time"
)

func NewLogger() zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	// defaults for non-dev environment
	// outputs as structured json
	var output io.Writer = os.Stderr
	// minimum log level is info
	var level = zerolog.InfoLevel

	if viper.GetString("app_env") == "development" {
		output = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339Nano}
		level = zerolog.DebugLevel
	}

	zerolog.SetGlobalLevel(level)

	return zerolog.New(output).With().Timestamp().Logger()
}
