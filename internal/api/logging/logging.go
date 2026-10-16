// Package logging builds the binary's one zerolog logger and carries
// request-scoped correlation fields through the context.
package logging

import (
	"context"
	"io"
	"strings"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// New returns a JSON logger writing to w at level. An empty or unknown level
// means info: the logger is built before config errors can be reported, and a
// typo in LOG_LEVEL must not silence the server. The returned logger records
// the fallback in a field so the typo is visible.
func New(w io.Writer, level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	ctx := zerolog.New(w).With().Timestamp()
	if err != nil || lvl == zerolog.NoLevel {
		if level != "" {
			ctx = ctx.Str("log_level_invalid", level)
		}
		lvl = zerolog.InfoLevel
	}
	return ctx.Logger().Level(lvl)
}

type requestIDKey struct{}

// WithRequestID returns a context carrying the request's correlation id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the correlation id in ctx, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Ctx returns log with the request id and the active trace and span ids from
// ctx added, so a line can be joined to its request and its trace.
func Ctx(ctx context.Context, log zerolog.Logger) *zerolog.Logger {
	lc := log.With()
	if id := RequestID(ctx); id != "" {
		lc = lc.Str("request_id", id)
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		lc = lc.Str("trace_id", sc.TraceID().String()).Str("span_id", sc.SpanID().String())
	}
	l := lc.Logger()
	return &l
}

// Component returns log tagged with the component that writes through it.
func Component(log zerolog.Logger, name string) zerolog.Logger {
	return log.With().Str("component", name).Logger()
}
