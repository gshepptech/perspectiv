// Package apiutil holds the helpers every Connect handler shares: typed
// domain errors and their single mapping to Connect codes, the caller
// identity carried in the request context, and cursor pagination tokens.
package apiutil

import (
	"context"
	"errors"

	"connectrpc.com/connect"
)

// codedError is a domain error that already knows its Connect code. Domain
// code returns it through the constructors below and may wrap it with %w; the
// boundary maps it with ToConnect.
type codedError struct {
	code  connect.Code
	msg   string
	cause error
}

func (e *codedError) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

func (e *codedError) Unwrap() error { return e.cause }

// InvalidArg reports a request that fails validation.
func InvalidArg(msg string) error { return &codedError{code: connect.CodeInvalidArgument, msg: msg} }

// NotFound reports a missing or hidden resource.
func NotFound(msg string) error { return &codedError{code: connect.CodeNotFound, msg: msg} }

// AlreadyExists reports a uniqueness collision.
func AlreadyExists(msg string) error { return &codedError{code: connect.CodeAlreadyExists, msg: msg} }

// PermissionDenied reports an authenticated caller who may not do this.
func PermissionDenied(msg string) error {
	return &codedError{code: connect.CodePermissionDenied, msg: msg}
}

// FailedPrecondition reports a request the current state does not allow.
func FailedPrecondition(msg string) error {
	return &codedError{code: connect.CodeFailedPrecondition, msg: msg}
}

// Unauthenticated reports missing, expired or invalid credentials.
func Unauthenticated(msg string) error {
	return &codedError{code: connect.CodeUnauthenticated, msg: msg}
}

// ResourceExhausted reports a rate limit or lockout.
func ResourceExhausted(msg string) error {
	return &codedError{code: connect.CodeResourceExhausted, msg: msg}
}

// Internal wraps an unexpected error. The cause stays reachable through
// errors.Is and errors.As for logging; ToConnect never sends it to the client.
func Internal(err error) error {
	return &codedError{code: connect.CodeInternal, msg: internalMessage, cause: err}
}

const internalMessage = "internal error"

// CodeOf reports the Connect code ToConnect would give err, and whether err
// carries one of the typed errors above (anywhere in its wrap chain).
func CodeOf(err error) (connect.Code, bool) {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code, true
	}
	return connect.CodeInternal, false
}

// ToConnect maps an error to the *connect.Error a handler returns.
//
// A typed error from this package, wrapped or not, keeps its code and its
// message; Internal keeps only the generic message. An error that already is
// a *connect.Error passes through unchanged, so an interceptor's own error is
// not rewritten. context.Canceled and context.DeadlineExceeded map to
// CodeCanceled and CodeDeadlineExceeded, because a client that went away is
// not a server fault. Every other error becomes CodeInternal with a generic
// message, so no internal detail reaches the client.
func ToConnect(err error) error {
	if err == nil {
		return nil
	}
	var ce *codedError
	if errors.As(err, &ce) {
		if ce.code == connect.CodeInternal {
			return connect.NewError(connect.CodeInternal, errors.New(internalMessage))
		}
		return connect.NewError(ce.code, errors.New(ce.msg))
	}
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr
	}
	switch {
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, errors.New("request canceled"))
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, errors.New("deadline exceeded"))
	}
	return connect.NewError(connect.CodeInternal, errors.New(internalMessage))
}
