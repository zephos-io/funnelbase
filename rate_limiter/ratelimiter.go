package rate_limiter

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
	"strconv"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/request"
	"zephos/funnelbase/util"
)

var (
	reqsAllowed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_allowed_total",
			Help:      "Total number of requests allowed through the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority"},
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
			Namespace:                   "funnelbase",
			Subsystem:                   "rate_limiter",
			Name:                        "reqs_wait_time_ms",
			Help:                        "Time (in milliseconds) requests are spent waiting to be allowed through the rate limiter",
			NativeHistogramBucketFactor: 1.1,
		}, []string{"rate_limiter_name", "limit_name", "queue_priority"},
	)

	reqsCancelled = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_cancelled_total",
			Help:      "Total number of requests cancelled while in the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority"},
	)
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
	//prometheus.MustRegister(reqsAllowed)

	return &RateLimiter{
		name: name,
	}
}

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

		//queue:       make(PriorityQueue),
		//queue:       make(chan *rateLimitRequest),
	}

	rule.limit = limit
	limit.rateLimiter = rl

	//heap.Init(&limit.queue)

	// 3 queues
	// 0 lowest priority
	// 1 second highest priority
	// 2 highest priority
	limit.queue = util.NewPriorityQueue(2)

	limit.rules = append(limit.rules, rule)
	rl.limits = append(rl.limits, limit)

	//go limit.queueHandler()
	return limit
}

func (rl *RateLimiter) LimitExists(name string) bool {
	for _, l := range rl.limits {
		if l.name == name {
			return true
		}
	}

	return false
}

func (rl *RateLimiter) Monitor() {
	fmt.Println("starting monitor")
	for {
		for _, l := range rl.limits {
			queues := l.queue.Lens()

			for i, q := range queues {

				labeledReqsWaiting := reqsWaiting.WithLabelValues(l.rateLimiter.name, l.name, pb.RequestPriority_name[int32(i)])

				labeledReqsWaiting.Set(float64(q))
			}
		}
	}
}

func (l *limit) StartQueueHandler() {
	r := l.rules[0]
	timeBetweenReqs := time.Duration(r.interval.Milliseconds()/int64(r.reqLimit)) * time.Millisecond

	for {
		if l.queue.Len() > 0 {
			if l.blockedUntil == nil {
				r := l.queue.RemoveWait()

				if r != nil {
					qr := r.(*queuedRequest)
					//log.Println("req received from queue", qr.request.Url)

					labeledReqsAllowed := reqsAllowed.WithLabelValues(l.rateLimiter.name, l.name, qr.request.Priority.Enum().String())
					labeledReqsWaitTime := reqsWaitTime.WithLabelValues(l.rateLimiter.name, l.name, qr.request.Priority.Enum().String())

					allowedNextReq := l.lastRequest.Add(timeBetweenReqs)

					timeUntilNextAllowed := allowedNextReq.Sub(time.Now())

					waitTime := timeUntilNextAllowed

					//fmt.Println("sleeping for ", waitTime)

					select {
					case <-qr.ctx.Done():
						//fmt.Println("stopping sleep due to context timeout")
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
				log.Printf("%s sleeping for %fs before unblocked", l.name, timeUntilUnblocked.Seconds())
				time.Sleep(timeUntilUnblocked)
				l.blockedUntil = nil
			}
		}
	}
}

func (rl *RateLimiter) GetLimitForRequest(req *pb.Request) *limit {
	for _, l := range rl.limits {
		if l.name == req.RateLimit {
			return l
		}
	}

	return nil
}

func (rl *RateLimiter) LimitRequest(ctx context.Context, req *pb.Request) error {
	if req.RateLimit == "" {
		return fmt.Errorf("rate limit is empty")
	}

	matchingLimit := rl.GetLimitForRequest(req)

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
		rlr = nil // stop memory leak

		reqsCancelled.WithLabelValues(rl.name, matchingLimit.name, req.Priority.Enum().String()).Inc()

		return ctx.Err()
	case _ = <-rlr.release:
		//log.Println(req.Url, timeSlept)
		return nil
	}
}

func (rl *RateLimiter) CheckForBackoff(req *pb.Request, resp *request.Response) error {
	matchingLimit := rl.GetLimitForRequest(req)

	if matchingLimit == nil {
		return fmt.Errorf("rate limit %s not found", req.RateLimit)
	}

	if resp.StatusCode == matchingLimit.backoffStatusCode {
		backoff := resp.Headers.Get(matchingLimit.retryAfterHeader)

		if backoff != "" {
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
