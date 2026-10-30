package api_test

import (
	"sort"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api"
)

// generatedProcedures lists every RPC in the generated perspectiv.v1 services.
func generatedProcedures(t *testing.T) []string {
	t.Helper()
	_ = perspectivv1.Role_ROLE_USER // links the generated files into the registry
	var procs []string
	protoregistry.GlobalFiles.RangeFilesByPackage("perspectiv.v1", func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			svc := fd.Services().Get(i)
			for j := range svc.Methods().Len() {
				procs = append(procs, "/"+string(svc.FullName())+"/"+string(svc.Methods().Get(j).Name()))
			}
		}
		return true
	})
	sort.Strings(procs)
	if len(procs) == 0 {
		t.Fatal("no generated perspectiv.v1 services registered")
	}
	return procs
}

func TestRoutesCoverEveryProcedure(t *testing.T) {
	t.Parallel()
	procs := generatedProcedures(t)
	inProto := map[string]bool{}
	for _, p := range procs {
		inProto[p] = true
		if _, ok := api.Routes[p]; !ok {
			t.Errorf("%s has no policy in api.Routes", p)
		}
	}
	for p := range api.Routes {
		if !inProto[p] {
			t.Errorf("api.Routes has %s, which no service defines", p)
		}
	}
}

func TestRoutePolicies(t *testing.T) {
	t.Parallel()
	verified := map[string]string{
		"/perspectiv.v1.PlaceService/CreatePlace":        "",
		"/perspectiv.v1.PerspectiveService/Publish":      api.LimitPublish,
		"/perspectiv.v1.PerspectiveService/Update":       "",
		"/perspectiv.v1.PerspectiveService/CreateInvite": "",
		"/perspectiv.v1.LoreService/AddLorePost":         api.LimitPost,
		"/perspectiv.v1.LoreService/NameLore":            api.LimitPost,
		"/perspectiv.v1.ArcService/RetitleHourArc":       "",
		"/perspectiv.v1.LoreService/ResolveClaim":        "",
		"/perspectiv.v1.LoreService/ClaimCharacter":      api.LimitClaim,
		"/perspectiv.v1.VoteService/Vote":                api.LimitVote,
		"/perspectiv.v1.VoteService/VoteRemove":          api.LimitVote,
		"/perspectiv.v1.ReactService/React":              api.LimitReact,
		"/perspectiv.v1.ModerationService/Report":        api.LimitReport,
		"/perspectiv.v1.UserService/UpdateProfile":       "",
		"/perspectiv.v1.UserService/ChangeUsername":      "",
	}
	admin := map[string]bool{
		"/perspectiv.v1.ModerationService/ListReports":  true,
		"/perspectiv.v1.ModerationService/Resolve":      true,
		"/perspectiv.v1.ModerationService/ListAuditLog": true,
		"/perspectiv.v1.ModerationService/LookupUser":   true,
		"/perspectiv.v1.AuthService/CreateAccessCode":   true,
		"/perspectiv.v1.AuthService/ListAccessCodes":    true,
		"/perspectiv.v1.AuthService/RevokeAccessCode":   true,
		"/perspectiv.v1.AuthService/ListWaitlist":       true,
		"/perspectiv.v1.AuthService/InviteFromWaitlist": true,
	}
	rateLimited := map[string]string{
		"/perspectiv.v1.AuthService/SignUp": api.LimitSignup,
		"/perspectiv.v1.AuthService/Login":  api.LimitLogin,
		// Joining the waitlist shares sign-up's budget: both create rows for
		// anyone who asks.
		"/perspectiv.v1.AuthService/JoinWaitlist": api.LimitSignup,
		// Both take the current password, so they share login's guessing limit.
		"/perspectiv.v1.AuthService/ChangePassword":     api.LimitLogin,
		"/perspectiv.v1.AuthService/RequestEmailChange": api.LimitLogin,
		// Searching descriptions is public and scans text; it is bounded per
		// address like the other public writes.
		"/perspectiv.v1.PlaceService/SearchCharacters": api.LimitSearch,
		// An export builds one large document; a few an hour is plenty.
		"/perspectiv.v1.UserService/ExportMyData": api.LimitExport,
		// Admin only, but a lookup by email is bounded so a leaked admin
		// token cannot probe addresses quickly.
		"/perspectiv.v1.ModerationService/LookupUser": api.LimitSearch,
	}
	for p, limit := range verified {
		if limit != "" {
			rateLimited[p] = limit
		}
	}

	for proc, pol := range api.Routes {
		t.Run(proc, func(t *testing.T) {
			t.Parallel()
			_, wantVerified := verified[proc]
			if pol.Verified != wantVerified {
				t.Errorf("Verified = %v, want %v", pol.Verified, wantVerified)
			}
			if wantVerified && pol.Access < api.User {
				t.Errorf("verified procedure has Access %v; it needs a signed-in caller", pol.Access)
			}
			if got := pol.Access == api.Admin; got != admin[proc] {
				t.Errorf("Admin = %v, want %v", got, admin[proc])
			}
			if pol.RateLimit != rateLimited[proc] {
				t.Errorf("RateLimit = %q, want %q", pol.RateLimit, rateLimited[proc])
			}
		})
	}
}
