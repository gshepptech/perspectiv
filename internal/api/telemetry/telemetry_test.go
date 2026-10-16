package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"

	"perspectiv/internal/api/telemetry"
)

func TestQueryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "sqlc query", sql: "-- name: GetUserByID :one\nSELECT id FROM users WHERE id = $1", want: "GetUserByID"},
		{name: "leading whitespace", sql: "\n  -- name: ListArcs :many\nSELECT 1", want: "ListArcs"},
		{name: "plain sql", sql: "SELECT 1", want: "unnamed"},
		{name: "empty name", sql: "-- name:\nSELECT 1", want: "unnamed"},
		{name: "empty", sql: "", want: "unnamed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := telemetry.QueryName(tc.sql); got != tc.want {
				t.Fatalf("QueryName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPoolTracerComposesTracers(t *testing.T) {
	t.Parallel()
	tr := telemetry.PoolTracer(zerolog.Nop())
	// otelpgx contributes the batch, copy, prepare, connect and pool-acquire
	// hooks; the composite must forward every one of them.
	if _, ok := tr.(pgx.BatchTracer); !ok {
		t.Error("composite does not trace batches")
	}
	if _, ok := tr.(pgx.ConnectTracer); !ok {
		t.Error("composite does not trace connects")
	}
	if _, ok := tr.(pgxpool.AcquireTracer); !ok {
		t.Error("composite does not trace pool acquires")
	}
}

// TestSetup is serial: Setup installs the process-global tracer provider.
func TestSetup(t *testing.T) {
	tests := []struct {
		name    string
		cfg     telemetry.Config
		wantErr bool
	}{
		{name: "no endpoint traces without export", cfg: telemetry.Config{ServiceName: "perspectiv", ServiceVersion: "test", Environment: "test"}},
		{name: "plaintext URL endpoint", cfg: telemetry.Config{ServiceName: "perspectiv", Endpoint: "http://127.0.0.1:1"}},
		{name: "bare endpoint with insecure", cfg: telemetry.Config{ServiceName: "perspectiv", Endpoint: "127.0.0.1:1", Insecure: true}},
		{name: "missing service name", cfg: telemetry.Config{}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			shutdown, err := telemetry.Setup(ctx, tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			// An unreachable collector must not fail span creation.
			_, span := otel.Tracer("test").Start(ctx, "op")
			if !span.SpanContext().IsValid() {
				t.Fatal("span has no valid trace id")
			}
			span.End()
			sctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			// The collector is unreachable; shutdown may report the failed
			// flush but must give up at its deadline rather than hang.
			started := time.Now()
			_ = shutdown(sctx)
			if waited := time.Since(started); waited > 3*time.Second {
				t.Fatalf("shutdown took %v past a 1s deadline", waited)
			}
		})
	}
}
