package store

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// PoolCollector exports pgxpool stats as Prometheus metrics. Register it with
// the default registry so /metrics exposes pool utilization.
type PoolCollector struct {
	pool *pgxpool.Pool

	acquiredConns *prometheus.Desc
	idleConns     *prometheus.Desc
	totalConns    *prometheus.Desc
	maxConns      *prometheus.Desc
}

// NewPoolCollector creates a collector for the given pool.
func NewPoolCollector(pool *pgxpool.Pool) *PoolCollector {
	return &PoolCollector{
		pool: pool,
		acquiredConns: prometheus.NewDesc(
			"pgxpool_acquired_conns",
			"Number of currently acquired connections in the pool.",
			nil, nil,
		),
		idleConns: prometheus.NewDesc(
			"pgxpool_idle_conns",
			"Number of currently idle connections in the pool.",
			nil, nil,
		),
		totalConns: prometheus.NewDesc(
			"pgxpool_total_conns",
			"Total number of connections currently in the pool.",
			nil, nil,
		),
		maxConns: prometheus.NewDesc(
			"pgxpool_max_conns",
			"Maximum number of connections allowed in the pool.",
			nil, nil,
		),
	}
}

func (c *PoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquiredConns
	ch <- c.idleConns
	ch <- c.totalConns
	ch <- c.maxConns
}

func (c *PoolCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(stat.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stat.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stat.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns()))
}
