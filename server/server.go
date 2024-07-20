package server

import (
	"context"
	"fmt"
	"log"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/rate_limiter"
	"zephos/funnelbase/redis"
	"zephos/funnelbase/request"
)

type Server struct {
	pb.UnimplementedFunnelbaseServer
	Redis       *redis.Redis
	RateLimiter *rate_limiter.RateLimiter
}

func (s *Server) QueueRequest(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	start := time.Now()

	//defer func() {
	//	duration := time.Since(start)
	//	fmt.Println(duration)
	//}()

	log.Println("RECEIVED", req.Url)

	//go func() {
	//	select {
	//	case <-ctx.Done():
	//		if ctx.Err() != nil {
	//			log.Println("context canceled")
	//		} else {
	//			fmt.Println("done")
	//
	//		}
	//
	//		return nil, nil
	//	}
	//}()

	//time.Sleep(2 * time.Second)
	//fmt.Println(time.Now(), ctx.Deadline())

	//if req.Timeout > 0 {
	//	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.Timeout)*time.Millisecond)
	//}

	if req.CacheLifespan > 0 {
		cachedResp, err := s.Redis.CheckCache(req)
		if err != nil {
			fmt.Println(err)
		}

		if cachedResp != nil {
			return cachedResp.ConvertResponseToGRPC()
		}
	}

	err := s.RateLimiter.LimitRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	duration := time.Since(start)

	log.Println("released", req.Url, duration)

	//log.Println("sleeping for ", sleepTime)
	//time.Sleep(sleepTime)

	resp, err := request.QueueRequest(req)
	if err != nil {
		return nil, err
	}

	//s.RateLimiter.IncrementUsed()
	//s.Redis.Increment(30*time.Second, "test", 1)
	//fmt.Println(s.RateLimiter.GetUsed())

	if resp.StatusCode == 200 && req.CacheLifespan > 0 {
		// cache will default to 0ms if no time is provided
		var cacheLifespan = time.Duration(req.CacheLifespan) * time.Millisecond

		err = s.Redis.CacheResponse(resp, cacheLifespan)
		if err != nil {
			return nil, err
		}
	}

	grpcResp, err := resp.ConvertResponseToGRPC()

	return grpcResp, err
}
