//go:build integration

package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) queryLines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		var m map[string]any
		if raw == "" {
			continue
		}
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", raw)
		}
		if m["message"] == "db query" {
			out = append(out, m)
		}
	}
	return out
}

// childSpanMentions reports whether a child of parent carries an attribute
// whose value contains text.
func childSpanMentions(spans []sdktrace.ReadOnlySpan, parent trace.SpanID, text string) bool {
	for _, s := range spans {
		if s.Parent().SpanID() != parent {
			continue
		}
		for _, a := range s.Attributes() {
			if strings.Contains(a.Value.String(), text) {
				return true
			}
		}
	}
	return false
}

// TestPoolTracerAgainstPostgres runs real queries through a pool built the
// way the composition root builds it and checks both halves of the
// composite: the query log line and the otelpgx span. It is serial because it
// installs the global tracer provider otelpgx reads.
func TestPoolTracerAgainstPostgres(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	base := testdb.New(t)
	cfg, err := pgxpool.ParseConfig(base.Config().ConnString())
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	logs := &lockedBuffer{}
	cfg.ConnConfig.Tracer = telemetry.PoolTracer(zerolog.New(logs).Level(zerolog.DebugLevel))
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// otelpgx traces queries inside a request's span, as in the server.
	ctx, parent := tp.Tracer("test").Start(ctx, "request")
	var one int
	if err := pool.QueryRow(ctx, "-- name: ProbeOne :one\nSELECT $1::int", 1).Scan(&one); err != nil || one != 1 {
		t.Fatalf("probe query = %d, %v", one, err)
	}
	err = pool.QueryRow(ctx, "-- name: ProbeNoRows :one\nSELECT 1 WHERE false").Scan(&one)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("no-rows query err = %v", err)
	}
	if _, err := pool.Exec(ctx, "-- name: ProbeBroken :exec\nSELECT * FROM no_such_table_here"); err == nil {
		t.Fatal("broken query succeeded")
	}

	parent.End()

	want := map[string]string{"ProbeOne": "debug", "ProbeNoRows": "debug", "ProbeBroken": "warn"}
	got := map[string]string{}
	for _, line := range logs.queryLines(t) {
		name, _ := line["query"].(string)
		level, _ := line["level"].(string)
		got[name] = level
		if _, ok := line["duration"]; !ok {
			t.Errorf("query line lacks duration: %v", line)
		}
	}
	for name, level := range want {
		if got[name] != level {
			t.Errorf("query %s logged at %q, want %q (all: %v)", name, got[name], level, got)
		}
	}

	if !childSpanMentions(rec.Ended(), parent.SpanContext().SpanID(), "ProbeOne") {
		t.Fatalf("no otelpgx span recorded for ProbeOne among %d spans", len(rec.Ended()))
	}
}
