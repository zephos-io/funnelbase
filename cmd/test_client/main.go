package main

import (
	"context"
	"flag"
	"fmt"
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

	rlInterface := &pb.Interface{
		Name: "spotify",
	}

	rlInterface2 := &pb.Interface{
		Name: "revenuecat",
	}

	_, err = client.AddInterface(context.Background(), rlInterface)
	_, err = client.AddInterface(context.Background(), rlInterface2)

	if err != nil {
		panic(err)
	}

	limit := &pb.RateLimit{
		Interface:         rlInterface.Name,
		Name:              "rolling_30s",
		BackoffStatusCode: 429,
		RetryAfterHeader:  "Retry-After",
		Period:            (60 * time.Second).Milliseconds(),
		Limit:             60,
	}

	limit2 := &pb.RateLimit{
		Interface:         rlInterface2.Name,
		Name:              "rolling_30s_2",
		BackoffStatusCode: 429,
		RetryAfterHeader:  "Retry-After",
		Period:            (60 * time.Second).Milliseconds(),
		Limit:             60,
	}

	_, err = client.AddRateLimit(context.Background(), limit)
	_, err = client.AddRateLimit(context.Background(), limit2)

	if err != nil {
		panic(err)
	}

	for i := 0; i < 1000; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			priority := pb.RequestPriority_LOW

			// every 5th request should be high priority
			if i%5 == 0 {
				priority = pb.RequestPriority_HIGH
			}

			resp, err := client.QueueRequest(ctx, &pb.Request{
				Url:       fmt.Sprintf("http://localhost:3333/v1/test?index=%d", i+1),
				Method:    pb.RequestMethod_GET,
				Interface: rlInterface.Name,
				RateLimit: limit.Name,
				Priority:  priority,
				Client:    "testing",
				Retries:   1,
				//CacheLifespan: (60 * time.Second).Milliseconds(),
			})
			if err != nil {
				//panic(err)
				log.Println("failed request", i+1, err)
			} else {
				log.Println("successful request", i+1, resp.Body)

			}

		}()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			priority := pb.RequestPriority_LOW

			// every 5th request should be high priority
			if i%5 == 0 {
				priority = pb.RequestPriority_HIGH
			}

			resp, err := client.QueueRequest(ctx, &pb.Request{
				Url:       fmt.Sprintf("http://localhost:3333/v1/test?index=%d", i+1),
				Method:    pb.RequestMethod_GET,
				Interface: rlInterface2.Name,
				RateLimit: limit2.Name,
				Priority:  priority,
				Client:    "testing2",
				Retries:   1,
				//CacheLifespan: (60 * time.Second).Milliseconds(),
			})
			if err != nil {
				//panic(err)
				log.Println("failed request", i+1, err)
			} else {
				log.Println("successful request", i+1, resp.Body)

			}

		}()

		//time.Sleep(time.Duration(rand.IntN(3000)) * time.Millisecond)
		time.Sleep(1 * time.Second)
	}

}
