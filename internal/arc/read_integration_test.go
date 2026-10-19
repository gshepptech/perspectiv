//go:build integration

package arc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"perspectiv/internal/api/apiutil"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/arc"
	"perspectiv/internal/testdb"
)

func ids(sums []*perspectivv1.PerspectiveSummary) []string {
	out := make([]string, 0, len(sums))
	for _, s := range sums {
		out = append(out, s.GetId())
	}
	return out
}

func sameIDs(t *testing.T, what string, got []string, want ...uuid.UUID) {
	t.Helper()
	ws := make([]string, 0, len(want))
	for _, w := range want {
		ws = append(ws, w.String())
	}
	if strings.Join(got, ",") != strings.Join(ws, ",") {
		t.Fatalf("%s = %v, want %v", what, got, ws)
	}
}

// CT-011, FR-030, AC-030, OT-032, FR-024, FR-036: an arc page carries canon,
// then every other lore (a demoted one included), then tales, with deleted
// and hidden perspectives as placeholders.
func TestGetArc_Sections(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	creator := w.user(t)
	dayID := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "The jukebox night", CreatedBy: creator})
	t0 := time.Now().Add(-time.Hour)
	at := func(i int) time.Time { return t0.Add(time.Duration(i) * time.Minute) }

	// Canon: four linked perspectives, one of them later deleted.
	c1 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(1), body: strings.Repeat("ab", 200)})
	c2 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(2)})
	c3 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(3)})
	c4 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(4), deleted: true})
	claim := w.link(t, c1, c2)
	w.link(t, c2, c3)
	w.link(t, c3, c4)
	canon := w.lore(t, dayID, "LORE_STANDING_CANON", 3.5, c1, c2, c3, c4)
	// A demoted lore (lower rank) and a higher-ranked sublore.
	d1 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(5)})
	d2 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(6)})
	demoted := w.lore(t, dayID, "LORE_STANDING_SUBLORE", 0.5, d1, d2)
	s1 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(7)})
	s2 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE", publishAt: at(8)})
	higher := w.lore(t, dayID, "LORE_STANDING_SUBLORE", 1.5, s1, s2)
	// A lore whose group dissolved has nothing left to show.
	w.lore(t, dayID, "LORE_STANDING_SUBLORE", 9)
	// Tales and an unlinked perspective.
	solo := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_SOLO", publishAt: at(9)})
	removed := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_REMOVED", publishAt: at(10)})
	hiddenTale := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_SOLO", publishAt: at(11), hidden: true})
	pending := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_PENDING", publishAt: at(12)})
	w.exec(t, `INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, c2, creator)

	resp, err := w.client(t).GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: dayID.String()}))
	if err != nil {
		t.Fatalf("GetArc: %v", err)
	}
	got := resp.Msg

	checkDayHeader(t, w, got.GetArc(), creator, canon)

	if got.GetCanon().GetLore().GetId() != canon.String() || got.GetCanon().GetLore().GetStanding() != perspectivv1.LoreStanding_LORE_STANDING_CANON {
		t.Fatalf("canon = %v", got.GetCanon().GetLore())
	}
	sameIDs(t, "canon perspectives", ids(got.GetCanon().GetPerspectives()), c1, c2, c3, c4)
	if n := len(got.GetCanon().GetLore().GetMembers()); n != 4 {
		t.Fatalf("canon members = %d, want 4", n)
	}
	if enc := got.GetCanon().GetLore().GetEncounters(); len(enc) != 3 || enc[0].GetClaimId() != claim.String() ||
		enc[0].GetPerspectiveId() != c1.String() || enc[0].GetClaimantPerspectiveId() != c2.String() {
		t.Fatalf("canon encounters = %v", enc)
	}

	if len(got.GetSublores()) != 2 || got.GetSublores()[0].GetLore().GetId() != higher.String() ||
		got.GetSublores()[1].GetLore().GetId() != demoted.String() {
		t.Fatalf("sublores = %v, want the higher-ranked then the demoted lore", got.GetSublores())
	}
	sameIDs(t, "demoted lore perspectives", ids(got.GetSublores()[1].GetPerspectives()), d1, d2)
	sameIDs(t, "tales", ids(got.GetTales()), solo, removed, hiddenTale)
	sameIDs(t, "unlinked", ids(got.GetUnlinked()), pending)
	if len(got.GetHours()) != 0 {
		t.Fatalf("hours = %v, want none", got.GetHours())
	}

	checkSummaries(t, got, canon)
}

func checkDayHeader(t *testing.T, w *world, a *perspectivv1.Arc, creator, canon uuid.UUID) {
	t.Helper()
	if a.GetTitle() != "The jukebox night" || a.GetLocalDate() != "2026-10-16" || a.Hour != nil || a.GetParentArcId() != "" ||
		a.GetCreatedBy().GetUserId() != creator.String() || a.GetCreatedBy().GetUsername() == "" || a.GetCanonLoreId() != canon.String() {
		t.Fatalf("arc = %v", a)
	}
	var chain []string
	for _, p := range a.GetPlaceChain() {
		chain = append(chain, p.GetName())
	}
	if strings.Join(chain, " > ") != "Ohio > Columbus > Joe's Diner" || a.GetPlace().GetId() != w.diner.String() ||
		a.GetPlace().GetKind() != perspectivv1.PlaceKind_PLACE_KIND_STORE {
		t.Fatalf("place chain = %v, place = %v", chain, a.GetPlace())
	}
}

func checkSummaries(t *testing.T, got *perspectivv1.GetArcResponse, canon uuid.UUID) {
	t.Helper()
	// Live summaries carry author, excerpt and votes; the opening is cut.
	live := got.GetCanon().GetPerspectives()[0]
	if live.GetAuthor().GetUsername() == "" || live.GetPartCount() != 1 || !strings.HasSuffix(live.GetExcerpt(), "…") ||
		len([]rune(live.GetExcerpt())) != 241 || live.GetLoreId() != canon.String() || live.GetGenre() != perspectivv1.Genre_GENRE_DRAMA {
		t.Fatalf("live summary = %v", live)
	}
	if got.GetCanon().GetPerspectives()[1].GetUpvotes() != 1 {
		t.Fatalf("upvotes = %d, want 1", got.GetCanon().GetPerspectives()[1].GetUpvotes())
	}
	// Deleted and hidden perspectives are placeholders.
	for _, p := range []*perspectivv1.PerspectiveSummary{got.GetCanon().GetPerspectives()[3], got.GetTales()[2]} {
		if (!p.GetDeleted() && !p.GetHidden()) || p.GetExcerpt() != "" || p.GetAuthor() != nil || p.GetPartCount() != 0 {
			t.Fatalf("placeholder summary = %v", p)
		}
	}
}

// AC-033, OT-035, FR-011: a day arc lists its hour child arcs in ascending
// hour order, each with its own canon; an hour arc names its parent.
func TestGetArc_HourTimeline(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	in := func(h int32) arc.ResolveInput {
		return arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(h), Title: "The long night", CreatedBy: w.user(t)}
	}
	h21 := w.mustResolve(t, in(21))
	h7 := w.mustResolve(t, in(7))
	h13 := w.mustResolve(t, in(13))
	dayID := w.arcRow(t, h21).parent.UUID

	// The 13:00 hour has a canon of its own; 07:00 has perspectives but none.
	p1 := w.perspective(t, h13, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	p2 := w.perspective(t, h13, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	w.perspective(t, h13, perspectiveOpts{loreStatus: "LORE_STATUS_SOLO", deleted: true})
	hourCanon := w.lore(t, h13, "LORE_STANDING_CANON", 2, p1, p2)
	w.perspective(t, h7, perspectiveOpts{loreStatus: "LORE_STATUS_PENDING"})
	// The day itself has a canon too.
	q1 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	q2 := w.perspective(t, dayID, perspectiveOpts{loreStatus: "LORE_STATUS_IN_LORE"})
	dayCanon := w.lore(t, dayID, "LORE_STANDING_CANON", 1, q1, q2)

	client := w.client(t)
	resp, err := client.GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: dayID.String()}))
	if err != nil {
		t.Fatalf("GetArc(day): %v", err)
	}
	if resp.Msg.GetCanon().GetLore().GetId() != dayCanon.String() {
		t.Fatalf("day canon = %v", resp.Msg.GetCanon())
	}
	hours := resp.Msg.GetHours()
	var order []int32
	for _, h := range hours {
		order = append(order, h.GetArc().GetHour())
	}
	if len(order) != 3 || order[0] != 7 || order[1] != 13 || order[2] != 21 {
		t.Fatalf("hour order = %v, want [7 13 21]", order)
	}
	if hours[0].GetCanon() != nil || hours[0].GetPerspectiveCount() != 1 {
		t.Fatalf("07:00 = canon %v, count %d", hours[0].GetCanon(), hours[0].GetPerspectiveCount())
	}
	if hours[1].GetCanon().GetLore().GetId() != hourCanon.String() || hours[1].GetPerspectiveCount() != 2 {
		t.Fatalf("13:00 = canon %v, count %d", hours[1].GetCanon(), hours[1].GetPerspectiveCount())
	}
	sameIDs(t, "13:00 canon", ids(hours[1].GetCanon().GetPerspectives()), p1, p2)
	if hours[1].GetArc().GetParentArcId() != dayID.String() || hours[1].GetArc().GetTitle() != "The long night · 13:00" ||
		hours[1].GetArc().GetPlace().GetId() != w.diner.String() {
		t.Fatalf("13:00 arc = %v", hours[1].GetArc())
	}

	hr, err := client.GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: h13.String()}))
	if err != nil {
		t.Fatalf("GetArc(hour): %v", err)
	}
	if hr.Msg.GetArc().GetHour() != 13 || hr.Msg.GetArc().GetParentArcId() != dayID.String() ||
		hr.Msg.GetCanon().GetLore().GetId() != hourCanon.String() || len(hr.Msg.GetHours()) != 0 || len(hr.Msg.GetTales()) != 1 {
		t.Fatalf("hour arc page = %v", hr.Msg)
	}
}

// An arc with nothing in it yet is a page with empty sections.
func TestGetArc_EmptyArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	id := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Title: "Quiet day", CreatedBy: w.user(t)})
	resp, err := w.client(t).GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: id.String()}))
	if err != nil {
		t.Fatal(err)
	}
	m := resp.Msg
	if m.GetCanon() != nil || len(m.GetSublores())+len(m.GetTales())+len(m.GetUnlinked())+len(m.GetHours()) != 0 || m.GetArc().GetCanonLoreId() != "" {
		t.Fatalf("empty arc page = %v", m)
	}
}

func TestGetArc_Errors(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	hiddenPlace := w.place(t, "PLACE_KIND_STORE", "Shut Shop", w.city)
	hiddenArc := w.mustResolve(t, arc.ResolveInput{PlaceID: hiddenPlace, LocalDate: day, Title: "Before the fall", CreatedBy: w.user(t)})
	w.exec(t, `UPDATE places SET hidden_at = now() WHERE id = $1`, hiddenPlace)
	client := w.client(t)

	tests := []struct {
		name string
		id   string
		want connect.Code
	}{
		{name: "unknown arc (CT-011)", id: uuid.NewString(), want: connect.CodeNotFound},
		{name: "arc at a hidden place", id: hiddenArc.String(), want: connect.CodeNotFound},
		{name: "malformed id", id: "not-a-uuid", want: connect.CodeInvalidArgument},
		{name: "missing id", id: "", want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := client.GetArc(context.Background(), as(uuid.Nil, &perspectivv1.GetArcRequest{ArcId: tc.id}))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("code = %v (%v), want %v", got, err, tc.want)
			}
		})
	}
}

// Only the hour arc's creator retitles it, and only an hour arc.
func TestRetitleHourArc(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	creator, other := w.user(t), w.user(t)
	hourID := w.mustResolve(t, arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: hourPtr(22), Title: "The long night", CreatedBy: creator})
	dayID := w.arcRow(t, hourID).parent.UUID
	client := w.client(t)
	retitle := func(user, id uuid.UUID, title string) (*perspectivv1.Arc, error) {
		resp, err := client.RetitleHourArc(context.Background(), as(user, &perspectivv1.RetitleHourArcRequest{ArcId: id.String(), Title: title}))
		if err != nil {
			return nil, err
		}
		return resp.Msg.GetArc(), nil
	}

	failures := []struct {
		name  string
		user  uuid.UUID
		arc   uuid.UUID
		title string
		want  connect.Code
	}{
		{name: "signed out", arc: hourID, title: "Last orders", want: connect.CodeUnauthenticated},
		{name: "not the creator", user: other, arc: hourID, title: "Last orders", want: connect.CodePermissionDenied},
		{name: "day arc", user: creator, arc: dayID, title: "Last orders", want: connect.CodeFailedPrecondition},
		{name: "unknown arc", user: creator, arc: uuid.New(), title: "Last orders", want: connect.CodeNotFound},
		{name: "place name title", user: creator, arc: hourID, title: "Joe's Diner", want: connect.CodeInvalidArgument},
		{name: "too short", user: creator, arc: hourID, title: "no", want: connect.CodeInvalidArgument},
	}
	for _, tc := range failures {
		if _, err := retitle(tc.user, tc.arc, tc.title); codeOf(err) != tc.want {
			t.Fatalf("%s: code = %v (%v), want %v", tc.name, codeOf(err), err, tc.want)
		}
	}
	if got := w.arcRow(t, hourID).title; got != "The long night · 22:00" {
		t.Fatalf("title after refused retitles = %q", got)
	}

	a, err := retitle(creator, hourID, " Last orders ")
	if err != nil {
		t.Fatalf("retitle: %v", err)
	}
	if a.GetTitle() != "Last orders" || a.GetHour() != 22 || a.GetParentArcId() != dayID.String() || len(a.GetPlaceChain()) != 3 {
		t.Fatalf("retitled arc = %v", a)
	}
	if got := w.arcRow(t, hourID).title; got != "Last orders" {
		t.Fatalf("stored title = %q", got)
	}
	if got := w.arcRow(t, dayID).title; got != "The long night" {
		t.Fatalf("day title changed to %q", got)
	}
}

// NFR-008: every list on an arc page pages with a cursor token while the
// canon stays first, and the viewer's standing comes from the server, not
// from whichever page their perspective is on.
func TestArcPagination(t *testing.T) {
	t.Parallel()
	w := newWorld(t, testdb.New(t))
	ctx := context.Background()
	in := func(h *int32) arc.ResolveInput {
		return arc.ResolveInput{PlaceID: w.diner, LocalDate: day, Hour: h, Title: "The jukebox night", CreatedBy: w.user(t)}
	}
	dayID := w.mustResolve(t, in(nil))
	var hourIDs []string
	for _, h := range []int32{18, 9, 12} {
		hourIDs = append(hourIDs, w.mustResolve(t, in(hourPtr(h))).String())
	}
	hourIDs = []string{hourIDs[1], hourIDs[2], hourIDs[0]} // 09:00, 12:00, 18:00

	t0 := time.Now().Add(-time.Hour)
	n := 0
	next := func(status string) uuid.UUID {
		n++
		return w.perspective(t, dayID, perspectiveOpts{loreStatus: status, publishAt: t0.Add(time.Duration(n) * time.Minute)})
	}
	pair := func() []uuid.UUID { return []uuid.UUID{next("LORE_STATUS_IN_LORE"), next("LORE_STATUS_IN_LORE")} }
	canon := w.lore(t, dayID, "LORE_STANDING_CANON", 5, pair()...)
	var sublores []string
	var lowest []uuid.UUID
	for _, rank := range []float64{3, 2, 1} {
		members := pair()
		sublores = append(sublores, w.lore(t, dayID, "LORE_STANDING_SUBLORE", rank, members...).String())
		lowest = members
	}
	var tales, unlinked []string
	for range 3 {
		tales = append(tales, next("LORE_STATUS_SOLO").String())
		unlinked = append(unlinked, next("LORE_STATUS_PENDING").String())
	}
	viewer := w.id(t, `SELECT author_id FROM perspectives WHERE id = $1`, lowest[0])

	client := w.client(t)
	resp, err := client.GetArc(ctx, as(viewer, &perspectivv1.GetArcRequest{ArcId: dayID.String(), PageSize: 2}))
	if err != nil {
		t.Fatalf("GetArc: %v", err)
	}
	got := resp.Msg
	if got.GetCanon().GetLore().GetId() != canon.String() || len(got.GetCanon().GetPerspectives()) != 2 {
		t.Fatalf("canon = %v", got.GetCanon())
	}
	if v := got.GetViewer(); v.GetPerspectiveId() != lowest[0].String() || v.GetLoreId() != sublores[2] {
		t.Fatalf("viewer = %v; want their perspective in the lowest sublore, which is on page 2", v)
	}

	walk := func(section perspectivv1.ArcSection, first []string, token string) []string {
		t.Helper()
		ids := first
		for token != "" {
			r, err := client.ListArcSection(ctx, as(uuid.Nil, &perspectivv1.ListArcSectionRequest{
				ArcId: dayID.String(), Section: section, PageToken: token, PageSize: 2,
			}))
			if err != nil {
				t.Fatalf("ListArcSection(%v): %v", section, err)
			}
			for _, l := range r.Msg.GetSublores() {
				ids = append(ids, l.GetLore().GetId())
			}
			for _, p := range r.Msg.GetPerspectives() {
				ids = append(ids, p.GetId())
			}
			for _, h := range r.Msg.GetHours() {
				ids = append(ids, h.GetArc().GetId())
			}
			token = r.Msg.GetNextPageToken()
		}
		return ids
	}
	ids := func(n int, f func(i int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = f(i)
		}
		return out
	}
	eq := func(name string, got, want []string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
	if len(got.GetSublores()) != 2 || len(got.GetTales()) != 2 || len(got.GetUnlinked()) != 2 || len(got.GetHours()) != 2 {
		t.Fatalf("first pages: %d sublores, %d tales, %d unlinked, %d hours; want 2 of each",
			len(got.GetSublores()), len(got.GetTales()), len(got.GetUnlinked()), len(got.GetHours()))
	}
	eq("sublores", walk(perspectivv1.ArcSection_ARC_SECTION_SUBLORES,
		ids(2, func(i int) string { return got.GetSublores()[i].GetLore().GetId() }), got.GetSubloresNextPageToken()), sublores)
	eq("tales", walk(perspectivv1.ArcSection_ARC_SECTION_TALES,
		ids(2, func(i int) string { return got.GetTales()[i].GetId() }), got.GetTalesNextPageToken()), tales)
	eq("unlinked", walk(perspectivv1.ArcSection_ARC_SECTION_UNLINKED,
		ids(2, func(i int) string { return got.GetUnlinked()[i].GetId() }), got.GetUnlinkedNextPageToken()), unlinked)
	eq("hours", walk(perspectivv1.ArcSection_ARC_SECTION_HOURS,
		ids(2, func(i int) string { return got.GetHours()[i].GetArc().GetId() }), got.GetHoursNextPageToken()), hourIDs)

	for name, req := range map[string]*perspectivv1.ListArcSectionRequest{
		"no section":        {ArcId: dayID.String()},
		"garbage token":     {ArcId: dayID.String(), Section: perspectivv1.ArcSection_ARC_SECTION_TALES, PageToken: "garbage"},
		"another's token":   {ArcId: dayID.String(), Section: perspectivv1.ArcSection_ARC_SECTION_SUBLORES, PageToken: got.GetTalesNextPageToken()},
		"hour out of range": {ArcId: dayID.String(), Section: perspectivv1.ArcSection_ARC_SECTION_HOURS, PageToken: apiutil.NextPageToken("24")},
	} {
		if _, err := client.ListArcSection(ctx, as(uuid.Nil, req)); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: code = %v, want InvalidArgument", name, codeOf(err))
		}
	}
	hourOnly, err := client.ListArcSection(ctx, as(uuid.Nil, &perspectivv1.ListArcSectionRequest{
		ArcId: hourIDs[0], Section: perspectivv1.ArcSection_ARC_SECTION_HOURS,
	}))
	if err != nil || len(hourOnly.Msg.GetHours()) != 0 {
		t.Fatalf("hours of an hour arc = %v, %v; want an empty page", hourOnly, err)
	}
	w.exec(t, `UPDATE places SET hidden_at = now() WHERE id = $1`, w.diner)
	if _, err := client.ListArcSection(ctx, as(uuid.Nil, &perspectivv1.ListArcSectionRequest{
		ArcId: dayID.String(), Section: perspectivv1.ArcSection_ARC_SECTION_TALES,
	})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("hidden arc: code = %v, want NotFound", codeOf(err))
	}
}
