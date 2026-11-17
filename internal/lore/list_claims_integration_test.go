//go:build integration

package lore_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
)

// claimsWorld has alice with two characters: bob's PENDING claim on the
// first, carol's REJECTED claim on the second (made later), and alice's own
// PENDING claim on bob's character.
type claimsWorld struct {
	*world
	alice, bob                 author
	onFirst, onSecond, byAlice string
}

func newClaimsWorld(t *testing.T) *claimsWorld {
	t.Helper()
	w := newWorld(t)
	cw := &claimsWorld{world: w, alice: w.author("alice"), bob: w.author("bob")}
	carol := w.author("carol")
	second := w.character(cw.alice.perspective, "the cook")
	cw.onFirst = w.mustClaim(cw.bob.user, cw.alice.character)
	cw.onSecond = w.mustClaim(carol.user, second)
	if _, err := w.resolve(cw.alice.user, cw.onSecond, false); err != nil {
		t.Fatalf("reject: %v", err)
	}
	cw.byAlice = w.mustClaim(cw.alice.user, cw.bob.character)
	return cw
}

func (w *world) listClaims(
	caller uuid.UUID, dir perspectivv1.ClaimDirection, status perspectivv1.ClaimStatus, token string, size int32,
) (*perspectivv1.ListClaimsResponse, error) {
	w.t.Helper()
	resp, err := w.client.ListClaims(context.Background(), asUser(caller, &perspectivv1.ListClaimsRequest{
		Direction: dir, Status: status, PageToken: token, PageSize: size,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func claimIDs(r *perspectivv1.ListClaimsResponse) []string {
	var out []string
	for _, cl := range r.GetClaims() {
		out = append(out, cl.GetId())
	}
	return out
}

func sameIDs(got []string, want ...string) bool {
	return slices.Equal(got, want)
}

// Claims show usernames only, never an email.
func TestListClaimsIncoming(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	in, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING, 0, "", 0)
	if err != nil {
		t.Fatalf("incoming: %v", err)
	}
	if got := claimIDs(in); !sameIDs(got, w.onSecond, w.onFirst) {
		t.Fatalf("incoming = %v, want newest first [%s %s]", got, w.onSecond, w.onFirst)
	}
	first := in.GetClaims()[1]
	want := &perspectivv1.Claim{
		Id:                    w.onFirst,
		ArcId:                 w.arc.String(),
		CharacterId:           w.alice.character.String(),
		CharacterLabel:        "the one who alice saw",
		PerspectiveId:         w.alice.perspective.String(),
		Author:                &perspectivv1.UserRef{UserId: w.alice.user.String(), Username: "alice"},
		Claimant:              &perspectivv1.UserRef{UserId: w.bob.user.String(), Username: "bob"},
		ClaimantPerspectiveId: w.bob.perspective.String(),
		Status:                perspectivv1.ClaimStatus_CLAIM_STATUS_PENDING,
		CreatedAt:             first.GetCreatedAt(),
		PlaceName:             "Ohio",
		LocalDate:             "2026-10-01",
	}
	if first.GetCreatedAt() == nil || !proto.Equal(first, want) {
		t.Errorf("incoming claim = %v, want %v", first, want)
	}
	if in.GetClaims()[0].GetResolvedAt() == nil {
		t.Error("rejected claim has no resolved_at")
	}
	raw, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "@") {
		t.Errorf("claim listing leaks an email: %s", raw)
	}
}

func TestListClaimsFiltersAndDirections(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	rejected, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING,
		perspectivv1.ClaimStatus_CLAIM_STATUS_REJECTED, "", 0)
	if err != nil {
		t.Fatalf("incoming rejected: %v", err)
	}
	if got := claimIDs(rejected); !sameIDs(got, w.onSecond) {
		t.Errorf("incoming REJECTED = %v, want [%s]", got, w.onSecond)
	}
	out, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING, 0, "", 0)
	if err != nil {
		t.Fatalf("outgoing: %v", err)
	}
	if got := claimIDs(out); !sameIDs(got, w.byAlice) {
		t.Errorf("outgoing = %v, want [%s]", got, w.byAlice)
	}
	bobOut, err := w.listClaims(w.bob.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_OUTGOING,
		perspectivv1.ClaimStatus_CLAIM_STATUS_ACCEPTED, "", 0)
	if err != nil {
		t.Fatalf("bob outgoing accepted: %v", err)
	}
	if got := claimIDs(bobOut); len(got) != 0 {
		t.Errorf("bob outgoing ACCEPTED = %v, want none", got)
	}
}

func TestListClaimsPaging(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	var seen []string
	token := ""
	for range 3 {
		page, err := w.listClaims(w.alice.user, perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING, 0, token, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		seen = append(seen, claimIDs(page)...)
		token = page.GetNextPageToken()
		if token == "" {
			break
		}
	}
	if !sameIDs(seen, w.onSecond, w.onFirst) || token != "" {
		t.Errorf("paged incoming = %v (next %q), want [%s %s] then no token", seen, token, w.onSecond, w.onFirst)
	}
}

func TestListClaimsRefusals(t *testing.T) {
	t.Parallel()
	w := newClaimsWorld(t)
	incoming := perspectivv1.ClaimDirection_CLAIM_DIRECTION_INCOMING
	cases := []struct {
		name   string
		caller uuid.UUID
		dir    perspectivv1.ClaimDirection
		status perspectivv1.ClaimStatus
		token  string
		code   connect.Code
	}{
		{name: "no direction", caller: w.alice.user, code: connect.CodeInvalidArgument},
		{name: "unknown direction", caller: w.alice.user, dir: perspectivv1.ClaimDirection(99), code: connect.CodeInvalidArgument},
		{name: "unknown status", caller: w.alice.user, dir: incoming, status: perspectivv1.ClaimStatus(42), code: connect.CodeInvalidArgument},
		{name: "bad page token", caller: w.alice.user, dir: incoming, token: "!!", code: connect.CodeInvalidArgument},
		{name: "keyset without a separator", caller: w.alice.user, dir: incoming, token: apiutil.NextPageToken("garbage"), code: connect.CodeInvalidArgument},
		{name: "keyset with a bad time", caller: w.alice.user, dir: incoming, token: apiutil.NextPageToken("x_" + uuid.NewString()), code: connect.CodeInvalidArgument},
		{name: "keyset with a bad id", caller: w.alice.user, dir: incoming, token: apiutil.NextPageToken("12_nope"), code: connect.CodeInvalidArgument},
		{name: "signed-out", dir: incoming, code: connect.CodeUnauthenticated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := w.listClaims(tc.caller, tc.dir, tc.status, tc.token, 0)
			wantCode(t, err, tc.code)
		})
	}
}
