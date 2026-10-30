package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// RegisterPool exports the database pool's state, read at scrape time. A
// pool that is always full shows as acquired equal to max with the
// empty-acquire count climbing: requests are waiting for a connection.
func RegisterPool(reg prometheus.Registerer, pool *pgxpool.Pool) {
	gauge := func(name, help string, v func(*pgxpool.Stat) float64) prometheus.Collector {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "db_pool_" + name, Help: help},
			func() float64 { return v(pool.Stat()) })
	}
	counter := func(name, help string, v func(*pgxpool.Stat) float64) prometheus.Collector {
		return prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "db_pool_" + name, Help: help},
			func() float64 { return v(pool.Stat()) })
	}
	reg.MustRegister(
		gauge("max_conns", "Most connections the pool may hold.", func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) }),
		gauge("acquired_conns", "Connections in use.", func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) }),
		gauge("idle_conns", "Connections open and idle.", func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) }),
		counter("acquires_total", "Connections handed out.", func(s *pgxpool.Stat) float64 { return float64(s.AcquireCount()) }),
		counter("empty_acquires_total", "Acquires that had to wait because no connection was idle.",
			func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) }),
		counter("acquire_wait_seconds_total", "Time spent acquiring connections.",
			func(s *pgxpool.Stat) float64 { return s.AcquireDuration().Seconds() }),
		counter("canceled_acquires_total", "Acquires abandoned because the request ended first.",
			func(s *pgxpool.Stat) float64 { return float64(s.CanceledAcquireCount()) }),
	)
}
