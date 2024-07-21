package main

import (
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/rate_limiter"
	"zephos/funnelbase/server"
	"zephos/funnelbase/services/cache"
	"zephos/funnelbase/services/prometheus"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

func main() {
	flag.Parse()

	c := cache.InitialiseClient()

	go c.Monitor()

	go prometheus.ListenAndServe()
	//prometheus.Record()

	rl := rate_limiter.New("spotify")
	//rl.AddLimit("rolling_30s", 30*time.Second, 15)

	go rl.Monitor()

	//rl.StartQueueHandlers()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterFunnelbaseServer(s, &server.Server{Cache: c, RateLimiter: rl})
	log.Printf("server listening at %v", lis.Addr())

	// Register reflection service on gRPC server.
	reflection.Register(s)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
