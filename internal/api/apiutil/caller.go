package apiutil

import (
	"context"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// Caller is the authenticated identity of a request. The authentication
// interceptor puts it in the context; handlers read it with CallerFrom and
// never parse a token themselves.
type Caller struct {
	UserID    uuid.UUID
	Role      perspectivv1.Role
	SessionID uuid.UUID
}

type callerKey struct{}

// WithCaller returns a context carrying c.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom returns the caller in ctx. ok is false for an anonymous request.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}
