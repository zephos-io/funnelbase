package server

import (
	"context"
	"fmt"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/redis"
	"zephos/funnelbase/request"
)

type Server struct {
	pb.UnimplementedFunnelbaseServer
	*redis.Redis
}

func (s *Server) QueueRequest(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		fmt.Println(duration)
	}()

	if req.CacheLifespan > 0 {
		cachedResp, err := s.CheckCache(req)
		if err != nil {
			fmt.Println(err)
		}

		if cachedResp != nil {
			return cachedResp.ConvertResponseToGRPC()
		}
	}

	resp, err := request.QueueRequest(req)
	if err != nil {
		return nil, err
	}

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
