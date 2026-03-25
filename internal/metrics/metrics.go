package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ccgateway_requests_total",
		Help: "Total number of proxy requests",
	}, []string{"provider", "model", "account_id", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ccgateway_request_duration_seconds",
		Help:    "Request duration in seconds",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
	}, []string{"provider", "model"})

	TokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ccgateway_tokens_total",
		Help: "Total tokens processed",
	}, []string{"provider", "account_id", "direction"})

	ActiveRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ccgateway_active_requests",
		Help: "Number of currently active requests",
	}, []string{"account_id"})

	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ccgateway_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half_open)",
	}, []string{"account_id"})

	CostTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ccgateway_cost_usd_total",
		Help: "Total cost in USD",
	}, []string{"provider", "account_id", "model"})

	RecorderDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ccgateway_recorder_dropped_total",
		Help: "Total number of recording queue drops due to full queue",
	})
)
