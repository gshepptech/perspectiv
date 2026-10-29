package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog"

	"perspectiv/gen/perspectiv/v1/perspectivv1connect"
	"perspectiv/internal/api"
	"perspectiv/internal/api/buildinfo"
	"perspectiv/internal/api/logging"
	"perspectiv/internal/api/metrics"
	"perspectiv/internal/api/telemetry"
	"perspectiv/internal/arc"
	"perspectiv/internal/auth"
	"perspectiv/internal/db"
	"perspectiv/internal/jobs"
	"perspectiv/internal/lore"
	"perspectiv/internal/lore/rank"
	"perspectiv/internal/moderation"
	"perspectiv/internal/notify"
	"perspectiv/internal/notify/mailer"
	"perspectiv/internal/perspective"
	"perspectiv/internal/place"
	"perspectiv/internal/react"
	"perspectiv/internal/user"
	"perspectiv/internal/vote"
	"perspectiv/web"
)

// shutdownTimeout bounds the drain of in-flight requests and running jobs
// after SIGTERM; Kubernetes' default grace period is 30 seconds.
const shutdownTimeout = 20 * time.Second

// serve runs the process for cfg.Role until ctx ends:
//
//   - api: the Connect API, the SPA, /healthz, /metrics and /about. Jobs are
//     inserted through an insert-only River client; none are worked here.
//   - worker: the River workers (expiry, notify fan-out, email) and the
//     periodic rank job. The HTTP listener serves only /healthz, /metrics
//     and /about, so probes and scrapes work; no Connect route is mounted.
//   - all: both.
//
// It returns exitOK after a clean shutdown and exitFailure when anything
// fails to start or stops on its own.
func serve(ctx context.Context, cfg config, stderr io.Writer) int {
	log := logging.New(stderr, cfg.LogLevel).With().Str("role", cfg.Role).Logger()
	if err := serveErr(ctx, cfg, log); err != nil {
		log.Error().Err(err).Msg("server failed")
		return exitFailure
	}
	log.Info().Msg("server stopped")
	return exitOK
}

func serveErr(ctx context.Context, cfg config, log zerolog.Logger) error {
	shutdownTracing, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:    "perspectiv",
		ServiceVersion: buildinfo.Version,
		Environment:    environment(cfg.PublicBaseURL),
		Endpoint:       cfg.OTLPEndpoint,
	})
	if err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(sctx); err != nil {
			log.Warn().Err(err).Msg("flush traces")
		}
	}()

	pool, err := openPool(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("migrate on start: %w", err)
		}
	}

	reg := metrics.NewRegistry()
	m := metrics.New(reg)
	app, err := build(cfg, pool, m, log)
	if err != nil {
		return err
	}

	handler := api.NewRouter(api.RouterConfig{
		Log:      log,
		Registry: reg,
		Health:   api.HealthChecks(app.healthChecks(pool)),
		Handlers: app.mounts,
		Static:   app.static,
		Preview:  app.preview,
	})

	// Bind before starting workers so a taken port fails fast.
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()
	log.Info().Str("addr", ln.Addr().String()).Bool("api", cfg.runsAPI()).Bool("workers", cfg.runsWorkers()).Msg("serving")

	if app.worker != nil {
		// River ends a client whose start context is cancelled with a hard
		// stop; shutdown stops it gracefully instead.
		if err := app.worker.Start(context.WithoutCancel(ctx)); err != nil {
			_ = srv.Close()
			return fmt.Errorf("start job workers: %w", err)
		}
	}

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serveDone:
		runErr = fmt.Errorf("http server: %w", err)
	case <-app.stopped():
		runErr = errors.New("job workers stopped unexpectedly")
	}
	return errors.Join(runErr, app.shutdown(ctx, srv, log))
}

// app is what one role runs.
type app struct {
	// mounts are the Connect services; empty unless the API runs.
	mounts []api.Mount
	// static is the SPA; nil unless the API runs.
	static fs.FS
	// preview names a shared link's page for its link preview.
	preview api.Previewer
	// worker is the working River client; nil unless the workers run.
	worker *river.Client[pgx.Tx]
}

// healthChecks are the /healthz components: the database in every role, and
// the job client where the workers run, unhealthy once it has stopped.
func (a *app) healthChecks(pool *pgxpool.Pool) map[string]func(context.Context) error {
	checks := map[string]func(context.Context) error{"database": pool.Ping}
	if a.worker != nil {
		checks["jobs"] = func(context.Context) error {
			select {
			case <-a.worker.Stopped():
				return errors.New("job client stopped")
			default:
				return nil
			}
		}
	}
	return checks
}

// stopped is closed when the working client stops; nil (never ready) where
// no workers run.
func (a *app) stopped() <-chan struct{} {
	if a.worker == nil {
		return nil
	}
	return a.worker.Stopped()
}

// shutdown drains HTTP requests, then lets running jobs finish, within
// shutdownTimeout; jobs still running then are cancelled.
func (a *app) shutdown(ctx context.Context, srv *http.Server, log zerolog.Logger) error {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	var errs []error
	if err := srv.Shutdown(sctx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	if a.worker == nil {
		return errors.Join(errs...)
	}
	if err := a.worker.Stop(sctx); err != nil {
		log.Warn().Err(err).Msg("graceful job stop timed out; cancelling running jobs")
		hctx, hcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer hcancel()
		if err := a.worker.StopAndCancel(hctx); err != nil {
			errs = append(errs, fmt.Errorf("stop job workers: %w", err))
		}
	}
	return errors.Join(errs...)
}

// build wires stores, services, handlers and workers for cfg.Role.
//
// Every producer enqueues through one insert-only River client, in every
// role. The working client, built only where the workers run, is a second
// client on the same pool; the rank worker also enqueues through the
// insert-only one, so it needs nothing from the client that runs it.
func build(cfg config, pool *pgxpool.Pool, m *metrics.Metrics, log zerolog.Logger) (*app, error) {
	inserter, err := jobs.NewClient(pool, log, m, nil, nil)
	if err != nil {
		return nil, err
	}
	enq := jobs.NewEnqueuer(inserter)
	loreSvc := lore.NewService(pool, enq, m)

	a := &app{}
	if cfg.runsAPI() {
		if err := a.addAPI(cfg, pool, m, enq, loreSvc, log); err != nil {
			return nil, err
		}
	}
	if cfg.runsWorkers() {
		workers := river.NewWorkers()
		river.AddWorker(workers, jobs.NewExpiryWorker(pool))
		river.AddWorker(workers, rank.NewWorker(pool, loreSvc, m, enq, cfg.CanonMinMembers))
		notify.AddWorkers(workers, pool, newMailer(cfg), m, log, cfg.PublicBaseURL)
		a.worker, err = jobs.NewClient(pool, log, m, workers, []*river.PeriodicJob{rank.PeriodicJob(cfg.RankInterval), notify.DigestPeriodicJob()})
		if err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *app) addAPI(cfg config, pool *pgxpool.Pool, m *metrics.Metrics, enq *jobs.Enqueuer, loreSvc *lore.Service, log zerolog.Logger) error {
	limiter := auth.NewLimiter(pool)
	authSvc, err := auth.NewService(pool, enq, limiter, auth.Config{
		JWTSecret:     cfg.JWTSecret,
		PublicBaseURL: cfg.PublicBaseURL,
		CookieSecure:  cfg.CookieSecure,
		SignUpMode:    auth.SignUpMode(cfg.SignUpMode),
	}, log)
	if err != nil {
		return err
	}
	avatars, err := user.NewAvatarStore(cfg.AvatarDir)
	if err != nil {
		return err
	}
	arcSvc := arc.NewService(pool)
	placeSvc := place.NewService(place.NewStore(pool))
	perspectiveSvc := perspective.NewService(pool, arcSvc, loreSvc, enq, m, cfg.ExpiryWindow)
	a.preview = previewer{arcs: arcSvc, perspectives: perspectiveSvc, lores: loreSvc, places: placeSvc}.preview
	chain := connect.WithInterceptors(api.Interceptors(log, authSvc, authSvc, limiter)...)

	a.mounts = []api.Mount{
		mount[perspectivv1connect.AuthServiceHandler](perspectivv1connect.NewAuthServiceHandler, auth.NewHandler(authSvc), chain),
		mount[perspectivv1connect.UserServiceHandler](perspectivv1connect.NewUserServiceHandler,
			user.NewHandler(user.NewService(pool, avatars, authSvc, log)), chain),
		mount[perspectivv1connect.PlaceServiceHandler](perspectivv1connect.NewPlaceServiceHandler,
			place.NewHandler(placeSvc), chain),
		mount[perspectivv1connect.ArcServiceHandler](perspectivv1connect.NewArcServiceHandler, arc.NewHandler(arcSvc), chain),
		mount[perspectivv1connect.PerspectiveServiceHandler](perspectivv1connect.NewPerspectiveServiceHandler,
			perspective.NewHandler(perspectiveSvc), chain),
		mount[perspectivv1connect.LoreServiceHandler](perspectivv1connect.NewLoreServiceHandler, lore.NewHandler(loreSvc), chain),
		mount[perspectivv1connect.VoteServiceHandler](perspectivv1connect.NewVoteServiceHandler,
			vote.NewHandler(vote.NewService(pool, loreSvc, enq)), chain),
		mount[perspectivv1connect.ReactServiceHandler](perspectivv1connect.NewReactServiceHandler,
			react.NewHandler(react.NewService(pool)), chain),
		mount[perspectivv1connect.NotifyServiceHandler](perspectivv1connect.NewNotifyServiceHandler,
			notify.NewHandler(notify.NewService(pool)), chain),
		mount[perspectivv1connect.ModerationServiceHandler](perspectivv1connect.NewModerationServiceHandler,
			moderation.NewHandler(moderation.NewService(pool, enq, log)), chain),
		avatars.Mount,
	}
	a.static, err = fs.Sub(web.Dist, "dist")
	return err
}

// mount binds a generated Connect handler constructor to its implementation
// and the interceptor chain, in the api.Mount shape the router takes.
func mount[T any](newHandler func(T, ...connect.HandlerOption) (string, http.Handler), impl T, chain connect.HandlerOption) api.Mount {
	return func(opts ...connect.HandlerOption) (string, http.Handler) {
		return newHandler(impl, append(opts, chain)...)
	}
}

func newMailer(cfg config) mailer.Mailer {
	if cfg.MailProvider == mailResend {
		return mailer.NewResend(cfg.ResendAPIKey, cfg.MailFrom)
	}
	return mailer.NewSMTP(cfg.SMTPAddr, cfg.MailFrom)
}

// environment names the deployment for the deployment.environment trace
// attribute. No setting names it, so it is the host the site is served on
// (perspectiv.example, localhost:5173), which tells deployments apart.
func environment(publicBaseURL string) string {
	u, err := url.Parse(publicBaseURL)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return u.Host
}
