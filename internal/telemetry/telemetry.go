// Package telemetry is the service's operational visibility: one access-log line
// per request carrying the request id, and Prometheus metrics for traffic and for
// the investigations that are the product's real work.
package telemetry

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "criaisis_http_requests_total", Help: "HTTP requests by route pattern and status.",
	}, []string{"method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "criaisis_http_request_duration_seconds", Help: "HTTP request latency by route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})

	investigations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "criaisis_investigations_total", Help: "Finished investigations by outcome (ok, failed, skipped).",
	}, []string{"outcome"})

	investigationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "criaisis_investigation_duration_seconds", Help: "Wall time of one clash.",
		Buckets: []float64{5, 10, 15, 25, 40, 60, 90},
	})
)

// ObserveInvestigation records one finished job. outcome is ok, failed or skipped.
func ObserveInvestigation(outcome string, d time.Duration) {
	investigations.WithLabelValues(outcome).Inc()
	if outcome == "ok" {
		investigationDuration.Observe(d.Seconds())
	}
}

// RegisterQueueDepth exposes how many incidents wait for a worker. A depth that
// stays near capacity means alerts are about to be shed.
func RegisterQueueDepth(depth func() int) {
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "criaisis_queue_depth", Help: "Incidents waiting for a worker.",
	}, func() float64 { return float64(depth()) })
}

// RegisterDeadLetters exposes how many jobs exhausted their attempts. Any growth
// is an investigation that never reached anyone and should page.
func RegisterDeadLetters(count func() int) {
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "criaisis_jobs_dead", Help: "Jobs that exhausted their retry attempts.",
	}, func() float64 { return float64(count()) })
}

// Middleware logs and measures every request. The route is the chi pattern, not
// the raw path, so metric cardinality stays bounded and ids never become labels.
// The query string is never logged: it can carry an OAuth code.
func Middleware(log *zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			elapsed := time.Since(start)

			httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
			httpDuration.WithLabelValues(route).Observe(elapsed.Seconds())

			if route == "/healthz" || route == "/readyz" || route == "/metrics" {
				return // probes would drown the log
			}
			log.Info().
				Str("request_id", middleware.GetReqID(r.Context())).
				Str("method", r.Method).Str("route", route).Str("path", r.URL.Path).
				Int("status", status).Int("bytes", ww.BytesWritten()).
				Dur("duration", elapsed).
				Msg("http request")
		})
	}
}

// Handler serves /metrics behind a bearer token. With no token configured the
// route must not be mounted at all: fail closed, never expose it by default.
func Handler(token string) http.Handler {
	h := promhttp.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
