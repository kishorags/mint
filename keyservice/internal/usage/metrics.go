package usage

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Flusher metrics. Defined in package usage where events happen
// they auto register into the same process wide default registry, so the api package's metreics handler
// exposes them too
var (
	flushWrites = promauto.NewCounter(prometheus.CounterOpts{
		Name: "usage_flush_writes_total",
		Help: "Total usage_counters rows written to Postgres by the flusher.",
	})
	flushDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "usage_flush_duration_seconds",
		Help:    "Wall-clock time of one usage flush.",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	})
	flushTenants = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "usage_flush_tenants",
		Help: "Tenants mirrored in the most recent flush.",
	})
	FlushIsLeader = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "usage_flusher_is_leader",
		Help: "1 if this replica currently holds the flush leader lease, 0 otherwise.",
	})
	FlushLastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "usage_flusher_last_success_timestamp",
		Help: "Unix timestamp of the last successful usage flush on this replica.",
	})
)
