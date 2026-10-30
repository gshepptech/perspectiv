package api

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"perspectiv/internal/api/metrics"
)

// MetricsInterceptor counts and times every API call by procedure and code.
// It goes outermost, before recovery, so a panic, a refused sign-in and a
// rate-limited call are all counted with the code the client received.
func MetricsInterceptor(m *metrics.Metrics) connect.Interceptor {
	return &metricsInterceptor{m: m}
}

type metricsInterceptor struct {
	m *metrics.Metrics
}

func (i *metricsInterceptor) record(procedure string, started time.Time, err error) {
	code := "ok"
	if err != nil {
		code = connect.CodeOf(err).String()
	}
	i.m.RPCs.WithLabelValues(procedure, code).Inc()
	i.m.RPCDuration.WithLabelValues(procedure).Observe(time.Since(started).Seconds())
}

func (i *metricsInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		started := time.Now()
		resp, err := next(ctx, req)
		i.record(req.Spec().Procedure, started, err)
		return resp, err
	}
}

func (i *metricsInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *metricsInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		started := time.Now()
		err := next(ctx, conn)
		i.record(conn.Spec().Procedure, started, err)
		return err
	}
}
