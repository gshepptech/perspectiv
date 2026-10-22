package api

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"perspectiv/internal/api/buildinfo"
	"perspectiv/internal/api/logging"
)

// Mount builds one Connect service handler with the given options and
// returns its path ("/perspectiv.v1.<Service>/") and handler; the generated
// perspectivv1connect.New<Service>Handler constructors have this shape once
// their service implementation is bound.
//
// The router passes its own request-size option. Each Mount MUST add the
// interceptor chain, connect.WithInterceptors(api.Interceptors(...)...), to
// the options it is given: the router has no authenticator, gate or limiter.
type Mount func(opts ...connect.HandlerOption) (string, http.Handler)

// RouterConfig is everything NewRouter serves.
type RouterConfig struct {
	Log zerolog.Logger
	// Registry is served on /metrics. Nil serves an empty registry.
	Registry *prometheus.Registry
	// Health reports each component's health for /healthz; see
	// HealthChecks. Nil reports only that the process is up.
	Health   func(ctx context.Context) map[string]error
	Handlers []Mount
	// Static is the built SPA (web/dist). Nil serves no SPA.
	Static fs.FS
	// Preview names the page a shared link points at, so link previews in
	// chat apps show its title and opening. Nil serves index.html as built.
	Preview Previewer
}

// Preview is what a link preview shows for one page.
type Preview struct {
	Title       string
	Description string
}

// Previewer returns the preview for a request path; ok is false when the
// path names nothing a signed-out reader may see.
type Previewer func(ctx context.Context, path string) (p Preview, ok bool)

// previewTimeout bounds one preview lookup; past it the page is served
// without one rather than late.
const previewTimeout = 500 * time.Millisecond

// MaxRequestBytes bounds a Connect request body. The largest legitimate
// request is a perspective of 10 parts of 300 words, far under this.
const MaxRequestBytes = 1 << 20

// healthTimeout bounds one /healthz evaluation.
const healthTimeout = 2 * time.Second

// NewRouter returns the server's HTTP handler:
//
//	GET  /healthz   component health, 200 or 503
//	GET  /metrics   Prometheus text from cfg.Registry
//	GET  /about     build info as JSON
//	POST /api/...   the Connect services in cfg.Handlers
//	GET  /*         the SPA: a file from cfg.Static, else index.html
func NewRouter(cfg RouterConfig) http.Handler {
	log := logging.Component(cfg.Log, "http")
	r := chi.NewRouter()
	r.Use(securityHeaders)

	r.Get("/healthz", healthHandler(log, cfg.Health))
	reg := cfg.Registry
	if reg == nil {
		reg = prometheus.NewRegistry()
	}
	r.Method(http.MethodGet, "/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	r.Get("/about", aboutHandler)

	api := http.NewServeMux()
	for _, m := range cfg.Handlers {
		p, h := m(connect.WithReadMaxBytes(MaxRequestBytes))
		api.Handle(p, h)
	}
	r.Mount("/api", http.StripPrefix("/api", api))

	spa := spaHandler(cfg.Static, cfg.Preview)
	r.Get("/*", spa)
	r.Head("/*", spa)
	return r
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// HealthStatus is the /healthz response body.
type HealthStatus struct {
	// Status is "ok" when every component is healthy, else "unavailable".
	Status string `json:"status"`
	// Components maps each checked component to "ok" or "unavailable".
	Components map[string]string `json:"components"`
}

// healthHandler serves /healthz.
//
// What it reports: one entry per component the composition root registers
// (the database for every role, the job client where River runs), each "ok"
// or "unavailable", and an overall status. Any failing component makes the
// response 503 so a readiness probe takes the pod out of rotation. The error
// text is logged, never returned: the endpoint is public. With no checks it
// answers 200 to say the process serves HTTP. The whole evaluation is bounded
// by a two-second timeout so a hung dependency reads as unavailable rather
// than hanging the probe.
func healthHandler(log zerolog.Logger, health func(context.Context) map[string]error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := HealthStatus{Status: "ok", Components: map[string]string{}}
		if health != nil {
			ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
			results := health(ctx)
			cancel()
			for name, err := range results {
				if err == nil {
					out.Components[name] = "ok"
					continue
				}
				out.Status = "unavailable"
				out.Components[name] = "unavailable"
				log.Error().Err(err).Str("event", "health").Str("component", name).Msg("component unhealthy")
			}
		}
		code := http.StatusOK
		if out.Status != "ok" {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, code, out)
	}
}

// HealthChecks runs each named check concurrently and returns its error by
// name; it is the usual RouterConfig.Health. A check that has not returned
// when ctx ends reports ctx's error.
func HealthChecks(checks map[string]func(context.Context) error) func(context.Context) map[string]error {
	names := make([]string, 0, len(checks))
	for n := range checks {
		names = append(names, n)
	}
	sort.Strings(names)
	return func(ctx context.Context) map[string]error {
		var mu sync.Mutex
		out := make(map[string]error, len(names))
		var wg sync.WaitGroup
		for _, n := range names {
			wg.Add(1)
			go func() {
				defer wg.Done()
				done := make(chan error, 1)
				go func() { done <- checks[n](ctx) }()
				var err error
				select {
				case err = <-done:
				case <-ctx.Done():
					err = ctx.Err()
				}
				mu.Lock()
				out[n] = err
				mu.Unlock()
			}()
		}
		wg.Wait()
		return out
	}
}

func aboutHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Get())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // the client may have gone; nothing to do.
}

// spaHandler serves the SPA. A path naming a file in static serves that file;
// any other path without a file extension serves index.html so client-side
// routes such as /places/<id> load the app. A missing path with an extension
// (a stale /assets/x.js) is 404 rather than HTML under a script's name.
// Hashed files under /assets/ are cached for a year; index.html never is.
func spaHandler(static fs.FS, preview Previewer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if static == nil {
			http.NotFound(w, r)
			return
		}
		// path.Clean on a rooted path removes every "..", and an fs.FS
		// rejects any name that would leave its root.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "index.html" {
			serveIndex(w, r, static, preview)
			return
		}
		st, err := fs.Stat(static, name)
		switch {
		case err == nil && !st.IsDir():
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, static, name) //nolint:gosec // name is cleaned and confined to the embedded bundle.
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			http.Error(w, "internal error", http.StatusInternalServerError)
		case path.Ext(name) != "":
			http.NotFound(w, r)
		default:
			serveIndex(w, r, static, preview)
		}
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, static fs.FS, preview Previewer) {
	body, err := fs.ReadFile(static, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if preview != nil {
		ctx, cancel := context.WithTimeout(r.Context(), previewTimeout)
		p, ok := preview(ctx, r.URL.Path)
		cancel()
		if ok {
			body = withPreview(body, p)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

var (
	titleTag   = regexp.MustCompile(`<title>[^<]*</title>`)
	metaTitle  = regexp.MustCompile(`(<meta property="og:title" content=")[^"]*(")`)
	metaDescs  = regexp.MustCompile(`(<meta (?:name="description"|property="og:description") content=")[^"]*(")`)
	previewMax = 300
)

// withPreview writes p into the page title and the description and Open
// Graph tags of index.html, escaped for HTML.
func withPreview(body []byte, p Preview) []byte {
	title := html.EscapeString(p.Title)
	desc := []rune(p.Description)
	if len(desc) > previewMax {
		desc = append(desc[:previewMax-1], '…')
	}
	d := html.EscapeString(string(desc))
	// The replacement strings hold user text; escape "$" so it is not read
	// as a group reference.
	t := strings.ReplaceAll(title, "$", "$$")
	d = strings.ReplaceAll(d, "$", "$$")
	body = titleTag.ReplaceAll(body, []byte("<title>"+t+" · Perspectiv</title>"))
	body = metaTitle.ReplaceAll(body, []byte("${1}"+t+"${2}"))
	return metaDescs.ReplaceAll(body, []byte("${1}"+d+"${2}"))
}
