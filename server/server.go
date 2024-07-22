package server

import (
	"context"
	"fmt"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/rate_limiter"
	"zephos/funnelbase/request"
	"zephos/funnelbase/services/cache"
	"zephos/funnelbase/util"
)

var (
	logger = util.NewLogger().With().Str("component", "server").Logger()
)

type Server struct {
	pb.UnimplementedFunnelbaseServer
	Cache       *cache.Cache
	RateLimiter *rate_limiter.RateLimiter
}

// QueueRequest handles gRPC calls to request
func (s *Server) QueueRequest(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	start := time.Now()

	reqLog := logger.With().Time("recv_time", start).Str("url", req.Url).Str("method", req.Method.String()).Str("priority", req.Priority.String()).Int64("cache", req.CacheLifespan).Logger()

	reqLog.Debug().Msgf("received request")

	if err := request.ValidateRequest(req); err != nil {
		return nil, err
	}

	// if cache lifespan is given, check to see if it exists in cache first
	if req.CacheLifespan > 0 {
		cachedResp, err := s.Cache.CheckCache(req)
		if err != nil {
			fmt.Println(err)
		}

		if cachedResp != nil {
			reqLog.Debug().Msgf("replying with cached response")
			return cachedResp.ConvertResponseToGRPC()
		}
	}

	// add request to rate limiter
	err := s.RateLimiter.LimitRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	duration := time.Since(start)

	reqLog.Debug().Msgf("released from rate limiter after %s", duration)

	// make the http request
	resp, err := request.Request(req)
	if err != nil {
		return nil, err
	}

	// check to see if backoff has been requested by the API
	if err := s.RateLimiter.CheckForBackoff(req, resp); err != nil {
		return nil, err
	}

	// if valid response and client requested caching, cache the request
	if resp.StatusCode == 200 && req.CacheLifespan > 0 {
		// cache will default to 0ms if no time is provided
		var cacheLifespan = time.Duration(req.CacheLifespan) * time.Millisecond

		err := s.Cache.CacheResponse(resp, cacheLifespan)
		if err != nil {
			return nil, err
		}

		reqLog.Debug().Msgf("added to cache for %s", cacheLifespan)
	}

	reqLog.Debug().Msg("replying")
	return resp.ConvertResponseToGRPC()
}

// AddRateLimit handles gRPC calls to add a limit to the rate limiter
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
