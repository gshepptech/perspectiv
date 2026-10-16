package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"perspectiv/internal/api/logging"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &m); err != nil {
		t.Fatalf("log line is not JSON: %q: %v", b, err)
	}
	return m
}

func TestNewLevels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		level       string
		debugLogged bool
		infoLogged  bool
		invalid     bool
	}{
		{name: "debug", level: "debug", debugLogged: true, infoLogged: true},
		{name: "upper case", level: "DEBUG", debugLogged: true, infoLogged: true},
		{name: "warn hides info", level: "warn"},
		{name: "empty means info", level: "", infoLogged: true},
		{name: "unknown means info and is flagged", level: "loud", infoLogged: true, invalid: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			log := logging.New(&buf, tc.level)
			log.Debug().Msg("d")
			if got := buf.Len() > 0; got != tc.debugLogged {
				t.Fatalf("debug logged = %v, want %v", got, tc.debugLogged)
			}
			buf.Reset()
			log.Info().Msg("i")
			if got := buf.Len() > 0; got != tc.infoLogged {
				t.Fatalf("info logged = %v, want %v", got, tc.infoLogged)
			}
			if !tc.infoLogged {
				return
			}
			line := decode(t, buf.Bytes())
			if line["message"] != "i" || line["level"] != "info" || line["time"] == nil {
				t.Fatalf("line = %v", line)
			}
			if _, flagged := line["log_level_invalid"]; flagged != tc.invalid {
				t.Fatalf("log_level_invalid present = %v, want %v", flagged, tc.invalid)
			}
		})
	}
}

func TestCtxAddsCorrelationFields(t *testing.T) {
	t.Parallel()
	traceID := trace.TraceID{1, 2, 3}
	spanID := trace.SpanID{4, 5, 6}
	traced := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))

	tests := []struct {
		name string
		ctx  context.Context
		want map[string]string
	}{
		{name: "bare context adds nothing", ctx: context.Background(), want: map[string]string{}},
		{name: "request id", ctx: logging.WithRequestID(context.Background(), "req-1"), want: map[string]string{"request_id": "req-1"}},
		{
			name: "request id and trace",
			ctx:  logging.WithRequestID(traced, "req-2"),
			want: map[string]string{"request_id": "req-2", "trace_id": traceID.String(), "span_id": spanID.String()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			base := logging.Component(zerolog.New(&buf), "test")
			logging.Ctx(tc.ctx, base).Info().Msg("x")
			line := decode(t, buf.Bytes())
			if line["component"] != "test" {
				t.Fatalf("component = %v", line["component"])
			}
			for _, k := range []string{"request_id", "trace_id", "span_id"} {
				want, ok := tc.want[k]
				got, present := line[k]
				if ok != present || (ok && got != want) {
					t.Fatalf("%s = %v (present %v), want %q (present %v)", k, got, present, want, ok)
				}
			}
			if got := logging.RequestID(tc.ctx); got != tc.want["request_id"] {
				t.Fatalf("RequestID = %q", got)
			}
		})
	}
}
