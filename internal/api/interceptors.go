package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/logging"
)

// Authenticator turns request headers into a caller. ok is false when the
// request presents no credentials. An error that is apiutil.Unauthenticated
// (invalid or expired credentials) is returned to the client as
// Unauthenticated so it knows to refresh; any other error means the
// credentials could not be checked and the call fails closed as Unavailable.
type Authenticator interface {
	Authenticate(ctx context.Context, h http.Header) (apiutil.Caller, bool, error)
}

// Standing is what the shared write gate knows about an account. The zero
// value refuses the write, so a gate that answers nothing fails closed.
type Standing int

const (
	// StandingUnverified: the email is not verified, or the account is
	// unknown or deleted.
	StandingUnverified Standing = iota
	// StandingSuspended: moderation suspended the account. It still signs in
	// and reads, but writes nothing other people read.
	StandingSuspended
	// StandingWriter: verified and not suspended.
	StandingWriter
)

// Gate answers the shared write gate from the users row on every call, so a
// suspension or a verification takes effect on the account's next write.
type Gate interface {
	WriteStanding(ctx context.Context, userID uuid.UUID) (Standing, error)
}

// Limiter counts one attempt of action by subject and reports whether it is
// within the limit. subject is "user:<uuid>" or "ip:<addr>".
type Limiter interface {
	Allow(ctx context.Context, action string, subject string) (bool, error)
}

// RequestIDHeader carries the correlation id in and out of every call.
const RequestIDHeader = "X-Request-Id"

const (
	msgVerifyEmail = "verify your email first"
	msgSuspended   = "your account is suspended"
	msgSlowDown    = "slow down"
	msgUnavailable = "service temporarily unavailable"
)

// Interceptors returns the Connect interceptor chain, outermost first:
// recovery, request id and tracing, logging, authentication, authorization,
// validation. Every Connect handler is mounted with exactly this chain.
func Interceptors(log zerolog.Logger, authn Authenticator, gate Gate, lim Limiter) []connect.Interceptor {
	log = logging.Component(log, "api")
	return []connect.Interceptor{
		&recoveryInterceptor{log: log},
		&requestInterceptor{tracer: otel.Tracer("perspectiv/internal/api")},
		&loggingInterceptor{log: log},
		newAuthn(log, authn),
		newAuthz(log, gate, lim),
		validationInterceptor{},
	}
}

// call is what a check needs to know about a request, unary or streaming.
type call struct {
	procedure string
	peerAddr  string
	header    http.Header
}

// checkInterceptor adapts a check that may replace the context or reject the
// call into a Connect interceptor for unary and streaming handlers.
type checkInterceptor struct {
	check func(ctx context.Context, c call) (context.Context, error)
}

func (i checkInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		ctx, err := i.check(ctx, call{req.Spec().Procedure, req.Peer().Addr, req.Header()})
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i checkInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i checkInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.check(ctx, call{conn.Spec().Procedure, conn.Peer().Addr, conn.RequestHeader()})
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// ---------------------------------------------------------------------------
// Recovery

// recoveryInterceptor turns a handler panic into CodeInternal and logs the
// stack, so one bad request cannot take the process down.
type recoveryInterceptor struct {
	log zerolog.Logger
}

func (i *recoveryInterceptor) recovered(ctx context.Context, procedure string, r any) error {
	logging.Ctx(ctx, i.log).Error().
		Str("event", "panic").
		Str("method", procedure).
		Str("panic", fmt.Sprint(r)).
		Bytes("stack", debug.Stack()).
		Msg("handler panicked")
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

func (i *recoveryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) {
		defer func() {
			if r := recover(); r != nil {
				resp, err = nil, i.recovered(ctx, req.Spec().Procedure, r)
			}
		}()
		return next(ctx, req)
	}
}

func (i *recoveryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *recoveryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = i.recovered(ctx, conn.Spec().Procedure, r)
			}
		}()
		return next(ctx, conn)
	}
}

// ---------------------------------------------------------------------------
// Request id and tracing

// requestInterceptor gives every call a correlation id and a server span.
// A client-supplied X-Request-Id is kept when it is a short printable token,
// otherwise a new UUID is minted; the id is echoed in the response header.
// The span continues a W3C traceparent the client sent.
type requestInterceptor struct {
	tracer trace.Tracer
}

func (i *requestInterceptor) start(ctx context.Context, procedure string, h http.Header) (context.Context, trace.Span, string) {
	id := h.Get(RequestIDHeader)
	if !validRequestID(id) {
		id = uuid.NewString()
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(h))
	ctx, span := i.tracer.Start(ctx, strings.TrimPrefix(procedure, "/"),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("rpc.system", "connect_rpc"),
			attribute.String("rpc.method", procedure),
			attribute.String("request.id", id),
		))
	return logging.WithRequestID(ctx, id), span, id
}

func endSpan(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, connect.CodeOf(err).String())
		span.SetAttributes(attribute.String("rpc.connect_rpc.error_code", connect.CodeOf(err).String()))
	}
	span.End()
}

func (i *requestInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		ctx, span, id := i.start(ctx, req.Spec().Procedure, req.Header())
		resp, err := next(ctx, req)
		endSpan(span, err)
		var cerr *connect.Error
		switch {
		case err == nil && resp != nil:
			resp.Header().Set(RequestIDHeader, id)
		case errors.As(err, &cerr):
			cerr.Meta().Set(RequestIDHeader, id)
		}
		return resp, err
	}
}

func (i *requestInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *requestInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, span, id := i.start(ctx, conn.Spec().Procedure, conn.RequestHeader())
		conn.ResponseHeader().Set(RequestIDHeader, id)
		err := next(ctx, conn)
		endSpan(span, err)
		return err
	}
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Logging

// loggingInterceptor writes one line per call: method, code and duration.
// Server faults log at error, client errors at warn, success at info.
type loggingInterceptor struct {
	log zerolog.Logger
}

func (i *loggingInterceptor) record(ctx context.Context, procedure string, started time.Time, err error) {
	log := logging.Ctx(ctx, i.log)
	ev := log.Info()
	code := "ok"
	if err != nil {
		c := connect.CodeOf(err)
		code = c.String()
		switch c {
		case connect.CodeInternal, connect.CodeUnknown, connect.CodeDataLoss, connect.CodeUnavailable:
			ev = log.Error().Err(err)
		default:
			ev = log.Warn()
		}
	}
	ev.Str("event", "rpc").Str("method", procedure).Str("code", code).Dur("duration", time.Since(started)).Msg("rpc")
}

func (i *loggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		started := time.Now()
		resp, err := next(ctx, req)
		i.record(ctx, req.Spec().Procedure, started, err)
		return resp, err
	}
}

func (i *loggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *loggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		started := time.Now()
		err := next(ctx, conn)
		i.record(ctx, conn.Spec().Procedure, started, err)
		return err
	}
}

// ---------------------------------------------------------------------------
// Authentication and authorization

// denial is one authz_denied audit line.
type denial struct {
	sub      string
	method   string
	required string
	role     string
	reason   string
}

// deny writes the audit line synchronously, before the error is returned,
// and returns err.
func deny(ctx context.Context, log zerolog.Logger, d denial, err error) error {
	logging.Ctx(ctx, log).Warn().
		Str("event", "authz_denied").
		Str("sub", d.sub).
		Str("method", d.method).
		Str("required", d.required).
		Str("role", d.role).
		Str("reason", d.reason).
		Msg("request denied")
	return err
}

func unavailable() error {
	return connect.NewError(connect.CodeUnavailable, errors.New(msgUnavailable))
}

// callerFields returns the sub and role an audit line records.
func callerFields(ctx context.Context) (sub, role string) {
	c, ok := apiutil.CallerFrom(ctx)
	if !ok {
		return "", "anonymous"
	}
	return c.UserID.String(), c.Role.String()
}

func newAuthn(log zerolog.Logger, authn Authenticator) checkInterceptor {
	return checkInterceptor{check: func(ctx context.Context, c call) (context.Context, error) {
		caller, ok, err := authn.Authenticate(ctx, c.header)
		if err != nil {
			d := denial{method: c.procedure, required: requiredFor(c.procedure), role: "anonymous"}
			if code, typed := apiutil.CodeOf(err); typed && code == connect.CodeUnauthenticated {
				d.reason = "invalid_credentials"
				return ctx, deny(ctx, log, d, apiutil.ToConnect(err))
			}
			d.reason = "authenticator_error"
			logging.Ctx(ctx, log).Error().Err(err).Str("method", c.procedure).Msg("authenticator failed")
			return ctx, deny(ctx, log, d, unavailable())
		}
		if !ok {
			return ctx, nil
		}
		return apiutil.WithCaller(ctx, caller), nil
	}}
}

func newAuthz(log zerolog.Logger, gate Gate, lim Limiter) checkInterceptor {
	return checkInterceptor{check: func(ctx context.Context, c call) (context.Context, error) {
		return ctx, authorize(ctx, log, gate, lim, c)
	}}
}

// authorize applies the procedure's policy in order: known procedure, access
// class, verified email, rate limit. Each stage fails closed.
func authorize(ctx context.Context, log zerolog.Logger, gate Gate, lim Limiter, c call) error {
	sub, role := callerFields(ctx)
	d := denial{sub: sub, method: c.procedure, role: role}
	pol, known := Routes[c.procedure]
	if !known {
		d.required, d.reason = "unknown", "unknown_procedure"
		return deny(ctx, log, d, apiutil.ToConnect(apiutil.PermissionDenied("permission denied")))
	}
	d.required = pol.Access.String()
	caller, signedIn := apiutil.CallerFrom(ctx)
	if (pol.Access >= User || pol.Verified) && !signedIn {
		d.reason = "not_signed_in"
		return deny(ctx, log, d, apiutil.ToConnect(apiutil.Unauthenticated("sign in required")))
	}
	if pol.Access == Admin && caller.Role != perspectivv1.Role_ROLE_ADMIN {
		d.reason = "role"
		return deny(ctx, log, d, apiutil.ToConnect(apiutil.PermissionDenied("admin only")))
	}
	if pol.Verified {
		d.required = "verified_email"
		if err := checkVerified(ctx, log, gate, caller, &d); err != nil {
			return err
		}
	}
	if pol.RateLimit != "" {
		d.required = "rate_limit:" + pol.RateLimit
		subjects := []string{"ip:" + clientIP(c.peerAddr, c.header)}
		if signedIn {
			subjects = append([]string{"user:" + caller.UserID.String()}, subjects...)
		}
		return checkRateLimit(ctx, log, lim, pol.RateLimit, subjects, &d)
	}
	return nil
}

func checkVerified(ctx context.Context, log zerolog.Logger, gate Gate, caller apiutil.Caller, d *denial) error {
	standing, err := gate.WriteStanding(ctx, caller.UserID)
	if err != nil {
		d.reason = "gate_error"
		logging.Ctx(ctx, log).Error().Err(err).Str("method", d.method).Msg("write gate failed")
		return deny(ctx, log, *d, unavailable())
	}
	switch standing {
	case StandingWriter:
		return nil
	case StandingSuspended:
		d.reason = "suspended"
		return deny(ctx, log, *d, apiutil.ToConnect(apiutil.PermissionDenied(msgSuspended)))
	case StandingUnverified:
		d.reason = "email_unverified"
		return deny(ctx, log, *d, apiutil.ToConnect(apiutil.FailedPrecondition(msgVerifyEmail)))
	default:
		d.reason = "gate_error"
		logging.Ctx(ctx, log).Error().Int("standing", int(standing)).Str("method", d.method).Msg("write gate gave an unknown standing")
		return deny(ctx, log, *d, unavailable())
	}
}

// checkRateLimit counts the call against every subject, user first. The
// first subject over its limit, or any limiter error, denies the call.
func checkRateLimit(ctx context.Context, log zerolog.Logger, lim Limiter, action string, subjects []string, d *denial) error {
	for _, s := range subjects {
		allowed, err := lim.Allow(ctx, action, s)
		if err != nil {
			d.reason = "limiter_error"
			logging.Ctx(ctx, log).Error().Err(err).Str("method", d.method).Msg("rate limiter failed")
			return deny(ctx, log, *d, unavailable())
		}
		if !allowed {
			d.reason = "rate_limited"
			return deny(ctx, log, *d, apiutil.ToConnect(apiutil.ResourceExhausted(msgSlowDown)))
		}
	}
	return nil
}

// requiredFor names a procedure's access class for an audit line written
// before authorization runs.
func requiredFor(procedure string) string {
	if pol, ok := Routes[procedure]; ok {
		return pol.Access.String()
	}
	return "unknown"
}

// clientIP returns the address the per-IP limit counts against.
//
// The TCP peer address is used as-is when it is a public address: a client
// talking to the server directly cannot choose it. When the peer is a
// loopback or private address the server sits behind a proxy (ingress-nginx
// in the cluster, Vite in development), and the rightmost X-Forwarded-For
// entry, the one that proxy appended, is the client. Entries further left are
// client-controlled and never trusted.
func clientIP(peerAddr string, h http.Header) string {
	host := peerAddr
	if hp, _, err := net.SplitHostPort(peerAddr); err == nil {
		host = hp
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !peer.IsLoopback() && !peer.IsPrivate() {
		return peer.String()
	}
	xff := h.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return peer.String()
	}
	parts := strings.Split(xff[len(xff)-1], ",")
	if fwd, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
		return fwd.Unmap().String()
	}
	return peer.String()
}
