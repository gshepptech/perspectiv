package importer

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	perspectivv1 "perspectiv/gen/perspectiv/v1"
	"perspectiv/internal/place/importer/dbq"
)

// batchSize is how many upserts go to the server in one round trip.
const batchSize = 1000

// Result counts what an import wrote.
type Result struct {
	// ByKind counts the plan's nodes per kind.
	ByKind map[perspectivv1.PlaceKind]int
	// Inserted, Updated and Matched count rows by outcome: a new row, a row
	// re-imported by its source id, or an existing same-named sibling reused.
	Inserted, Updated, Matched int
	// Merged counts input records folded into a same-named sibling.
	Merged int
	// Skipped counts records outside the United States.
	Skipped int
	// Unfiled counts US malls and stores left out because their address
	// names no imported city or town.
	Unfiled int
	// Unlisted counts US places left out as offices, services or care
	// places nobody would write about.
	Unlisted int
}

// Importer writes a Plan to the places table.
type Importer struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
}

// New returns an Importer writing through pool.
func New(pool *pgxpool.Pool, log zerolog.Logger) *Importer {
	return &Importer{pool: pool, log: log}
}

// Import prepares the plan from the two files and writes it. A file that
// cannot be parsed returns a *ParseError and a division whose parent cannot
// be resolved an *UnresolvedParentError; either way nothing is written.
func (im *Importer) Import(ctx context.Context, divisions, places io.Reader) (Result, error) {
	plan, err := Prepare(divisions, places)
	if err != nil {
		return Result{}, err
	}
	return im.Write(ctx, plan)
}

// Write upserts every node of plan in one transaction, parents before
// children, so a failure part way leaves the table as it was. Re-running the
// same import updates rows in place by source id.
func (im *Importer) Write(ctx context.Context, plan *Plan) (Result, error) {
	res := Result{
		ByKind:   map[perspectivv1.PlaceKind]int{},
		Merged:   len(plan.merged),
		Skipped:  plan.Skipped,
		Unfiled:  plan.Unfiled,
		Unlisted: plan.Unlisted,
	}
	tx, err := im.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ids := make(map[string]uuid.UUID, len(plan.nodes))
	nodes := plan.nodes
	for len(nodes) > 0 {
		// A batch never spans two kinds: a child's parent id must be known
		// before the child is queued.
		n := 1
		for n < len(nodes) && n < batchSize && nodes[n].Kind == nodes[0].Kind {
			n++
		}
		if err := writeBatch(ctx, tx, nodes[:n], ids, &res); err != nil {
			return Result{}, err
		}
		nodes = nodes[n:]
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit import: %w", err)
	}
	im.log.Info().
		Int("states", res.ByKind[perspectivv1.PlaceKind_PLACE_KIND_STATE]).
		Int("cities", res.ByKind[perspectivv1.PlaceKind_PLACE_KIND_CITY]).
		Int("towns", res.ByKind[perspectivv1.PlaceKind_PLACE_KIND_TOWN]).
		Int("malls", res.ByKind[perspectivv1.PlaceKind_PLACE_KIND_MALL]).
		Int("stores", res.ByKind[perspectivv1.PlaceKind_PLACE_KIND_STORE]).
		Int("inserted", res.Inserted).
		Int("updated", res.Updated).
		Int("matched", res.Matched).
		Int("merged", res.Merged).
		Int("skipped", res.Skipped).
		Int("unfiled", res.Unfiled).
		Int("unlisted", res.Unlisted).
		Msg("place import committed")
	return res, nil
}

func writeBatch(ctx context.Context, tx pgx.Tx, nodes []Node, ids map[string]uuid.UUID, res *Result) error {
	params := make([]dbq.UpsertImportedParams, len(nodes))
	for i, n := range nodes {
		p := dbq.UpsertImportedParams{
			Kind:     kindDB(n.Kind),
			Name:     n.Name,
			Timezone: n.Timezone,
			Lat:      float8(n.Lat),
			Lng:      float8(n.Lng),
			Source:   sourceDB(n.Source),
			SourceID: n.SourceID,
			Category: categoryDB(n.Category),
		}
		if n.ParentKey != "" {
			parent, ok := ids[n.ParentKey]
			if !ok {
				return fmt.Errorf("place %s: parent %s was not written before it", n.Key, n.ParentKey)
			}
			p.ParentID = uuid.NullUUID{UUID: parent, Valid: true}
		}
		params[i] = p
	}
	var firstErr error
	dbq.New(tx).UpsertImported(ctx, params).QueryRow(func(i int, row dbq.UpsertImportedRow, err error) {
		if firstErr != nil {
			return
		}
		if err != nil {
			firstErr = fmt.Errorf("write place %s (%q): %w", nodes[i].Key, nodes[i].Name, err)
			return
		}
		ids[nodes[i].Key] = row.ID
		res.ByKind[nodes[i].Kind]++
		switch row.Outcome {
		case "inserted":
			res.Inserted++
		case "updated":
			res.Updated++
		case "matched":
			res.Matched++
		}
	})
	return firstErr
}
