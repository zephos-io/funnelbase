package main

import (
	"fmt"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"net"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/pkg/rate_limiter"
	"zephos/funnelbase/pkg/server"
	"zephos/funnelbase/pkg/services/cache"
	"zephos/funnelbase/pkg/services/prometheus"
	"zephos/funnelbase/pkg/util"
)

var (
	logger = util.NewLogger().With().Str("component", "main").Logger()
)

func main() {
	logger.Info().Msgf("starting funnelbase for %s environment", viper.GetString("app_env"))

	c, err := cache.New()

	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create cache")
	}

	go prometheus.ListenAndServe()

	rl := rate_limiter.New("spotify")

	go rl.Monitor()

	// have to get as string as .GetInt returns 0
	port := viper.GetString("port")

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to listen")
	}
	s := grpc.NewServer()
	pb.RegisterFunnelbaseServer(s, &server.Server{Cache: c, RateLimiter: rl})
	logger.Info().Msgf("funnelbase server listening at %v", lis.Addr())

	// register reflection service on gRPC server.
	reflection.Register(s)

	if err = s.Serve(lis); err != nil {
		logger.Fatal().Err(err).Msg("failed to serve")
	}
}
