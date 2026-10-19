//go:build integration

package auth_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// A suspended account fails the shared write gate: every route that needs a
// verified email refuses it, although its email is verified and its access
// token is still live. The gate reads the users row on every call, so the
// suspension takes effect without the account signing in again.
func TestSuspendedAccountFailsWriteGate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.signUp("Writer_One")
	tok := e.linkToken(emailOf("Writer_One"), "verify_email", "/verify-email")
	if _, err := e.auth().VerifyEmail(context.Background(), connect.NewRequest(&perspectivv1.VerifyEmailRequest{Token: tok})); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	access, _ := e.mustLogin("Writer_One")
	for rpc, err := range e.verifiedRoutes(access) {
		if connect.CodeOf(err) != connect.CodeUnimplemented {
			t.Fatalf("%s before suspension: %v, want it to reach the handler", rpc, err)
		}
	}

	// The write moderation's SUSPEND_USER action makes.
	e.exec(`UPDATE users SET suspended_at = coalesce(suspended_at, now()) WHERE id = $1`, id)

	for rpc, err := range e.verifiedRoutes(access) {
		if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "your account is suspended") {
			t.Errorf("%s while suspended: %v, want PermissionDenied naming the suspension", rpc, err)
		}
	}
}
