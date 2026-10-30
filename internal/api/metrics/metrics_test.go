package metrics_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"perspectiv/internal/api/metrics"
)

func TestNewExposesDomainCounters(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry()
	m := metrics.New(reg)

	tests := []struct {
		name    string
		counter prometheus.Counter
		metric  string
		incs    int
	}{
		{name: "perspectives published", counter: m.PerspectivesPublished, metric: "perspectives_published_total", incs: 1},
		{name: "claims accepted", counter: m.ClaimsAccepted, metric: "claims_accepted_total", incs: 2},
		{name: "canon changes", counter: m.CanonChanges, metric: "canon_changes_total", incs: 3},
		{name: "job failures", counter: m.JobFailures, metric: "job_failures_total", incs: 4},
		{name: "email send failures", counter: m.EmailSendFailures, metric: "email_send_failures_total", incs: 5},
	}
	for _, tc := range tests {
		for range tc.incs {
			tc.counter.Inc()
		}
	}
	gathered, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	values := map[string]float64{}
	for _, mf := range gathered {
		for _, mm := range mf.GetMetric() {
			if c := mm.GetCounter(); c != nil {
				values[mf.GetName()] = c.GetValue()
			}
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := values[tc.metric]
			if !ok {
				t.Fatalf("%s not gathered from the registry", tc.metric)
			}
			if got != float64(tc.incs) {
				t.Fatalf("%s = %v, want %d", tc.metric, got, tc.incs)
			}
			if c := testutil.ToFloat64(tc.counter); c != float64(tc.incs) {
				t.Fatalf("counter value = %v, want %d", c, tc.incs)
			}
		})
	}
}

func TestNewRegistryHasRuntimeCollectors(t *testing.T) {
	t.Parallel()
	mfs, err := metrics.NewRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	names := map[string]bool{}
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}
	if !names["go_goroutines"] {
		t.Fatal("go_goroutines missing from the registry")
	}
}

func TestNewTwiceOnOneRegistryPanics(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	metrics.New(reg)
	defer func() {
		if recover() == nil {
			t.Fatal("second New on one registry did not panic")
		}
	}()
	metrics.New(reg)
}

// The pool's state is read at scrape time; pgxpool connects lazily, so a
// pool with no reachable server still reports its configured size.
func TestRegisterPoolExportsPoolState(t *testing.T) {
	t.Parallel()
	cfg, err := pgxpool.ParseConfig("postgres://u@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 7
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	reg := metrics.NewRegistry()
	metrics.RegisterPool(reg, pool)
	want := `
# HELP db_pool_max_conns Most connections the pool may hold.
# TYPE db_pool_max_conns gauge
db_pool_max_conns 7
# HELP db_pool_empty_acquires_total Acquires that had to wait because no connection was idle.
# TYPE db_pool_empty_acquires_total counter
db_pool_empty_acquires_total 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "db_pool_max_conns", "db_pool_empty_acquires_total"); err != nil {
		t.Fatal(err)
	}
	if n, err := testutil.GatherAndCount(reg); err != nil || n < 7 {
		t.Fatalf("gathered %d series (%v), want the go, process and 7 pool metrics", n, err)
	}
}
