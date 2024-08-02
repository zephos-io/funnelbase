package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"zephos/funnelbase/pkg/util"
)

var (
	logger = util.NewLogger().With().Str("component", "prometheus").Logger()
)

// general grpc metrics
var (
	ReqsReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "grpc",
			Name:      "reqs_received_total",
			Help:      "Total number of requests received through gRPC",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	CachedResponses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "grpc",
			Name:      "cached_responses_total",
			Help:      "Total number of responses from gRPC that were fully cached (including batch requests)",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	ErrorResponses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "grpc",
			Name:      "error_responses_total",
			Help:      "Total number of gRPC responses that were errors",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	SuccessResponses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "grpc",
			Name:      "success_responses_total",
			Help:      "Total number of gRPC responses that were successful",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)
)

// rate limiter metrics
var (
	ReqsAllowed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_allowed_total",
			Help:      "Total number of requests allowed through the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	ReqsWaiting = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_waiting",
			Help:      "Number of requests waiting to be allowed through the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority"},
	)

	ReqsWaitTime = promauto.NewHistogramVec(
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

	ReqsCancelled = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "reqs_cancelled_total",
			Help:      "Total number of requests cancelled while in the rate limiter",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	Backoffs = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "rate_limiter",
			Name:      "backoff_total",
			Help:      "Total number of backoff's requested by APIs",
		}, []string{"rate_limiter_name", "limit_name"},
	)
)

// outbound api request metrics
var (
	OutboundRequests = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: "funnelbase",
			Subsystem: "outbound_requester",
			Name:      "reqs",
			Help:      "Total number of outbound requests performed and latencies (in milliseconds)",
		}, []string{"limit_name", "client"},
	)
)

// batch metrics
var (
	BatchItems = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "cache",
			Name:      "batch_items_total",
			Help:      "Total number of items in batch requests",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)

	BatchItemsCached = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "funnelbase",
			Subsystem: "cache",
			Name:      "batch_items_cached_total",
			Help:      "Total number of items in batch requests that exist in the cache",
		}, []string{"rate_limiter_name", "limit_name", "queue_priority", "client"},
	)
)

func ListenAndServe() {
	logger.Info().Msg("starting prometheus server")
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe("0.0.0.0:2112", nil)
	if err != nil {
		logger.Fatal().Err(err).Msg("error starting http server")
	}
}
