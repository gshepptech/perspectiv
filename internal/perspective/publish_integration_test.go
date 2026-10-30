//go:build integration

package perspective_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/proto"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
)

// AC-010, OT-015, FR-015: part count and words per part.
func TestPublishPartLimits(t *testing.T) {
	t.Parallel()
	ten := make([]string, 10)
	for i := range ten {
		ten[i] = words(300)
	}
	eleven := append(append([]string{}, ten...), "one more")
	tests := []struct {
		name  string
		parts []string
		want  connect.Code
	}{
		{name: "ten parts of 300 words publish", parts: ten},
		{name: "an eleventh part is rejected", parts: eleven, want: connect.CodeInvalidArgument},
		{name: "a 301-word part is rejected", parts: []string{words(10), words(301)}, want: connect.CodeInvalidArgument},
		{name: "no parts is rejected", parts: nil, want: connect.CodeInvalidArgument},
		{name: "a blank part is rejected", parts: []string{"fine", "  \n "}, want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			req := w.publishReq()
			req.Parts = tc.parts
			resp, err := w.client.Publish(context.Background(), as(w.user(), req))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("Publish code = %v, want %v (err %v)", got, tc.want, err)
			}
			if tc.want != 0 {
				if n := w.count(`SELECT count(*) FROM perspectives`); n != 0 {
					t.Fatalf("rejected publish left %d perspectives", n)
				}
				return
			}
			assertParts(t, w.get(uuid.Nil, resp.Msg.GetPerspectiveId()), tc.parts)
		})
	}
}

func assertParts(t *testing.T, p *perspectivv1.Perspective, want []string) {
	t.Helper()
	if len(p.GetParts()) != len(want) {
		t.Fatalf("stored %d parts, want %d", len(p.GetParts()), len(want))
	}
	for i, part := range p.GetParts() {
		if part.GetPosition() != int32(i+1) || part.GetBody() != want[i] { //nolint:gosec // at most 10 parts
			t.Fatalf("part %d has position %d, want %d with the sent body", i, part.GetPosition(), i+1)
		}
	}
}

// AC-017, FR-021: at most 10 characters.
func TestPublishCharacterLimit(t *testing.T) {
	t.Parallel()
	chars := func(n int) []*perspectivv1.CharacterInput {
		out := make([]*perspectivv1.CharacterInput, n)
		for i := range out {
			out[i] = &perspectivv1.CharacterInput{Label: "the cook " + string(rune('A'+i))}
		}
		return out
	}
	tests := []struct {
		name  string
		chars []*perspectivv1.CharacterInput
		want  connect.Code
	}{
		{name: "ten characters publish", chars: chars(10)},
		{name: "an eleventh character is rejected", chars: chars(11), want: connect.CodeInvalidArgument},
		{name: "a blank label is rejected", chars: []*perspectivv1.CharacterInput{{Label: "  "}}, want: connect.CodeInvalidArgument},
		{name: "an unknown character id is rejected", chars: []*perspectivv1.CharacterInput{{Id: uuid.NewString(), Label: "x"}}, want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			req := w.publishReq()
			req.Characters = tc.chars
			resp, err := w.client.Publish(context.Background(), as(w.user(), req))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("Publish code = %v, want %v (err %v)", got, tc.want, err)
			}
			if tc.want != 0 {
				return
			}
			p := w.get(uuid.Nil, resp.Msg.GetPerspectiveId())
			if len(p.GetCharacters()) != len(tc.chars) {
				t.Fatalf("stored %d characters, want %d", len(p.GetCharacters()), len(tc.chars))
			}
			for i, c := range p.GetCharacters() {
				if c.GetPosition() != int32(i+1) || c.GetLabel() != tc.chars[i].GetLabel() || c.GetId() == "" {
					t.Fatalf("character %d = %+v", i, c)
				}
			}
		})
	}
}

// AC-011, CT-005: the first publish needs a valid title and creates the arc.
func TestPublishNewArcTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, title string
		want        connect.Code
	}{
		{name: "missing", title: "", want: connect.CodeInvalidArgument},
		{name: "too short", title: "ab", want: connect.CodeInvalidArgument},
		{name: "too long", title: strings.Repeat("a", 81), want: connect.CodeInvalidArgument},
		{name: "the place name", title: "Joe's Diner", want: connect.CodeInvalidArgument},
		{name: "the date", title: day, want: connect.CodeInvalidArgument},
		{name: "place and date", title: "Joe's Diner " + day, want: connect.CodeInvalidArgument},
		{name: "a real title", title: "Pancakes at midnight"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			req := w.publishReq()
			req.TitleIfNewArc = tc.title
			_, err := w.client.Publish(context.Background(), as(w.user(), req))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("Publish code = %v, want %v (err %v)", got, tc.want, err)
			}
			wantArcs := 0
			if tc.want == 0 {
				wantArcs = 1
			}
			if n := w.count(`SELECT count(*) FROM arcs`); n != wantArcs {
				t.Fatalf("arcs = %d, want %d", n, wantArcs)
			}
		})
	}
}

// AC-011, OT-012: a second user at the same place and day joins the arc
// without a title.
func TestPublishJoinsArcWithoutTitle(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	first := w.publish(w.user(), w.publishReq())
	req := w.publishReq()
	req.TitleIfNewArc = ""
	second := w.publish(w.user(), req)
	if second.GetArcId() != first.GetArcId() {
		t.Fatalf("second arc %s, want the first's %s", second.GetArcId(), first.GetArcId())
	}
	if n := w.count(`SELECT count(*) FROM arcs`); n != 1 {
		t.Fatalf("arcs = %d, want 1", n)
	}
	if p := w.get(uuid.Nil, second.GetPerspectiveId()); p.GetArcTitle() != "The night the fryer caught fire" {
		t.Fatalf("arc title = %q", p.GetArcTitle())
	}
}

// OT-012: concurrent first publishes produce exactly one day arc.
func TestPublishConcurrentFirstWriters(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	const n = 6
	users := make([]uuid.UUID, n)
	for i := range users {
		users[i] = w.user()
	}
	arcs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			resp, err := w.client.Publish(context.Background(), as(users[i], w.publishReq()))
			errs[i] = err
			if err == nil {
				arcs[i] = resp.Msg.GetArcId()
			}
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
		if arcs[i] != arcs[0] {
			t.Fatalf("publish %d went to arc %s, want %s", i, arcs[i], arcs[0])
		}
	}
	if got := w.count(`SELECT count(*) FROM arcs`); got != 1 {
		t.Fatalf("arcs = %d, want 1", got)
	}
}

// AC-012, OT-016, FR-016, FR-060: one perspective per user per arc.
func TestPublishSecondInArcAlreadyExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	first := w.publish(me, w.publishReq())

	_, err := w.client.Publish(ctx, as(me, w.publishReq()))
	if codeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("second publish code = %v, want AlreadyExists (err %v)", codeOf(err), err)
	}
	if n := w.count(`SELECT count(*) FROM perspectives WHERE author_id = $1`, me); n != 1 {
		t.Fatalf("author has %d perspectives, want 1", n)
	}

	// The composer then finds the existing perspective for that place and day.
	list, err := w.client.ListMine(ctx, as(me, &perspectivv1.ListMineRequest{
		Status:    perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED,
		PlaceId:   w.place.String(),
		LocalDate: day,
	}))
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	if got := list.Msg.GetPerspectives(); len(got) != 1 || got[0].GetId() != first.GetPerspectiveId() {
		t.Fatalf("ListMine = %v, want only %s", got, first.GetPerspectiveId())
	}

	// Republishing the same draft id is the same arc again.
	_, err = w.client.Publish(ctx, as(me, func() *perspectivv1.PublishRequest {
		r := w.publishReq()
		r.PerspectiveId = first.GetPerspectiveId()
		return r
	}()))
	if codeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("publish of a published id code = %v, want AlreadyExists", codeOf(err))
	}

	// A different hour is a different arc, so it publishes.
	hourly := w.publishReq()
	hourly.Hour = hourPtr(9)
	w.publish(me, hourly)
}

// A member who deleted their story in an arc may write there again; the
// deleted one stays a removed placeholder. A story a moderator hid still
// holds the place.
func TestPublishAfterDeleteInSameArc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	first := w.publish(me, w.publishReq())
	if _, err := w.client.Delete(ctx, as(me, &perspectivv1.PerspectiveServiceDeleteRequest{PerspectiveId: first.GetPerspectiveId()})); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	second := w.publish(me, w.publishReq())
	if second.GetArcId() != first.GetArcId() || second.GetPerspectiveId() == first.GetPerspectiveId() {
		t.Fatalf("second story = %v, want a new one in the same arc", second)
	}
	if n := w.count(`SELECT count(*) FROM perspectives WHERE arc_id = $1 AND author_id = $2`, first.GetArcId(), me); n != 2 {
		t.Fatalf("rows in the arc = %d, want the deleted one and the new one", n)
	}

	hidden := w.user()
	h := w.publish(hidden, w.publishReq())
	w.exec(`UPDATE perspectives SET hidden_at = now() WHERE id = $1`, h.GetPerspectiveId())
	_, err := w.client.Publish(ctx, as(hidden, w.publishReq()))
	if codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("publish after a moderator's hide = %v, want FailedPrecondition", codeOf(err))
	}
}

// AC-013, OT-017, FR-012: an hour files under the hour child arc.
func TestPublishHourArc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	req := w.publishReq()
	req.Hour = hourPtr(15)
	resp := w.publish(me, req)

	var hour *int16
	var parent *uuid.UUID
	if err := w.pool.QueryRow(ctx, `SELECT hour, parent_arc_id FROM arcs WHERE id = $1`, resp.GetArcId()).Scan(&hour, &parent); err != nil {
		t.Fatalf("read arc: %v", err)
	}
	if hour == nil || *hour != 15 || parent == nil {
		t.Fatalf("perspective arc hour=%v parent=%v, want the hour-15 child", hour, parent)
	}
	if n := w.count(`SELECT count(*) FROM perspectives WHERE arc_id = $1`, *parent); n != 0 {
		t.Fatalf("day arc holds %d perspectives directly, want 0", n)
	}

	// Update carries no hour; after an edit the hour is still 15.
	if _, err := w.client.Update(ctx, as(me, &perspectivv1.PerspectiveServiceUpdateRequest{
		PerspectiveId: resp.GetPerspectiveId(), Genre: perspectivv1.Genre_GENRE_COMEDY, Parts: []string{"Edited."},
	})); err != nil {
		t.Fatalf("Update: %v", err)
	}
	p := w.get(me, resp.GetPerspectiveId())
	if p.Hour == nil || p.GetHour() != 15 || p.GetArcId() != resp.GetArcId() {
		t.Fatalf("after update hour=%v arc=%s, want 15 in %s", p.Hour, p.GetArcId(), resp.GetArcId())
	}

	bad := w.publishReq()
	bad.Hour = hourPtr(24)
	if _, err := w.client.Publish(ctx, as(w.user(), bad)); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("hour 24 code = %v, want InvalidArgument", codeOf(err))
	}
}

// AC-016, OT-014, FR-009: the picked local date is what is stored.
func TestPublishKeepsPickedDate(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	req := w.publishReq()
	req.LocalDate = yesterday
	resp := w.publish(w.user(), req)
	p := w.get(uuid.Nil, resp.GetPerspectiveId())
	if p.GetLocalDate() != yesterday {
		t.Fatalf("local date = %s, want %s", p.GetLocalDate(), yesterday)
	}
	var stored string
	if err := w.pool.QueryRow(context.Background(), `SELECT local_date::text FROM arcs WHERE id = $1`, resp.GetArcId()).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != yesterday {
		t.Fatalf("arc local_date = %s, want %s", stored, yesterday)
	}

	for _, bad := range []string{"", "2026-13-01", "15/10/2026"} {
		r := w.publishReq()
		r.LocalDate = bad
		if _, err := w.client.Publish(context.Background(), as(w.user(), r)); codeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("date %q code = %v, want InvalidArgument", bad, codeOf(err))
		}
	}
}

// AC-015, FR-017: exactly one genre from the enum.
func TestPublishGenre(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		genre perspectivv1.Genre
		want  connect.Code
	}{
		{name: "slice of life is stored", genre: perspectivv1.Genre_GENRE_SLICE_OF_LIFE},
		{name: "unspecified is rejected", genre: perspectivv1.Genre_GENRE_UNSPECIFIED, want: connect.CodeInvalidArgument},
		{name: "out of range is rejected", genre: perspectivv1.Genre(99), want: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			req := w.publishReq()
			req.Genre = tc.genre
			resp, err := w.client.Publish(context.Background(), as(w.user(), req))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("code = %v, want %v (err %v)", got, tc.want, err)
			}
			if tc.want == 0 {
				if g := w.get(uuid.Nil, resp.Msg.GetPerspectiveId()).GetGenre(); g != tc.genre {
					t.Fatalf("genre = %v, want %v", g, tc.genre)
				}
			}
		})
	}
}

// CT-005, ST-001: an unverified or signed-out caller cannot publish; an
// unknown place is NotFound.
func TestPublishGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	if _, err := w.client.Publish(ctx, as(w.unverifiedUser(), w.publishReq())); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("unverified code = %v, want FailedPrecondition", codeOf(err))
	}
	if _, err := w.client.Publish(ctx, as(uuid.Nil, w.publishReq())); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("signed out code = %v, want Unauthenticated", codeOf(err))
	}
	req := w.publishReq()
	req.PlaceId = uuid.NewString()
	if _, err := w.client.Publish(ctx, as(w.user(), req)); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown place code = %v, want NotFound", codeOf(err))
	}
	if n := w.count(`SELECT count(*) FROM perspectives`); n != 0 {
		t.Fatalf("refused publishes left %d perspectives", n)
	}
}

// FR-047, NFR-009: publish enqueues the expiry job and the new-perspective
// event in its transaction and counts the publish.
func TestPublishSideEffects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()
	before := testutil.ToFloat64(w.metrics.PerspectivesPublished)
	resp := w.publish(me, w.publishReq())
	if got := testutil.ToFloat64(w.metrics.PerspectivesPublished) - before; got != 1 {
		t.Fatalf("perspectives_published_total rose by %v, want 1", got)
	}

	var publishedAt, scheduledAt time.Time
	if err := w.pool.QueryRow(ctx, `SELECT published_at FROM perspectives WHERE id = $1`, resp.GetPerspectiveId()).Scan(&publishedAt); err != nil {
		t.Fatal(err)
	}
	if err := w.pool.QueryRow(ctx, `SELECT scheduled_at FROM river_job
		WHERE kind = 'perspective_expiry' AND args ->> 'PerspectiveID' = $1`, resp.GetPerspectiveId()).Scan(&scheduledAt); err != nil {
		t.Fatalf("expiry job: %v", err)
	}
	if d := scheduledAt.Sub(publishedAt); d < expiryWindow-time.Second || d > expiryWindow+time.Second {
		t.Fatalf("expiry scheduled %v after publish, want %v", d, expiryWindow)
	}
	for _, kind := range []perspectivv1.NotificationKind{
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_PERSPECTIVE_IN_ARC,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_AT_FOLLOWED_PLACE,
		perspectivv1.NotificationKind_NOTIFICATION_KIND_NEW_STORY_BY_FOLLOWED_WRITER,
	} {
		if n := w.count(`SELECT count(*) FROM river_job WHERE kind = 'notify'
			AND args ->> 'PerspectiveID' = $1 AND (args ->> 'Kind')::int = $2`,
			resp.GetPerspectiveId(), int(kind)); n != 1 {
			t.Fatalf("%s notify jobs = %d, want 1", kind, n)
		}
	}

	// A refused publish enqueues nothing and counts nothing.
	if _, err := w.client.Publish(ctx, as(me, w.publishReq())); codeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("second publish code = %v", codeOf(err))
	}
	if n := w.count(`SELECT count(*) FROM river_job WHERE kind = 'perspective_expiry'`); n != 1 {
		t.Fatalf("expiry jobs = %d, want 1", n)
	}
	if got := testutil.ToFloat64(w.metrics.PerspectivesPublished) - before; got != 1 {
		t.Fatalf("counter after a refused publish rose by %v, want 1", got)
	}
}

// ST-001, AC-014, FR-018, FR-044: drafts autosave and publish is explicit.
func TestDraftThenPublish(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newWorld(t)
	me := w.user()

	saved, err := w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{
		PlaceId: w.place.String(), LocalDate: day, Parts: []string{"First line"},
		Characters: []*perspectivv1.CharacterInput{{Label: "the cook"}},
	}))
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	d := saved.Msg.GetPerspective()
	if d.GetStatus() != perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_DRAFT || d.GetArcId() != "" || d.GetId() == "" {
		t.Fatalf("draft = %+v", d)
	}
	if n := w.count(`SELECT count(*) FROM arcs`); n != 0 {
		t.Fatalf("a draft created %d arcs", n)
	}
	// A draft is its author's alone.
	if _, err := w.client.Get(ctx, as(w.user(), &perspectivv1.PerspectiveServiceGetRequest{PerspectiveId: d.GetId()})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("someone else's draft code = %v, want NotFound", codeOf(err))
	}
	if _, err := w.client.Get(ctx, as(uuid.Nil, &perspectivv1.PerspectiveServiceGetRequest{PerspectiveId: d.GetId()})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("signed-out draft code = %v, want NotFound", codeOf(err))
	}

	// Autosave again: same id, new text, the character kept by id.
	cook := d.GetCharacters()[0].GetId()
	saved, err = w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{
		PerspectiveId: d.GetId(), PlaceId: w.place.String(), LocalDate: day, Hour: hourPtr(20),
		TitleIfNewArc: "Pancakes at midnight", Genre: perspectivv1.Genre_GENRE_MYSTERY,
		Parts:      []string{"First line", "Second line"},
		Characters: []*perspectivv1.CharacterInput{{Id: cook, Label: "the night cook"}, {Label: "the cashier"}},
	}))
	if err != nil {
		t.Fatalf("SaveDraft again: %v", err)
	}
	d = saved.Msg.GetPerspective()
	if len(d.GetParts()) != 2 || d.GetTitleIfNewArc() != "Pancakes at midnight" || d.GetHour() != 20 ||
		d.GetCharacters()[0].GetId() != cook || d.GetCharacters()[0].GetLabel() != "the night cook" || len(d.GetCharacters()) != 2 {
		t.Fatalf("second save = %+v", d)
	}
	if n := w.count(`SELECT count(*) FROM perspectives WHERE author_id = $1`, me); n != 1 {
		t.Fatalf("autosave made %d rows, want 1", n)
	}

	// Publish the draft by id.
	resp := w.publish(me, &perspectivv1.PublishRequest{
		PerspectiveId: d.GetId(), PlaceId: w.place.String(), LocalDate: day, Hour: hourPtr(20),
		TitleIfNewArc: "Pancakes at midnight", Genre: perspectivv1.Genre_GENRE_MYSTERY,
		Parts:      []string{"Final first", "Final second"},
		Characters: []*perspectivv1.CharacterInput{{Id: cook, Label: "the night cook"}},
	})
	if resp.GetPerspectiveId() != d.GetId() {
		t.Fatalf("published id %s, want the draft's %s", resp.GetPerspectiveId(), d.GetId())
	}
	p := w.get(uuid.Nil, d.GetId())
	if p.GetStatus() != perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED ||
		p.GetLoreStatus() != perspectivv1.LoreStatus_LORE_STATUS_PENDING || p.GetArcId() != resp.GetArcId() ||
		p.GetParts()[0].GetBody() != "Final first" || len(p.GetCharacters()) != 1 || p.GetCharacters()[0].GetId() != cook ||
		p.GetAuthor().GetUserId() != me.String() || p.GetPublishedAt() == nil {
		t.Fatalf("published = %+v", p)
	}

	// A published perspective is no longer a draft.
	if _, err := w.client.SaveDraft(ctx, as(me, &perspectivv1.SaveDraftRequest{PerspectiveId: d.GetId(), Parts: []string{"x"}})); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("SaveDraft on published code = %v, want FailedPrecondition", codeOf(err))
	}
	// Someone else's draft id cannot be published.
	other, err := w.client.SaveDraft(ctx, as(w.user(), &perspectivv1.SaveDraftRequest{Parts: []string{"mine"}}))
	if err != nil {
		t.Fatal(err)
	}
	stolen := w.publishReq()
	stolen.PerspectiveId = other.Msg.GetPerspective().GetId()
	if _, err := w.client.Publish(ctx, as(w.user(), stolen)); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("publishing another's draft code = %v, want NotFound", codeOf(err))
	}
}

// Drafts enforce the same part, word and character limits as publish.
func TestDraftLimits(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	me := w.user()
	tests := []struct {
		name string
		req  *perspectivv1.SaveDraftRequest
		want connect.Code
	}{
		{name: "empty draft saves", req: &perspectivv1.SaveDraftRequest{}},
		{name: "301 words", req: &perspectivv1.SaveDraftRequest{Parts: []string{words(301)}}, want: connect.CodeInvalidArgument},
		{name: "eleven parts", req: &perspectivv1.SaveDraftRequest{Parts: make([]string, 11)}, want: connect.CodeInvalidArgument},
		{name: "bad date", req: &perspectivv1.SaveDraftRequest{LocalDate: "tomorrow"}, want: connect.CodeInvalidArgument},
		{name: "bad place", req: &perspectivv1.SaveDraftRequest{PlaceId: "nope"}, want: connect.CodeInvalidArgument},
		{name: "unknown place", req: &perspectivv1.SaveDraftRequest{PlaceId: uuid.NewString()}, want: connect.CodeNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := w.client.SaveDraft(context.Background(), as(me, tc.req))
			if got := codeOf(err); got != tc.want {
				t.Fatalf("code = %v, want %v (err %v)", got, tc.want, err)
			}
		})
	}
}

// The Get response for a published perspective carries vote and react totals.
func TestGetCounts(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	me, reader := w.user(), w.user()
	resp := w.publish(me, w.publishReq())
	w.exec(`INSERT INTO votes (perspective_id, user_id) VALUES ($1, $2)`, resp.GetPerspectiveId(), reader)
	w.exec(`INSERT INTO reacts (perspective_id, user_id, emoji) VALUES ($1, $2, 'EMOJI_FIRE'), ($1, $3, 'EMOJI_FIRE')`,
		resp.GetPerspectiveId(), reader, me)
	p := w.get(reader, resp.GetPerspectiveId())
	want := []*perspectivv1.ReactCount{{Emoji: perspectivv1.Emoji_EMOJI_FIRE, Count: 2, Mine: true}}
	if p.GetUpvotes() != 1 || !p.GetVoted() || len(p.GetReacts()) != 1 || !proto.Equal(p.GetReacts()[0], want[0]) {
		t.Fatalf("counts = upvotes %d voted %v reacts %v", p.GetUpvotes(), p.GetVoted(), p.GetReacts())
	}
	if anon := w.get(uuid.Nil, resp.GetPerspectiveId()); anon.GetVoted() || anon.GetReacts()[0].GetMine() {
		t.Fatalf("signed-out view marks voted=%v mine=%v", anon.GetVoted(), anon.GetReacts()[0].GetMine())
	}
}
