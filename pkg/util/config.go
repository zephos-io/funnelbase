package util

import (
	"flag"
	"github.com/peterbourgon/ff/v3"
	"github.com/spf13/viper"
	"log"
	"os"
)

var (
	fs = flag.NewFlagSet("funnelbase", flag.ExitOnError)
	_  = fs.Int("port", 50051, "The server port")
	_  = fs.String("app_env", "development", "The app environment")
)

func init() {
	if err := initialiseConfig(); err != nil {
		log.Fatalf("failed to initialise config: %v\n", err)
	}
}

func parseFlags() error {
	return ff.Parse(fs, os.Args[1:], ff.WithEnvVars())
}

func initialiseConfig() error {
	if err := parseFlags(); err != nil {
		return err
	}

	fs.VisitAll(func(f *flag.Flag) {
		viper.Set(f.Name, f.Value)
	})

	return nil
}
