package server

import (
	"context"
	"fmt"
	"log"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/rate_limiter"
	"zephos/funnelbase/request"
	"zephos/funnelbase/services/cache"
)

type Server struct {
	pb.UnimplementedFunnelbaseServer
	Cache       *cache.Cache
	RateLimiter *rate_limiter.RateLimiter
}

func (s *Server) QueueRequest(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	start := time.Now()

	//defer func() {
	//	duration := time.Since(start)
	//	fmt.Println(duration)
	//}()

	//log.Println("RECEIVED", req.Url)

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
		cachedResp, err := s.Cache.CheckCache(req)
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

	resp, err := request.QueueRequest(req)
	if err != nil {
		return nil, err
	}

	if err := s.RateLimiter.CheckForBackoff(req, resp); err != nil {
		return nil, err
	}

	if resp.StatusCode == 200 && req.CacheLifespan > 0 {
		// cache will default to 0ms if no time is provided
		var cacheLifespan = time.Duration(req.CacheLifespan) * time.Millisecond

		err = s.Cache.CacheResponse(resp, cacheLifespan)
		if err != nil {
			return nil, err
		}
	}

	return resp.ConvertResponseToGRPC()
}

func (s *Server) AddRateLimit(ctx context.Context, req *pb.RateLimit) (*pb.RateLimitResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if req.Period == 0 {
		return nil, fmt.Errorf("period is required")
	}

	if req.Limit == 0 {
		return nil, fmt.Errorf("limit is required")
	}

	if s.RateLimiter.LimitExists(req.Name) {
		return &pb.RateLimitResponse{Response: "limit already exists"}, nil
	}

	l := s.RateLimiter.AddLimit(req.Name, time.Duration(req.Period)*time.Millisecond, req.Limit, int(req.BackoffStatusCode), req.RetryAfterHeader)
	go l.StartQueueHandler()

	return &pb.RateLimitResponse{Response: "successfully created new limit"}, nil
}
