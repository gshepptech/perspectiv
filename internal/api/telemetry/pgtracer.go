package telemetry

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/logging"
)

// PoolTracer returns the pgx tracer for the pool's ConnConfig.Tracer: otelpgx
// for spans, composed with a query-level tracer that logs each query's name,
// duration and failure. Neither may be dropped.
func PoolTracer(log zerolog.Logger) pgx.QueryTracer {
	return multitracer.New(
		otelpgx.NewTracer(),
		&queryTracer{log: logging.Component(log, "db")},
	)
}

// queryTracer logs one line per query. It logs the sqlc query name (from the
// "-- name: X :kind" comment sqlc puts first in every query), never the SQL
// arguments, which can hold personal data. Failures log at warn, except
// pgx.ErrNoRows, which is an ordinary "not found"; successes log at debug.
type queryTracer struct {
	log zerolog.Logger
}

type queryStartKey struct{}

type queryStart struct {
	at   time.Time
	name string
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryStart{at: time.Now(), name: QueryName(data.SQL)})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	st, ok := ctx.Value(queryStartKey{}).(queryStart)
	if !ok {
		return
	}
	log := logging.Ctx(ctx, t.log)
	ev := log.Debug()
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		ev = log.Warn().Err(data.Err)
	}
	ev.Str("query", st.name).
		Dur("duration", time.Since(st.at)).
		Int64("rows", data.CommandTag.RowsAffected()).
		Msg("db query")
}

// QueryName returns the sqlc name of a query, or "unnamed" for SQL without a
// leading "-- name:" comment.
func QueryName(sql string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(sql), "\n")
	rest, ok := strings.CutPrefix(line, "-- name:")
	if !ok {
		return "unnamed"
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "unnamed"
	}
	return fields[0]
}
