//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"perspectiv/internal/testdb"
)

// The app's pool plans every statement with its arguments, never JIT
// compiles, and holds at most DB_MAX_CONNS connections.
func TestOpenPoolSessionSettings(t *testing.T) {
	t.Parallel()
	dsn := testdb.New(t).Config().ConnString()
	pool, err := openPool(context.Background(), dsn, 7, zerolog.Nop())
	if err != nil {
		t.Fatalf("openPool: %v", err)
	}
	t.Cleanup(pool.Close)
	for setting, want := range map[string]string{"jit": "off", "plan_cache_mode": "force_custom_plan"} {
		var got string
		if err := pool.QueryRow(context.Background(), "SELECT current_setting($1)", setting).Scan(&got); err != nil {
			t.Fatalf("%s: %v", setting, err)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", setting, got, want)
		}
	}
	if got := pool.Config().MaxConns; got != 7 {
		t.Errorf("max conns = %d, want 7", got)
	}
}
