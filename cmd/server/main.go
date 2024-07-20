package main

import (
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/rate_limiter"
	"zephos/funnelbase/redis"
	"zephos/funnelbase/server"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

func main() {
	flag.Parse()

	r := redis.InitialiseClient()

	rl := rate_limiter.New("spotify", r)
	rl.AddLimit("spotify_rolling_30s", 30*time.Second, 15)

	rl.StartQueueHandlers()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterFunnelbaseServer(s, &server.Server{Redis: r, RateLimiter: rl})
	log.Printf("server listening at %v", lis.Addr())

	// Register reflection service on gRPC server.
	reflection.Register(s)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
