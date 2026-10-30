package api_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"perspectiv/internal/api/metrics"
)

// Every call is counted by procedure and the code the client got, a panic
// and a refused sign-in included, and timed by procedure.
func TestMetricsInterceptor(t *testing.T) {
	t.Parallel()
	m := metrics.New(prometheus.NewRegistry())
	s := newStack(t, stackOpts{metrics: m})
	const (
		place   = "/perspectiv.v1.PlaceService/GetPlace"
		profile = "/perspectiv.v1.UserService/UpdateProfile"
	)
	if _, err := s.call(t, place, "", nil); err != nil {
		t.Fatalf("GetPlace: %v", err)
	}
	_, _ = s.call(t, place, "", map[string]string{"X-Test-Panic": "1"})
	_, _ = s.call(t, profile, "", nil)

	tests := []struct {
		procedure, code string
		want            float64
	}{
		{procedure: place, code: "ok", want: 1},
		{procedure: place, code: "internal", want: 1},
		{procedure: profile, code: "unauthenticated", want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.procedure+" "+tc.code, func(t *testing.T) {
			if got := testutil.ToFloat64(m.RPCs.WithLabelValues(tc.procedure, tc.code)); got != tc.want {
				t.Fatalf("rpc_requests_total = %v, want %v", got, tc.want)
			}
		})
	}
	if n := testutil.CollectAndCount(m.RPCDuration); n != 2 {
		t.Fatalf("duration series = %d, want one per procedure (2)", n)
	}
}
