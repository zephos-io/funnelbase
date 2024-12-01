package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/pkg/rate_limiter"
	"zephos/funnelbase/pkg/request"
	"zephos/funnelbase/pkg/services/cache"
	"zephos/funnelbase/pkg/services/prometheus"
	"zephos/funnelbase/pkg/util"
)

var (
	logger = util.NewLogger().With().Str("component", "server").Logger()
)

type Server struct {
	pb.UnimplementedFunnelbaseServer
	Cache        *cache.Cache
	RateLimiters map[string]*rate_limiter.RateLimiter
}

// QueueRequest handles gRPC calls to request
func (s *Server) QueueRequest(ctx context.Context, req *pb.Request) (resp *pb.Response, err error) {
	ctx = context.WithValue(ctx, "retry_count", 0)

	rl, ok := s.RateLimiters[req.Interface]

	if !ok {
		return nil, fmt.Errorf("interface %q not found", req.Interface)
	}

	prometheus.ReqsReceived.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Inc()

	start := time.Now()

	reqLog := logger.With().Time("recv_time", start).Str("url", req.Url).Str("method", req.Method.String()).Str("priority", req.Priority.String()).Str("client", req.Client).Int32("cache", req.CacheLifespan).Logger()

	reqLog.Debug().Msgf("received request")

	defer func() {
		if err != nil {
			reqLog.Warn().Err(err).Msgf("replying after %s due to error", time.Since(start))
			prometheus.ErrorResponses.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Inc()
		} else {
			reqLog.Debug().Msgf("replying after %s", time.Since(start))
			prometheus.SuccessResponses.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Inc()
		}

	}()

	if err := request.ValidateRequest(req); err != nil {
		return nil, err
	}

	// if cache lifespan is given, check to see if it exists in cache first (for non batch requests)
	if req.CacheLifespan > 0 && req.Method == pb.RequestMethod_GET && req.BatchItemUrl == "" {
		cachedResp, err := s.Cache.GetCachedResponse(req)
		if err != nil {
			reqLog.Warn().Err(err).Msg("failed to get cache of request")
		}

		if cachedResp != nil {
			reqLog.Debug().Msgf("replying with cached response")
			prometheus.CachedResponses.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Inc()
			return cachedResp.ConvertResponseToGRPC()
		}

		// will continue if no cache exists
	}

	// if cache lifespan is given, check to see if any of the batch requested items exist in cache
	batchCachedItems := make(map[string]*cache.BatchItem)
	if req.CacheLifespan > 0 && req.BatchItemUrl != "" {
		batchReqItems := s.Cache.BatchGetCache(req, batchCachedItems)

		prometheus.BatchItems.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Add(float64(len(batchCachedItems) + len(batchReqItems)))
		prometheus.BatchItemsCached.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Add(float64(len(batchCachedItems)))

		reqLog.Debug().Msgf("improved batch request from %d items to %d", len(batchCachedItems)+len(batchReqItems), len(batchReqItems))

		// if all items from the batch can be found in the cache
		if len(batchReqItems) == 0 {
			batchCachedResponse := make(map[string][]interface {
			})

			for _, cachedItem := range batchCachedItems {
				batchCachedResponse[req.BatchArrayId] = append(batchCachedResponse[req.BatchArrayId], cachedItem.CachedBody)
			}

			batchResponseStr, err := json.Marshal(batchCachedResponse)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to marshall batch cached response")
			} else {
				batchResponse := &cache.CachedResponse{
					Url:  req.Url,
					Body: string(batchResponseStr),
				}

				prometheus.CachedResponses.WithLabelValues(rl.Name, req.RateLimit, req.Priority.String(), req.Client).Inc()
				return batchResponse.ConvertResponseToGRPC()
			}
		} else {
			// rebuild the batch url params to exclude the items that exist in cache

			// already been validated at this point
			batchUrl, _ := url.Parse(req.Url)

			values := batchUrl.Query()
			values.Set(req.BatchParamId, strings.Join(batchReqItems, ","))

			batchUrl.RawQuery = values.Encode()

			req.Url = batchUrl.String()
		}
	}

	var httpErr error

	for {
		reqLog = reqLog.With().Int("retry_count", ctx.Value("retry_count").(int)).Logger()

		if ctx.Value("retry_count").(int) <= int(req.Retries) || int(req.Retries) <= 0 {
			// add request to rate limiter
			err := rl.LimitRequest(ctx, req)
			if err != nil {
				return nil, err
			}

			duration := time.Since(start)

			reqLog.Debug().Msgf("released from rate limiter after %s", duration)

			// make the http request
			resp, err := request.Request(ctx, req)
			if err != nil {
				return nil, err
			}

			// check to see if backoff has been requested by the API
			if err := rl.CheckForBackoff(req, resp); err != nil {
				return nil, err
			}

			// check for error status code and retry
			if resp.StatusCode >= 500 {
				ctx = context.WithValue(ctx, "retry_count", ctx.Value("retry_count").(int)+1)

				httpErr = fmt.Errorf("error code %d recieved", resp.StatusCode)

				if req.Retries <= 0 {
					return nil, httpErr
				}

				continue
			}

			// if valid response, attempt to cache the response
			if resp.StatusCode == 200 && req.Method == pb.RequestMethod_GET {
				// defaults to 0 if not provided
				var cacheLifespan = time.Duration(req.CacheLifespan) * time.Millisecond

				// caching for batch request
				if req.BatchItemUrl != "" {
					var batchJsonMap map[string][]map[string]interface {
					}

					// unmarshall response body out to jsonMap
					err := json.Unmarshal([]byte(resp.Body), &batchJsonMap)
					if err != nil {
						reqLog.Warn().Err(err).Msg("failed to unmarshall batch response body")
						return resp.ConvertResponseToGRPC()
					}

					batchJsonItemsArray, ok := batchJsonMap[req.BatchArrayId]
					if !ok {
						reqLog.Warn().Msgf("%q doesnt exist in batch", req.BatchArrayId)
						return nil, fmt.Errorf("%q doesnt exist in batch", req.BatchArrayId)
					}

					// cache the batch items
					for _, jsonItem := range batchJsonItemsArray {
						jsonItemId, ok := jsonItem[req.BatchItemId]

						if ok {
							batchItemUrl := strings.ReplaceAll(req.BatchItemUrl, "{batchItemId}", jsonItemId.(string))

							itemStr, err := json.Marshal(jsonItem)

							if err != nil {
								reqLog.Warn().Err(err).Msg("failed to convert item json back to string")
								continue
							}

							err = s.Cache.CacheResponse(&cache.CachedResponse{
								Url:        batchItemUrl,
								Body:       string(itemStr),
								StatusCode: resp.StatusCode,
							}, cacheLifespan)

							if err != nil {
								reqLog.Warn().Err(err).Msg("failed to cache batch item response")
								continue
							}

						} else {
							reqLog.Warn().Msgf("%q doesnt exist in batch array", req.BatchItemId)
						}
					}

					// merge the requested batch items with the cached items
					for _, cachedItem := range batchCachedItems {
						batchJsonItemsArray = append(batchJsonItemsArray, cachedItem.CachedBody.(map[string]interface{}))
					}

					batchJsonMap[req.BatchArrayId] = batchJsonItemsArray

					batchResponseStr, err := json.Marshal(batchJsonMap)
					if err != nil {
						reqLog.Warn().Err(err).Msg("failed to unmarshall merged batch cached body")
						return nil, err
					} else {
						resp.Body = string(batchResponseStr)

						return resp.ConvertResponseToGRPC()
					}
				} else {
					// caching for non batch request
					err := s.Cache.CacheResponse(&cache.CachedResponse{
						Url:        resp.Request.URL.String(),
						Body:       resp.Body,
						StatusCode: resp.StatusCode,
					}, cacheLifespan)

					if err != nil {
						reqLog.Warn().Err(err).Msg("failed to cache response")
					}

					//reqLog.Debug().Msgf("added to cache for %s", cacheLifespan)
				}
			}

			return resp.ConvertResponseToGRPC()
		} else {
			return nil, fmt.Errorf("too many retries (%d/%d): %v", ctx.Value("retry_count").(int)-1, req.Retries, httpErr)
		}
	}
}

// AddInterface handles gRPC calls to add an interface (rate limiter) i.e. spotify, etc.
func (s *Server) AddInterface(ctx context.Context, req *pb.Interface) (*pb.InterfaceResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	_, ok := s.RateLimiters[req.Name]

	if ok {
		return &pb.InterfaceResponse{Response: "interface already exists"}, nil
	}

	rl := rate_limiter.New(req.Name)

	s.RateLimiters[req.Name] = rl

	go rl.Monitor()

	return &pb.InterfaceResponse{Response: "successfully created new interface"}, nil
}

// AddRateLimit handles gRPC calls to add a limit to the rate limiter
func (s *Server) AddRateLimit(ctx context.Context, req *pb.RateLimit) (*pb.RateLimitResponse, error) {
	if req.Interface == "" {
		return nil, fmt.Errorf("interface is required")
	}

	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if req.Period == 0 {
		return nil, fmt.Errorf("period is required")
	}

	if req.Limit == 0 {
		return nil, fmt.Errorf("limit is required")
	}

	rl, ok := s.RateLimiters[req.Interface]

	if !ok {
		return nil, fmt.Errorf("interface %q not found", req.Interface)
	}

	limit := rl.GetLimit(req.Name)
	if limit != nil {
		if limit.Rules[0].ReqLimit != req.Limit {
			logger.Info().Msgf("updating %q limit from %d to %d", req.Name, limit.Rules[0].ReqLimit, req.Limit)
			limit.Rules[0].ReqLimit = req.Limit
			return &pb.RateLimitResponse{Response: "updated limit"}, nil
		}

		return &pb.RateLimitResponse{Response: "limit already exists"}, nil
	}

	l := rl.AddLimit(req.Name, time.Duration(req.Period)*time.Millisecond, req.Limit, int(req.BackoffStatusCode), req.RetryAfterHeader)
	go l.StartQueueHandler()

	return &pb.RateLimitResponse{Response: "successfully created new limit"}, nil
}
