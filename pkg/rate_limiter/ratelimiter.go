package rate_limiter

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"strconv"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/pkg/request"
	"zephos/funnelbase/pkg/util"
)

var (
	reqsAllowed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_allowed_total",
			Help:      "Total number of requests allowed through the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	reqsWaiting = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_waiting",
			Help:      "Number of requests waiting to be allowed through the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority"},
	)

	reqsWaitTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_wait_time_ms",
			Help:      "Time (in milliseconds) requests are spent waiting to be allowed through the rate limiter",
			//NativeHistogramBucketFactor: 1.1,
			Buckets: []float64{0, 10, 25, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 15000, 30000, 45000, 60000, 120000},
			//Buckets: prometheus.ExponentialBucketsRange(0.0, 60000.0, 10),
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	reqsCancelled = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_cancelled_total",
			Help:      "Total number of requests cancelled while in the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	backoffs = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "backoff_total",
			Help:      "Total number of backoff's requested by APIs",
		}, []string{"rate_limiter_name", "limit_name"},
	)

	monitorInterval = 1 * time.Second

	logger = util.NewLogger().With().Str("component", "rate_limiter").Logger()
)

type RateLimiter struct {
	name   string
	limits []*limit
}

type limit struct {
	rateLimiter *RateLimiter
	name        string
	queue       *util.PriorityQueue
	rules       []rule
	lastRequest time.Time
	// status code for backoff
	backoffStatusCode int
	// header to identify backoff amount
	retryAfterHeader string
	// time at which the limit is allowed to let requests through
	blockedUntil *time.Time
}

type rule struct {
	limit    *limit
	name     string
	interval time.Duration
	//isRollingWindow bool
	reqLimit int32
}

type queuedRequest struct {
	request    *pb.Request
	release    chan time.Duration
	timeQueued time.Time
	ctx        context.Context
}

func New(name string) *RateLimiter {
	logger.Info().Msgf("creating rate limiter: %s", name)
	return &RateLimiter{
		name: name,
	}
}

// AddLimit adds a limit to the rate limiter
func (rl *RateLimiter) AddLimit(name string, interval time.Duration, requestLimit int32, backoffStatusCode int, retryAfterHeader string) *limit {
	rule := rule{
		name:     name,
		interval: interval,
		//isRollingWindow: true,
		reqLimit: requestLimit,
	}

	limit := &limit{
		name:              name,
		lastRequest:       time.UnixMilli(0),
		backoffStatusCode: backoffStatusCode,
		retryAfterHeader:  retryAfterHeader,
		blockedUntil:      nil,
	}

	rule.limit = limit
	limit.rateLimiter = rl

	//heap.Init(&limit.queue)

	// 3 queues
	// 0 lowest priority
	// 1 second highest priority
	// 2 highest priority
	limit.queue = util.NewPriorityQueue(len(pb.RequestPriority_name) - 1)

	limit.rules = append(limit.rules, rule)
	rl.limits = append(rl.limits, limit)

	logger.Info().Msgf("added rate limit %q to %q", name, rl.name)

	return limit
}

// LimitExists checks to see if the limit exists in the rate limiter
func (rl *RateLimiter) LimitExists(name string) bool {
	for _, l := range rl.limits {
		if l.name == name {
			return true
		}
	}

	return false
}

// Monitor observes the rate limiter on a timer to send to Prometheus
func (rl *RateLimiter) Monitor() {
	logger.Info().Msgf("starting rate limit monitor (interval: %s)", monitorInterval.String())

	ticker := time.NewTicker(monitorInterval)

	for range ticker.C {
		for _, l := range rl.limits {
			queues := l.queue.Lens()

			// for priorities
			for p, n := range pb.RequestPriority_name {
				// number of requests waiting in priority queue
				rw := queues[int(p)]

				labeledReqsWaiting := reqsWaiting.WithLabelValues(l.rateLimiter.name, l.name, n)
				labeledReqsWaiting.Set(float64(rw))
			}
		}
	}
}

// StartQueueHandler manages the throughput of requests through the rate limiter
func (l *limit) StartQueueHandler() {
	logger.Info().Msgf("starting queue handler for limit: %s", l.name)
	r := l.rules[0]
	timeBetweenReqs := time.Duration(r.interval.Milliseconds()/int64(r.reqLimit)) * time.Millisecond

	for {
		if l.queue.Len() > 0 {
			if l.blockedUntil == nil {
				r := l.queue.RemoveWait()

				if r != nil {
					qr := r.(*queuedRequest)

					labeledReqsAllowed := reqsAllowed.WithLabelValues(l.rateLimiter.name, l.name, qr.request.Priority.Enum().String(), qr.request.Client)
					labeledReqsWaitTime := reqsWaitTime.WithLabelValues(l.rateLimiter.name, l.name, qr.request.Priority.Enum().String(), qr.request.Client)

					allowedNextReq := l.lastRequest.Add(timeBetweenReqs)

					timeUntilNextAllowed := allowedNextReq.Sub(time.Now())

					waitTime := timeUntilNextAllowed

					select {
					case <-qr.ctx.Done():
						// in here when the context is cancelled due to timeout
					case <-time.After(waitTime):
						qr.release <- waitTime
						l.lastRequest = time.Now()

						labeledReqsAllowed.Inc()

						timeSinceQueued := time.Now().Sub(qr.timeQueued)
						labeledReqsWaitTime.Observe(float64(timeSinceQueued.Milliseconds()))
					}

				}
			} else {
				timeUntilUnblocked := l.blockedUntil.Sub(time.Now())
				logger.Info().Msgf("%q limit is sleeping for %s seconds before continuing", l.name, timeUntilUnblocked)
				time.Sleep(timeUntilUnblocked)
				l.blockedUntil = nil
			}
		}
	}
}

// getLimitForRequest returns the limit of a given request
func (rl *RateLimiter) getLimitForRequest(req *pb.Request) *limit {
	for _, l := range rl.limits {
		if l.name == req.RateLimit {
			return l
		}
	}

	return nil
}

// LimitRequest takes a given request, adds it the relevant queue then waits until it's allowed through the rate limiter
func (rl *RateLimiter) LimitRequest(ctx context.Context, req *pb.Request) error {
	if req.RateLimit == "" {
		return fmt.Errorf("rate limit is empty")
	}

	matchingLimit := rl.getLimitForRequest(req)

	if matchingLimit == nil {
		return fmt.Errorf("rate limit %s not found", req.RateLimit)
	}

	rlr := &queuedRequest{request: req, release: make(chan time.Duration, 1), ctx: ctx}

	priority := int(req.Priority.Number())

	rlr.timeQueued = time.Now()
	matchingLimit.queue.Add(rlr, priority)

	select {
	case <-ctx.Done():
		matchingLimit.queue.ClearSpecific(rlr, priority)

		logger.Info().Str("url", rlr.request.Url).Str("limit", matchingLimit.name).Str("priority", rlr.request.Priority.String()).Msg("request context cancelled")

		rlr = nil // stop memory leak

		reqsCancelled.WithLabelValues(rl.name, matchingLimit.name, req.Priority.Enum().String(), req.Client).Inc()

		return ctx.Err()
	case _ = <-rlr.release:
		// in here once the request is released from the rate limiter
		return nil
	}
}

// CheckForBackoff checks a given response to see if the API has requested a backoff
func (rl *RateLimiter) CheckForBackoff(req *pb.Request, resp *request.Response) error {
	matchingLimit := rl.getLimitForRequest(req)

	if matchingLimit == nil {
		return fmt.Errorf("rate limit %s not found", req.RateLimit)
	}

	if resp.StatusCode == matchingLimit.backoffStatusCode {
		backoff := resp.Headers.Get(matchingLimit.retryAfterHeader)

		if backoff != "" {
			logger.Info().Msgf("backoff recieved for %q limit (%s: %s)", matchingLimit.name, matchingLimit.retryAfterHeader, backoff)

			backoffs.WithLabelValues(rl.name, matchingLimit.name).Inc()
			// headers are always strings so convert to number
			backoffSeconds, err := strconv.Atoi(backoff)
			if err != nil {
				return err
			}

			blockedUntilTime := time.Now().Add(time.Duration(backoffSeconds) * time.Second)
			matchingLimit.blockedUntil = &blockedUntilTime
		}
	}

	return nil
}
