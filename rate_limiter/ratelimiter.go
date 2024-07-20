package rate_limiter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	pb "zephos/funnelbase/api"
	"zephos/funnelbase/redis"
	"zephos/funnelbase/util"
)

type RateLimiter struct {
	name   string
	redis  *redis.Redis
	limits []*limit
}

type limit struct {
	name        string
	lastRequest time.Time
	//queue       chan *rateLimitRequest
	queue *util.PriorityQueue
	rules []rule
}

type rule struct {
	name            string
	interval        time.Duration
	isRollingWindow bool
	limit           int32
	used            int32
	speed           float32
}

type queuedRequest struct {
	request *pb.Request
	release chan time.Duration
	ctx     context.Context
}

func New(name string, r *redis.Redis) *RateLimiter {
	return &RateLimiter{
		name:  name,
		redis: r,
	}
}

func (rl *RateLimiter) AddLimit(name string, interval time.Duration, requestLimit int32) {
	rule := rule{
		name:            name,
		interval:        interval,
		isRollingWindow: true,
		limit:           requestLimit,
		used:            0,
		speed:           1.0,
	}

	limit := &limit{
		name:        name,
		lastRequest: time.UnixMilli(0),
		//queue:       make(PriorityQueue),
		//queue:       make(chan *rateLimitRequest),
	}

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
	//for req := range l.queue {
	//	//log.Println("req received from queue", req)
	//	waitTime := 2 * time.Second
	//	time.Sleep(waitTime)
	//	req.release <- waitTime
	//}
	//for {
	//	//for l.queue.Len() > 0 {
	//	//	req := heap.Pop(&l.queue).(*queuedRequest)
	//	//	//fmt.Printf("%.2d\n", item.priority)
	//	//	//log.Println("req received from queue", req.priority)
	//	//	waitTime := 2 * time.Second
	//	//	time.Sleep(waitTime)
	//	//	req.release <- waitTime
	//	//}
	//}

	//for qr := l.queue.RemoveWait(); qr != nil; qr = l.queue.RemoveWait() {
	//	qr := qr.(*queuedRequest)
	//	log.Println("req received from queue", qr.request.Url)
	//	waitTime := 2 * time.Second
	//	time.Sleep(waitTime)
	//	qr.release <- waitTime
	//}

	for {
		if l.queue.Len() > 0 {
			r := l.queue.RemoveWait()

			if r != nil {
				qr := r.(*queuedRequest)
				log.Println("req received from queue", qr.request.Url)

				waitTime := 5 * time.Second

				select {
				case <-qr.ctx.Done():
					fmt.Println("stopping sleep due to context timeout")
				case <-time.After(waitTime):
					qr.release <- waitTime

				}

			}
		}
	}
}

//func (rl *RateLimiter) IncrementUsed() {
//	foundRule := rl.limits[0].rules[0]
//	window := foundRule.interval
//
//	_, _ = rl.redis.Decrement(window, foundRule.name, "pending", 1)
//	_, _ = rl.redis.Increment(window, foundRule.name, "used", 1)
//	_, _ = rl.redis.SetLastRequestTime(window, foundRule.name)
//}

//func (rl *RateLimiter) LimitRequest() time.Duration {
//	foundRule := rl.limits[0].rules[0]
//	window := foundRule.interval
//
//	currentPendingCount, _ := rl.redis.Increment(window, foundRule.name, "pending", 1)
//	//currentUsedCountStr, _ := rl.redis.Get(window, foundRule.name, "used")
//	//currentUsedCount, _ := strconv.Atoi(currentUsedCountStr)
//	lastRequestTimeStr, _ := rl.redis.Get(window, foundRule.name, "last_request_time")
//
//	lastRequestTime, _ := time.Parse(time.RFC3339, lastRequestTimeStr)
//
//	//now := time.Now()
//	//timeUntilNextWindow := now.Truncate(window).Add(window).UnixMilli() - now.UnixMilli()
//	//
//	//remainingRequests := int64(foundRule.limit - currentPendingCount - int32(currentUsedCount))
//	//
//	//if remainingRequests <= 0 {
//	//	return time.Duration(timeUntilNextWindow) * time.Millisecond
//	//}
//	//
//	//sleepTime := timeUntilNextWindow / remainingRequests
//
//	msBetweenRequests := window.Milliseconds() / int64(foundRule.limit)
//
//	msSleepTime := int64(currentPendingCount) * msBetweenRequests
//
//	sleepTime := time.Duration(msSleepTime) * time.Millisecond
//
//	if time.Now().Sub(lastRequestTime) > sleepTime {
//		return 0
//	}
//
//	return sleepTime
//}

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
