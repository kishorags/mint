package store

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ErrBackpressure is returned when the in-flight query limit is reached.
var ErrBackpressure = errors.New("database backpressure: too many in-flight queries")

var (
	semaphoreUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "store_inflight_queries",
		Help: "Current number of in-flight Postgres queries (semaphore utilization).",
	})
	semaphoreRejected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "store_backpressure_rejected_total",
		Help: "Total queries rejected due to backpressure (semaphore full).",
	})
)

// querySemaphore limits the number of concurrent in-flight Postgres queries.
// When full, new queries are rejected immediately with ErrBackpressure rather
// than queuing unbounded goroutines that can exhaust the connection pool.
type querySemaphore struct {
	sem chan struct{}
}

func newQuerySemaphore(limit int) *querySemaphore {
	return &querySemaphore{sem: make(chan struct{}, limit)}
}

// Acquire attempts to take a semaphore slot. Returns ErrBackpressure if the
// semaphore is full. Respects context cancellation.
func (q *querySemaphore) Acquire(ctx context.Context) error {
	select {
	case q.sem <- struct{}{}:
		semaphoreUtilization.Inc()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		semaphoreRejected.Inc()
		return ErrBackpressure
	}
}

// Release returns a semaphore slot.
func (q *querySemaphore) Release() {
	<-q.sem
	semaphoreUtilization.Dec()
}
