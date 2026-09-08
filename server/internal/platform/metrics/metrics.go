package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

type Metrics struct {
	path string

	registry     *prometheus.Registry
	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	httpSize     *prometheus.HistogramVec
	httpInFlight prometheus.Gauge
}

func New(cfg config.MetricsConfig) (*Metrics, error) {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		path:     cfg.Path,
		registry: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests handled by the API.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route", "status"}),
		httpSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Namespace,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response size in bytes.",
			Buckets:   prometheus.ExponentialBuckets(100, 2, 12),
		}, []string{"method", "route", "status"}),
		httpInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: cfg.Namespace,
			Subsystem: "http",
			Name:      "in_flight_requests",
			Help:      "Current number of in-flight HTTP requests.",
		}),
	}

	collectors := []prometheus.Collector{
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		m.httpRequests,
		m.httpDuration,
		m.httpSize,
		m.httpInFlight,
	}
	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return nil, fmt.Errorf("register prometheus collector: %w", err)
		}
	}

	return m, nil
}

func (m *Metrics) Path() string {
	if m == nil || m.path == "" {
		return "/metrics"
	}
	return m.path
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func (m *Metrics) IncHTTPInFlight() {
	if m == nil || m.httpInFlight == nil {
		return
	}
	m.httpInFlight.Inc()
}

func (m *Metrics) DecHTTPInFlight() {
	if m == nil || m.httpInFlight == nil {
		return
	}
	m.httpInFlight.Dec()
}

func (m *Metrics) ObserveHTTPRequest(method string, route string, status int, duration time.Duration, responseSize int) {
	if m == nil {
		return
	}
	statusLabel := strconv.Itoa(status)
	m.httpRequests.WithLabelValues(method, route, statusLabel).Inc()
	m.httpDuration.WithLabelValues(method, route, statusLabel).Observe(duration.Seconds())
	if responseSize >= 0 {
		m.httpSize.WithLabelValues(method, route, statusLabel).Observe(float64(responseSize))
	}
}
