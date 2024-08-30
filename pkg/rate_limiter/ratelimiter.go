package rate_limiter

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/pkg/request"
	"zephos/funnelbase/pkg/services/prometheus"
	"zephos/funnelbase/pkg/util"
)

var (
	monitorInterval = 1 * time.Second

	logger = util.NewLogger().With().Str("component", "rate_limiter").Logger()
)

type RateLimiter struct {
	Name   string
	limits []*Limit
}

type Limit struct {
	rateLimiter *RateLimiter
	name        string
	queue       *util.PriorityQueue
	Rules       []*Rule
	lastRequest time.Time
	// status code for backoff
	backoffStatusCode int
	// header to identify backoff amount
	retryAfterHeader string
	// time at which the Limit is allowed to let requests through
	blockedUntil *time.Time

	mutex sync.Mutex
	cond  *sync.Cond
}

type Rule struct {
	limit    *Limit
	name     string
	interval time.Duration
	//isRollingWindow bool
	ReqLimit int32
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
		Name: name,
	}
}

// AddLimit adds a limit to the rate limiter
func (rl *RateLimiter) AddLimit(name string, interval time.Duration, requestLimit int32, backoffStatusCode int, retryAfterHeader string) *Limit {
	rule := &Rule{
		name:     name,
		interval: interval,
		//isRollingWindow: true,
		ReqLimit: requestLimit,
	}

	limit := &Limit{
		name:              name,
		lastRequest:       time.UnixMilli(0),
		backoffStatusCode: backoffStatusCode,
		retryAfterHeader:  retryAfterHeader,
		blockedUntil:      nil,
	}

	limit.cond = sync.NewCond(&limit.mutex)

	rule.limit = limit
	limit.rateLimiter = rl

	//heap.Init(&limit.queue)

	// 3 queues
	// 0 lowest priority
	// 1 second highest priority
	// 2 highest priority
	limit.queue = util.NewPriorityQueue(len(pb.RequestPriority_name) - 1)

	limit.Rules = append(limit.Rules, rule)
	rl.limits = append(rl.limits, limit)

	logger.Info().Msgf("added rate limit %q to %q", name, rl.Name)

	return limit
}

// GetLimit gets a limit from the rate limiter that matches the name
func (rl *RateLimiter) GetLimit(name string) *Limit {
	for _, l := range rl.limits {
		if l.name == name {
			return l
		}
	}

	return nil
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

				labeledReqsWaiting := prometheus.ReqsWaiting.WithLabelValues(l.rateLimiter.Name, l.name, n)
				labeledReqsWaiting.Set(float64(rw))
			}
		}
	}
}

// StartQueueHandler manages the throughput of requests through the rate limiter
func (l *Limit) StartQueueHandler() {
	logger.Info().Msgf("starting queue handler for limit: %s", l.name)
	r := l.Rules[0]

	for {
		func() {
			l.cond.L.Lock()
			defer l.cond.L.Unlock()

			// wait until signal is received that something has entered the queue
			l.cond.Wait()

			// loop until nothing exists in the queue
			// this system was implemented to avoid potential request leaks caused by uneven amount of signals and requests. there exists
			// a scenario where the queue wont be cleared as it was never signalled. think this is just due to timings when the queue gets flooded
			for {
				// only peek, don't remove. still need to check if blocked below
				reqPeek := l.queue.Peek()

				if r != nil && reqPeek != nil {
					// check if there is an outstanding block
					if l.blockedUntil == nil {

						// remove (dont wait). still potential for it to be nil so check just in case
						req := l.queue.Remove()

						if req != nil {
							qr := req.(*queuedRequest)

							// timeBetweenReqs should go here as it can update in real time
							timeBetweenReqs := time.Duration(r.interval.Milliseconds()/int64(r.ReqLimit)) * time.Millisecond

							labeledReqsAllowed := prometheus.ReqsAllowed.WithLabelValues(l.rateLimiter.Name, l.name, qr.request.Priority.Enum().String(), qr.request.Client)
							labeledReqsWaitTime := prometheus.ReqsWaitTime.WithLabelValues(l.rateLimiter.Name, l.name, qr.request.Priority.Enum().String(), qr.request.Client)

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
						logger.Info().Msgf("%q limit is sleeping for %s before continuing", l.name, timeUntilUnblocked)
						time.Sleep(timeUntilUnblocked)
						l.blockedUntil = nil
					}
				} else {
					break
				}
			}
		}()
	}
}

// getLimitForRequest returns the Limit of a given request
func (rl *RateLimiter) getLimitForRequest(req *pb.Request) *Limit {
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
	matchingLimit.cond.Signal()

	select {
	case <-ctx.Done():
		matchingLimit.queue.ClearSpecific(rlr, priority)

		logger.Info().Str("url", rlr.request.Url).Str("limit", matchingLimit.name).Str("priority", rlr.request.Priority.String()).Msg("request context cancelled")

		rlr = nil // stop memory leak

		prometheus.ReqsCancelled.WithLabelValues(rl.Name, matchingLimit.name, req.Priority.Enum().String(), req.Client).Inc()

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

			prometheus.Backoffs.WithLabelValues(rl.Name, matchingLimit.name).Inc()
			// headers are always strings so convert to number
			backoffSeconds, err := strconv.Atoi(backoff)
			if err != nil {
				return err
			}

			backoffDuration := time.Duration(backoffSeconds) * time.Second
			// add a buffer to the backoff just in case there is any overlap in requests which may trigger an extension
			backoffBuffer := 2 * time.Second
			blockedUntilTime := time.Now().Add(backoffDuration + backoffBuffer)
			matchingLimit.blockedUntil = &blockedUntilTime
		}
	}

	return nil
}
