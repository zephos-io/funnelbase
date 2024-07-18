package main

import (
	"context"
	"flag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"time"
	pb "zephos/funnelbase/api"
)

var (
	serverAddr = flag.String("addr", "localhost:50051", "The server address in the format of host:port")
)

func main() {
	flag.Parse()
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	defer conn.Close()
	client := pb.NewFunnelbaseClient(conn)

	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := client.QueueRequest(ctx, &pb.Request{Url: "http://localhost:3333/v1/test", Method: pb.RequestMethod_GET})
		if err != nil {
			panic(err)
		}

		log.Println("successful request")

		time.Sleep(500 * time.Millisecond)
	}

}
