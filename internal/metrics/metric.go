package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP request metrics — recorded by middleware.
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "platform_http_requests_total",
			Help: "Total HTTP requests by method, path, and status code.",
		},
		[]string{"method", "path", "status_code"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "platform_http_request_duration_seconds",
			Help:    "HTTP request latency distribution.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "path"},
	)

	// Function invocation metrics — recorded by the invoke proxy handlers so
	// data persists across Knative scale-to-zero cycles.
	FunctionInvocationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "platform_function_invocations_total",
			Help: "Total function invocations by workspace, function, and status code.",
		},
		[]string{"workspace", "function", "status_code"},
	)

	FunctionInvocationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "platform_function_invocation_duration_seconds",
			Help:    "Function invocation latency distribution.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"workspace", "function"},
	)

	// Deploy pipeline metrics — recorded by the worker.
	DeployDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "platform_deploy_duration_seconds",
			Help:    "Time from deploy start to completion.",
			Buckets: []float64{1, 5, 10, 15, 30, 60, 120},
		},
		[]string{"status"},
	)

	DeployQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "platform_deploy_queue_depth",
			Help: "Current number of pending messages in the deploy NATS stream.",
		},
	)

	DeploysTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "platform_deploys_total",
			Help: "Total deploys by status.",
		},
		[]string{"status"},
	)
)
