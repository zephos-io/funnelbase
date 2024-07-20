package rate_limiter

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
	"strings"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/util"
)

var (
	reqsAllowed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rate_limiter_requests_allowed",
			Help: "The total number of requests allowed through the rate limiter",
		}, []string{"rate_limiter_name", "limit_name"},
	)
)

type RateLimiter struct {
	name   string
	limits []*limit
}

type limit struct {
	rateLimiter *RateLimiter
	name        string
	lastRequest time.Time
	//queue       chan *rateLimitRequest
	queue *util.PriorityQueue
	rules []rule
}

type rule struct {
	limit           *limit
	name            string
	interval        time.Duration
	isRollingWindow bool
	reqLimit        int32
	used            int32
	speed           float32
}

type queuedRequest struct {
	request *pb.Request
	release chan time.Duration
	ctx     context.Context
}

func New(name string) *RateLimiter {
	//prometheus.MustRegister(reqsAllowed)

	return &RateLimiter{
		name: name,
	}
}

func (rl *RateLimiter) AddLimit(name string, interval time.Duration, requestLimit int32) {
	rule := rule{
		name:            name,
		interval:        interval,
		isRollingWindow: true,
		reqLimit:        requestLimit,
		used:            0,
		speed:           1.0,
	}

	limit := &limit{
		name:        name,
		lastRequest: time.UnixMilli(0),
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
}

func (rl *RateLimiter) StartQueueHandlers() {
	for _, l := range rl.limits {
		// reassignment required due to this:
		// https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		go l.queueHandler()
	}
}

func (l *limit) queueHandler() {
	r := l.rules[0]
	timeBetweenReqs := time.Duration(r.interval.Milliseconds()/int64(r.reqLimit)) * time.Millisecond

	labeledReqsAllowed := reqsAllowed.WithLabelValues(l.rateLimiter.name, l.name)

	for {
		if l.queue.Len() > 0 {
			r := l.queue.RemoveWait()

			if r != nil {
				qr := r.(*queuedRequest)
				log.Println("req received from queue", qr.request.Url)

				allowedNextReq := l.lastRequest.Add(timeBetweenReqs)

				timeUntilNextAllowed := allowedNextReq.Sub(time.Now())

				waitTime := timeUntilNextAllowed

				fmt.Println("sleeping for ", waitTime)

				select {
				case <-qr.ctx.Done():
					fmt.Println("stopping sleep due to context timeout")
				case <-time.After(waitTime):
					qr.release <- waitTime
					l.lastRequest = time.Now()
					//reqsAllowed.
					//	//reqsAllowed.Inc()
					labeledReqsAllowed.Inc()
				}

			}
		}
	}
}

var count = 0

func (rl *RateLimiter) LimitRequest(ctx context.Context, req *pb.Request) error {
	if req.RateLimit == "" {
		return fmt.Errorf("rate limit is empty")
	}

	limitExists := false
	var matchingLimit *limit
	for _, l := range rl.limits {
		if l.name == req.RateLimit {
			limitExists = true
			matchingLimit = l
		}
	}

	if !limitExists || matchingLimit == nil {
		return fmt.Errorf("rate limit %s not found", req.RateLimit)
	}

	rlr := &queuedRequest{request: req, release: make(chan time.Duration, 1), ctx: ctx}

	count++
	has := strings.HasSuffix(req.Url, "index=20")

	if has {
		fmt.Printf("")
	}

	priority := int(req.Priority.Number())

	matchingLimit.queue.Add(rlr, priority)

	fmt.Println("added", matchingLimit.queue.Len())

	select {
	case <-ctx.Done():
		matchingLimit.queue.ClearSpecific(rlr, priority)
		rlr = nil // stop memory leak
		fmt.Println("exiting", matchingLimit.queue.Len())
		return ctx.Err()
	case _ = <-rlr.release:
		//log.Println(req.Url, timeSlept)
		return nil
	}
}

func (rl *RateLimiter) GetUsed() int32 {
	return rl.limits[0].rules[0].used
}
