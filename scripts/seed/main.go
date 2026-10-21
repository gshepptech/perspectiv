// Command seed writes launch stories into a Perspectiv database: a handful
// of arcs at real places, each a few perspectives by seed accounts, linked
// by claims into lore and up-voted so the rank job can make canon.
//
// Every write goes through the same handlers and services the server
// mounts, as the seed account would make it, so the data obeys every rule a
// real user's does. Seed accounts have an email at seedDomain, which is how
// to find or remove them later:
//
//	SELECT id, username FROM users WHERE email LIKE '%@seed.perspectiv.local';
//
// Seed accounts get a random password nobody knows and every email toggle
// off, so seeding never sends mail to an address that does not exist.
//
// Run it with make seed (DATABASE_URL from .env.dev). Running it again adds
// only what is missing: perspectives, claims and thread posts already there
// are kept, so new content for an arc lands in the arc seeded before.
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/api/apiutil"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/arc"
	"perspectiv/internal/auth"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/perspective"
	"perspectiv/internal/react"
	"perspectiv/internal/vote"
)

const seedDomain = "seed.perspectiv.local"

//go:embed content.json
var contentJSON []byte

type content struct {
	Arcs []seedArc `json:"arcs"`
}

type seedArc struct {
	Place        string            `json:"place"`
	City         string            `json:"city"`
	Date         string            `json:"date"`
	Hour         *int32            `json:"hour"`
	Title        string            `json:"title"`
	Perspectives []seedPerspective `json:"perspectives"`
	Claims       []seedClaim       `json:"claims"`
	Posts        []seedPost        `json:"posts"`
}

type seedPerspective struct {
	User       string   `json:"user"`
	Genre      string   `json:"genre"`
	Parts      []string `json:"parts"`
	Characters []string `json:"characters"`
	// Solo marks a tale nobody linked to; its expiry window has long passed.
	Solo bool `json:"solo"`
}

// seedPost is one message in the thread of the lore that InLoreOf's
// perspective belongs to.
type seedPost struct {
	InLoreOf string `json:"in_lore_of"`
	User     string `json:"user"`
	Body     string `json:"body"`
}

type seedClaim struct {
	Claimant  string `json:"claimant"`
	Author    string `json:"author"`
	Character string `json:"character"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

// app holds the handlers seeding calls, built as the server builds them.
type app struct {
	pool        *pgxpool.Pool
	perspective *perspective.Handler
	lore        *lore.Handler
	vote        *vote.Handler
	react       *react.Handler
	users       map[string]uuid.UUID
}

func run(ctx context.Context) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is not set")
	}
	var c content
	if err := json.Unmarshal(contentJSON, &c); err != nil {
		return fmt.Errorf("read content: %w", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	m := metrics.New(prometheus.NewRegistry())
	inserter, err := jobs.NewClient(pool, zerolog.Nop(), m, nil, nil)
	if err != nil {
		return fmt.Errorf("job client: %w", err)
	}
	enq := jobs.NewEnqueuer(inserter)
	loreSvc := lore.NewService(pool, enq, m)
	a := &app{
		pool:        pool,
		perspective: perspective.NewHandler(perspective.NewService(pool, arc.NewService(pool), loreSvc, enq, m, 168*time.Hour)),
		lore:        lore.NewHandler(loreSvc),
		vote:        vote.NewHandler(vote.NewService(pool, loreSvc, enq)),
		react:       react.NewHandler(react.NewService(pool)),
		users:       map[string]uuid.UUID{},
	}
	for _, sa := range c.Arcs {
		wrote, err := a.seedArc(ctx, sa)
		if err != nil {
			return fmt.Errorf("arc %q: %w", sa.Title, err)
		}
		if wrote > 0 {
			fmt.Printf("seeded  %s, %s: %q (%d new perspectives)\n", sa.Place, sa.City, sa.Title, wrote)
		} else {
			fmt.Printf("skipped %s, %s: %q is already there\n", sa.Place, sa.City, sa.Title)
		}
	}
	return nil
}

// as returns ctx carrying user as the signed-in caller, the identity the
// authentication interceptor gives a handler.
func (a *app) as(ctx context.Context, user string) context.Context {
	return apiutil.WithCaller(ctx, apiutil.Caller{UserID: a.users[user], Role: perspectivv1.Role_ROLE_USER, SessionID: uuid.New()})
}

func (a *app) seedArc(ctx context.Context, sa seedArc) (int, error) {
	var placeID uuid.UUID
	err := a.pool.QueryRow(ctx, `SELECT p.id FROM places p JOIN places c ON c.id = p.parent_id
		WHERE p.name = $1 AND c.name = $2 AND p.hidden_at IS NULL`, sa.Place, sa.City).Scan(&placeID)
	if err != nil {
		return 0, fmt.Errorf("find place %s, %s (import the place data first): %w", sa.Place, sa.City, err)
	}
	day, err := time.Parse(time.DateOnly, sa.Date)
	if err != nil {
		return 0, fmt.Errorf("date: %w", err)
	}
	ids, arcID, added, err := a.publishAll(ctx, sa, placeID, day)
	if err != nil || added == 0 {
		return 0, err
	}
	if err := a.link(ctx, sa, ids); err != nil {
		return 0, err
	}
	if err := a.voteAndReact(ctx, sa, ids); err != nil {
		return 0, err
	}
	if err := a.markTales(ctx, sa, ids); err != nil {
		return 0, err
	}
	if err := a.post(ctx, sa, ids, day); err != nil {
		return 0, err
	}
	// Let the next rank pass see the arc, whatever the claims changed.
	if _, err := a.pool.Exec(ctx, `UPDATE arcs SET lore_dirty_at = now() WHERE id = $1`, arcID); err != nil {
		return 0, fmt.Errorf("mark arc for ranking: %w", err)
	}
	return added, nil
}

// publishAll publishes the arc's perspectives in order, the first creating
// the arc, and returns every perspective's id by writer. A writer who already
// has a perspective in the arc keeps it, so content added to an arc that was
// seeded before lands in the same arc; added counts the new ones.
func (a *app) publishAll(ctx context.Context, sa seedArc, placeID uuid.UUID, day time.Time) (map[string]string, string, int, error) {
	ids := map[string]string{}
	var arcID string
	added := 0
	for i, sp := range sa.Perspectives {
		if err := a.ensureUser(ctx, sp.User); err != nil {
			return nil, "", 0, err
		}
		var existing, existingArc uuid.UUID
		err := a.pool.QueryRow(ctx, `SELECT x.id, x.arc_id FROM perspectives x JOIN arcs a ON a.id = x.arc_id
			WHERE a.place_id = $1 AND a.local_date = $2 AND a.hour IS NOT DISTINCT FROM $3
			AND x.author_id = $4 AND x.deleted_at IS NULL`, placeID, sa.Date, sa.Hour, a.users[sp.User]).Scan(&existing, &existingArc)
		if err == nil {
			ids[sp.User], arcID = existing.String(), existingArc.String()
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, "", 0, fmt.Errorf("find %s's perspective: %w", sp.User, err)
		}
		req := &perspectivv1.PublishRequest{
			PlaceId: placeID.String(), LocalDate: sa.Date, Hour: sa.Hour,
			Genre: perspectivv1.Genre(perspectivv1.Genre_value["GENRE_"+sp.Genre]), Parts: sp.Parts,
		}
		if i == 0 {
			req.TitleIfNewArc = sa.Title
		}
		for _, label := range sp.Characters {
			req.Characters = append(req.Characters, &perspectivv1.CharacterInput{Label: label})
		}
		res, err := a.perspective.Publish(a.as(ctx, sp.User), connect.NewRequest(req))
		if err != nil {
			return nil, "", 0, fmt.Errorf("publish as %s: %w", sp.User, err)
		}
		ids[sp.User], arcID = res.Msg.GetPerspectiveId(), res.Msg.GetArcId()
		added++
		if err := a.backdate(ctx, res.Msg.GetPerspectiveId(), day, sa.Hour, i); err != nil {
			return nil, "", 0, err
		}
	}
	return ids, arcID, added, nil
}

// post writes the thread messages, each once, dated the day after the arc so
// the thread reads as people finding each other afterwards.
func (a *app) post(ctx context.Context, sa seedArc, ids map[string]string, day time.Time) error {
	for i, sp := range sa.Posts {
		var loreID uuid.UUID
		if err := a.pool.QueryRow(ctx, `SELECT lore_id FROM perspectives WHERE id = $1 AND lore_id IS NOT NULL`, ids[sp.InLoreOf]).Scan(&loreID); err != nil {
			return fmt.Errorf("lore of %s: %w", sp.InLoreOf, err)
		}
		var exists bool
		if err := a.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM lore_posts WHERE lore_id = $1 AND author_id = $2 AND body = $3)`,
			loreID, a.users[sp.User], sp.Body).Scan(&exists); err != nil {
			return fmt.Errorf("find post: %w", err)
		}
		if exists {
			continue
		}
		res, err := a.lore.AddLorePost(a.as(ctx, sp.User), connect.NewRequest(&perspectivv1.AddLorePostRequest{LoreId: loreID.String(), Body: sp.Body}))
		if err != nil {
			return fmt.Errorf("%s posts in %s's lore: %w", sp.User, sp.InLoreOf, err)
		}
		at := day.Add(24*time.Hour + time.Duration(9+i)*time.Hour + time.Duration(7*i)*time.Minute)
		if _, err := a.pool.Exec(ctx, `UPDATE lore_posts SET created_at = $2 WHERE id = $1`, res.Msg.GetPost().GetId(), at); err != nil {
			return fmt.Errorf("backdate post: %w", err)
		}
	}
	return nil
}

// markTales makes the perspectives the content marks solo into tales, as the
// expiry job would have once their window passed unclaimed.
func (a *app) markTales(ctx context.Context, sa seedArc, ids map[string]string) error {
	for _, sp := range sa.Perspectives {
		if !sp.Solo {
			continue
		}
		if _, err := a.pool.Exec(ctx, `UPDATE perspectives SET lore_status = 'LORE_STATUS_SOLO'
			WHERE id = $1 AND lore_status = 'LORE_STATUS_PENDING'`, ids[sp.User]); err != nil {
			return fmt.Errorf("mark tale: %w", err)
		}
	}
	return nil
}

// ensureUser creates a verified seed account with a password nobody knows
// and every notification email turned off.
func (a *app) ensureUser(ctx context.Context, name string) error {
	if _, ok := a.users[name]; ok {
		return nil
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("password: %w", err)
	}
	hash, err := auth.NewPasswords(0).Hash(hex.EncodeToString(secret))
	if err != nil {
		return err
	}
	var id uuid.UUID
	err = a.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, username, age_confirmed_at, email_verified_at, created_at)
		VALUES ($1, $2, $3, now(), now(), now() - interval '60 days')
		ON CONFLICT ((lower(email))) DO UPDATE SET email = EXCLUDED.email
		RETURNING id`, name+"@"+seedDomain, hash, name).Scan(&id)
	if err != nil {
		return fmt.Errorf("account %s: %w", name, err)
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO notification_preferences (user_id, kind, in_app, email)
		SELECT $1, k, true, false FROM unnest($2::text[]) AS k
		ON CONFLICT (user_id, kind) DO UPDATE SET email = false`, id, allKinds()); err != nil {
		return fmt.Errorf("mute email for %s: %w", name, err)
	}
	a.users[name] = id
	return nil
}

func allKinds() []string {
	var out []string
	for n, v := range perspectivv1.NotificationKind_value {
		if v != 0 {
			out = append(out, n)
		}
	}
	return out
}

// backdate places a perspective a few hours after the moment it is about,
// the i-th writer an hour or so after the one before, as people write the
// next morning.
func (a *app) backdate(ctx context.Context, id string, day time.Time, hour *int32, i int) error {
	at := day.Add(20 * time.Hour)
	if hour != nil {
		at = day.Add(time.Duration(*hour)*time.Hour + 2*time.Hour)
	}
	at = at.Add(time.Duration(i) * 67 * time.Minute)
	_, err := a.pool.Exec(ctx, `UPDATE perspectives SET published_at = $2, created_at = $2, updated_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("backdate: %w", err)
	}
	return nil
}

// link files each claim and has the character's author accept it.
func (a *app) link(ctx context.Context, sa seedArc, ids map[string]string) error {
	for _, cl := range sa.Claims {
		characterID, err := a.characterID(ctx, ids[cl.Author], cl.Character)
		if err != nil {
			return err
		}
		var claimed bool
		if err := a.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM claims WHERE character_id = $1 AND claimant_user_id = $2)`,
			characterID, a.users[cl.Claimant]).Scan(&claimed); err != nil {
			return fmt.Errorf("find claim: %w", err)
		}
		if claimed {
			continue
		}
		res, err := a.lore.ClaimCharacter(a.as(ctx, cl.Claimant), connect.NewRequest(&perspectivv1.ClaimCharacterRequest{CharacterId: characterID}))
		if err != nil {
			return fmt.Errorf("%s claims %q: %w", cl.Claimant, cl.Character, err)
		}
		if _, err := a.lore.ResolveClaim(a.as(ctx, cl.Author), connect.NewRequest(&perspectivv1.ResolveClaimRequest{
			ClaimId: res.Msg.GetClaimId(), Accept: true,
		})); err != nil {
			return fmt.Errorf("%s accepts %q: %w", cl.Author, cl.Character, err)
		}
	}
	return nil
}

func (a *app) characterID(ctx context.Context, perspectiveID, label string) (string, error) {
	var id uuid.UUID
	err := a.pool.QueryRow(ctx, `SELECT id FROM characters WHERE perspective_id = $1 AND label = $2`, perspectiveID, label).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("character %q is not in perspective %s", label, perspectiveID)
	}
	if err != nil {
		return "", fmt.Errorf("find character: %w", err)
	}
	return id.String(), nil
}

// voteAndReact has every writer in the arc up-vote the others and leave a
// react or two, so linked lore has the votes the rank job scores.
func (a *app) voteAndReact(ctx context.Context, sa seedArc, ids map[string]string) error {
	emojis := []perspectivv1.Emoji{
		perspectivv1.Emoji_EMOJI_HEART, perspectivv1.Emoji_EMOJI_LAUGH, perspectivv1.Emoji_EMOJI_WOW,
		perspectivv1.Emoji_EMOJI_FIRE, perspectivv1.Emoji_EMOJI_CLAP,
	}
	for i, voter := range sa.Perspectives {
		for j, target := range sa.Perspectives {
			if i == j || (i+j)%3 == 2 {
				continue
			}
			ctxAs := a.as(ctx, voter.User)
			if _, err := a.vote.Vote(ctxAs, connect.NewRequest(&perspectivv1.VoteRequest{PerspectiveId: ids[target.User], On: true})); err != nil {
				return fmt.Errorf("%s votes for %s: %w", voter.User, target.User, err)
			}
			if _, err := a.react.React(ctxAs, connect.NewRequest(&perspectivv1.ReactRequest{
				PerspectiveId: ids[target.User], Emoji: emojis[(i+2*j)%len(emojis)], On: true,
			})); err != nil {
				return fmt.Errorf("%s reacts to %s: %w", voter.User, target.User, err)
			}
		}
	}
	return nil
}
