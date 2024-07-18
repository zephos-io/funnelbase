package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/request"
)

var ctx = context.Background()

type CachedResponse struct {
	Body       string `redis:"body"`
	StatusCode int    `redis:"statusCode"`
}

func (r *Redis) CacheResponse(resp *request.Response, cacheLifespan time.Duration) error {
	err := r.HSet(ctx, resp.Request.URL.String(), &CachedResponse{
		Body:       resp.Body,
		StatusCode: resp.StatusCode,
	}).Err()

	if err != nil {
		return err
	}

	return r.Expire(ctx, resp.Request.URL.String(), cacheLifespan).Err()
}

func (r *Redis) CheckCache(req *pb.Request) (*CachedResponse, error) {
	res := r.HGet(ctx, req.Url, "body")

	if errors.Is(res.Err(), redis.Nil) {
		return nil, nil
	}

	if res.Err() != nil {
		return nil, fmt.Errorf("failed to get cached response %s: %v", req.Url, res.Err())
	}

	//var cachedResponse CachedResponse
	//if err := res.Scan(&cachedResponse); err != nil {
	//	return nil, fmt.Errorf("failed to scan to cached response for %s: %v", req.Url, err)
	//}

	return &CachedResponse{
		Body: res.Val(),
	}, nil
}

func (cr *CachedResponse) ConvertResponseToGRPC() (*pb.Response, error) {
	resp := &request.Response{
		StatusCode: cr.StatusCode,
		Body:       cr.Body,
		CacheHit:   true,
	}

	return resp.ConvertResponseToGRPC()
}
