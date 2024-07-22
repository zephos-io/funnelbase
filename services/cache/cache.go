package cache

import (
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/request"
	"zephos/funnelbase/util"
)

var ctx = context.Background()

var (
	cachedCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "funnelbase",
			Subsystem: "cache",
			Name:      "cached_count",
			Help:      "Number of cached items",
		},
	)
	monitorInterval = 1 * time.Second
	logger          = util.NewLogger().With().Str("component", "cache").Logger()
)

type Cache struct {
	client *redis.Client
}

type CachedResponse struct {
	Body       string `redis:"body"`
	StatusCode int    `redis:"statusCode"`
}

func New() (*Cache, error) {
	client, err := InitialiseRedis()

	if err != nil {
		return nil, err
	}

	return &Cache{client: client}, nil
}

func (c *Cache) CacheResponse(resp *request.Response, cacheLifespan time.Duration) error {
	err := c.client.HSet(ctx, resp.Request.URL.String(), &CachedResponse{
		Body:       resp.Body,
		StatusCode: resp.StatusCode,
	}).Err()

	if err != nil {
		return err
	}

	return c.client.Expire(ctx, resp.Request.URL.String(), cacheLifespan).Err()
}

func (c *Cache) CheckCache(req *pb.Request) (*CachedResponse, error) {
	res := c.client.HGet(ctx, req.Url, "body")

	if errors.Is(res.Err(), redis.Nil) {
		return nil, nil
	}

	if res.Err() != nil {
		return nil, fmt.Errorf("failed to get cached response %s: %v", req.Url, res.Err())
	}

	return &CachedResponse{
		Body: res.Val(),
	}, nil
}

func (c *Cache) Monitor() {
	logger.Info().Msgf("starting prometheus monitor (interval: %s)", monitorInterval.String())

	ticker := time.NewTicker(monitorInterval)

	for range ticker.C {
		res := c.client.DBSize(ctx)

		if res.Err() != nil {
			fmt.Println("error")
		}

		cachedCount.Set(float64(res.Val()))
	}
}

func (cr *CachedResponse) ConvertResponseToGRPC() (*pb.Response, error) {
	resp := &request.Response{
		StatusCode: cr.StatusCode,
		Body:       cr.Body,
		CacheHit:   true,
	}

	return resp.ConvertResponseToGRPC()
}
