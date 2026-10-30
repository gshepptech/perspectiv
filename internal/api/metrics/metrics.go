// Package metrics defines the Prometheus collectors the domains increment.
// The /metrics endpoint serves the registry they are registered on.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics holds the domain counters. Each domain increments only its own:
// perspective publishes, lore accepts claims, the rank job changes canon, the
// job client counts failed attempts and the mailer counts send failures.
type Metrics struct {
	PerspectivesPublished prometheus.Counter
	ClaimsAccepted        prometheus.Counter
	CanonChanges          prometheus.Counter
	JobFailures           prometheus.Counter
	EmailSendFailures     prometheus.Counter
	// RPCs counts every API call by procedure and Connect code ("ok" for
	// success); RPCDuration times them by procedure. The procedures are the
	// fixed set the services declare, so the labels stay bounded.
	RPCs        *prometheus.CounterVec
	RPCDuration *prometheus.HistogramVec
}

// New creates the domain counters and registers them on reg. It panics when
// reg already holds collectors with these names, which is a wiring bug in the
// composition root, not a runtime condition.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		PerspectivesPublished: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "perspectives_published_total",
			Help: "Perspectives moved from draft to published.",
		}),
		ClaimsAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "claims_accepted_total",
			Help: "Character claims accepted by the claimed perspective's author.",
		}),
		CanonChanges: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "canon_changes_total",
			Help: "Arc canon changes written by the rank job, including canon cleared.",
		}),
		JobFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "job_failures_total",
			Help: "Failed background job attempts.",
		}),
		EmailSendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "email_send_failures_total",
			Help: "Email sends the mail provider rejected or that failed in transit.",
		}),
		RPCs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_requests_total",
			Help: "API calls by procedure and Connect code (ok for success).",
		}, []string{"procedure", "code"}),
		RPCDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rpc_request_duration_seconds",
			Help:    "API call duration by procedure, from the outermost interceptor.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"procedure"}),
	}
	reg.MustRegister(m.PerspectivesPublished, m.ClaimsAccepted, m.CanonChanges, m.JobFailures, m.EmailSendFailures,
		m.RPCs, m.RPCDuration)
	return m
}

// NewRegistry returns a registry holding the Go runtime and process
// collectors, ready for New and for the router's /metrics endpoint.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}
