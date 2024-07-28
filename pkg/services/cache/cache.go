package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"net/url"
	"strings"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/pkg/request"
	"zephos/funnelbase/pkg/services/prometheus"
	"zephos/funnelbase/pkg/util"
)

var ctx = context.Background()

var (
	monitorInterval = 1 * time.Second
	logger          = util.NewLogger().With().Str("component", "cache").Logger()
)

type Cache struct {
	client *redis.Client
}

type CachedResponse struct {
	Url        string
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

// CacheResponse takes in a response and caches it in redis
// if the cache lifespan has been set, it will set the key in redis and apply the TTL
// if the cache lifespan has NOT been set, it will update the existing cache (only if it exists)
func (c *Cache) CacheResponse(cResp *CachedResponse, cacheLifespan time.Duration) error {
	if cacheLifespan > 0 {
		err := c.client.HSet(ctx, cResp.Url, cResp).Err()

		if err != nil {
			return err
		}

		return c.client.Expire(ctx, cResp.Url, cacheLifespan).Err()
	} else {
		// lua script to update the key only if it exists
		var updateCache = redis.NewScript(`
			if redis.call("exists", KEYS[1]) == 1 then
				redis.call("hset", KEYS[1], unpack(ARGV))
			else
				return 0
			end

			return 1
		`)

		_, err := updateCache.Run(ctx, c.client, []string{cResp.Url}, cResp).Result()

		return err
	}
}

func (c *Cache) GetCachedResponse(req *pb.Request) (*CachedResponse, error) {
	res := c.client.HGet(ctx, req.Url, "body")

	if errors.Is(res.Err(), redis.Nil) {
		return nil, nil
	}

	if res.Err() != nil {
		return nil, fmt.Errorf("failed to get cached response %s: %v", req.Url, res.Err())
	}

	return &CachedResponse{
		Url:  req.Url,
		Body: res.Val(),
	}, nil
}

type BatchItem struct {
	Url        string
	CachedBody interface{}
}

// BatchGetCache turns a gRPC request url returns two things
// batchCachedItems contains a list of BatchItem that has cache information about the batch items
// batchReqItems contains a list of ids that don't exist in the cache and should be kept for the request
func (c *Cache) BatchGetCache(req *pb.Request, batchCachedItems map[string]*BatchItem) []string {
	var batchReqItems []string

	// already been validated at this point
	batchUrl, _ := url.Parse(req.Url)

	// split the query param that identifies the list of ids
	itemIds := strings.Split(batchUrl.Query().Get(req.BatchParamId), ",")

	for _, id := range itemIds {
		// build the equivalent singular api request for caching purposes
		batchItemUrl := strings.ReplaceAll(req.BatchItemUrl, "{batchItemId}", id)

		cachedResp, err := c.GetCachedResponse(&pb.Request{
			Url: batchItemUrl,
		})

		if err != nil {
			logger.Warn().Err(err).Msg("failed to get batch item cache")
		}

		if cachedResp != nil {
			var jsonCachedRespBody interface{}

			// convert the string body into an interface
			err := json.Unmarshal([]byte(cachedResp.Body), &jsonCachedRespBody)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to unmarshall batched item cache")
				batchReqItems = append(batchReqItems, id)
			} else {
				// add item to batch cached item
				batchCachedItems[id] = &BatchItem{
					Url:        batchItemUrl,
					CachedBody: jsonCachedRespBody,
				}
			}
		} else {
			// add to list of request items if it doesn't exist in cache
			batchReqItems = append(batchReqItems, id)
		}
	}

	return batchReqItems
}

func (c *Cache) Monitor() {
	logger.Info().Msgf("starting prometheus monitor (interval: %s)", monitorInterval.String())

	ticker := time.NewTicker(monitorInterval)

	for range ticker.C {
		res := c.client.DBSize(ctx)

		if res.Err() != nil {
			fmt.Println("error")
		}

		prometheus.CachedCount.Set(float64(res.Val()))
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
