package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api"
	"perspectiv/internal/arc"
	"perspectiv/internal/lore"
	"perspectiv/internal/perspective"
	"perspectiv/internal/place"
)

// previewer names the page a shared link points at, from the same public
// reads a signed-out reader gets: a hidden, deleted or draft page has none.
type previewer struct {
	arcs         *arc.Service
	perspectives *perspective.Service
	lores        *lore.Service
	places       *place.Service
}

// preview dispatches /arcs/<id>, /lores/<id>, /perspectives/<id> and
// /places/<id>; any other path has no preview.
func (p previewer) preview(ctx context.Context, path string) (api.Preview, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return api.Preview{}, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return api.Preview{}, false
	}
	switch parts[0] {
	case "arcs":
		return p.arc(ctx, id)
	case "lores":
		return p.lore(ctx, id)
	case "perspectives":
		return p.story(ctx, id)
	case "places":
		return p.place(ctx, id)
	}
	return api.Preview{}, false
}

// day is "October 3, 2026" for "2026-10-03".
func day(localDate string) string {
	t, err := time.Parse(time.DateOnly, localDate)
	if err != nil {
		return localDate
	}
	return t.Format("January 2, 2006")
}

// where is "UCF Stadium, Orlando": the arc's place and the city above it.
func where(chain []*perspectivv1.PlaceRef) string {
	if len(chain) == 0 {
		return ""
	}
	out := chain[len(chain)-1].GetName()
	for i := len(chain) - 2; i >= 0; i-- {
		if chain[i].GetKind() == perspectivv1.PlaceKind_PLACE_KIND_CITY {
			return out + ", " + chain[i].GetName()
		}
	}
	return out
}

func quoted(excerpt string) string {
	if excerpt == "" {
		return ""
	}
	return " “" + excerpt + "”"
}

func (p previewer) arc(ctx context.Context, id uuid.UUID) (api.Preview, bool) {
	res, err := p.arcs.GetArc(ctx, uuid.Nil, id, 1)
	if err != nil {
		return api.Preview{}, false
	}
	a := res.GetArc()
	first := ""
	if c := res.GetCanon().GetPerspectives(); len(c) > 0 {
		first = c[0].GetExcerpt()
	}
	return api.Preview{
		Title:       a.GetTitle(),
		Description: fmt.Sprintf("Every side of the story from the people at %s on %s.%s", where(a.GetPlaceChain()), day(a.GetLocalDate()), quoted(first)),
	}, true
}

func (p previewer) lore(ctx context.Context, id uuid.UUID) (api.Preview, bool) {
	l, err := p.lores.GetLore(ctx, id)
	if err != nil {
		return api.Preview{}, false
	}
	arcID, err := uuid.Parse(l.GetArcId())
	if err != nil {
		return api.Preview{}, false
	}
	res, err := p.arcs.GetArc(ctx, uuid.Nil, arcID, 1)
	if err != nil {
		return api.Preview{}, false
	}
	a := res.GetArc()
	title := l.GetName()
	if title == "" {
		title = a.GetTitle()
	}
	return api.Preview{
		Title: title,
		Description: fmt.Sprintf("%d people at %s on %s who found each other in their stories.",
			l.GetMemberCount(), where(a.GetPlaceChain()), day(a.GetLocalDate())),
	}, true
}

func (p previewer) story(ctx context.Context, id uuid.UUID) (api.Preview, bool) {
	s, err := p.perspectives.Get(ctx, uuid.Nil, id)
	if err != nil || s.GetDeleted() || s.GetHidden() || s.GetStatus() != perspectivv1.PerspectiveStatus_PERSPECTIVE_STATUS_PUBLISHED {
		return api.Preview{}, false
	}
	opening := ""
	for _, part := range s.GetParts() {
		if part.GetPosition() == 1 {
			opening = part.GetBody()
		}
	}
	return api.Preview{
		Title:       fmt.Sprintf("%s · %s's side", s.GetArcTitle(), s.GetAuthor().GetUsername()),
		Description: fmt.Sprintf("At %s on %s.%s", s.GetPlace().GetName(), day(s.GetLocalDate()), quoted(opening)),
	}, true
}

func (p previewer) place(ctx context.Context, id uuid.UUID) (api.Preview, bool) {
	pl, err := p.places.Get(ctx, id)
	if err != nil {
		return api.Preview{}, false
	}
	names := make([]string, 0, len(pl.Ancestors))
	for i := len(pl.Ancestors) - 1; i >= 0; i-- {
		names = append(names, pl.Ancestors[i].Name)
	}
	in := ""
	if len(names) > 0 {
		in = " in " + strings.Join(names, ", ")
	}
	return api.Preview{
		Title:       pl.Name,
		Description: fmt.Sprintf("Stories from the people who were at %s%s, day by day.", pl.Name, in),
	}, true
}
