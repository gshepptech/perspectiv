package chart

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// chartDir is the directory holding Chart.yaml, the one this file sits in.
func chartDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Dir(file)
}

// helmTemplate runs helm template against the chart and returns the output
// and whether helm exited zero.
func helmTemplate(t *testing.T, args ...string) (string, bool) {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Fatal("helm is not on PATH: brew install helm (macOS) or see https://helm.sh/docs/intro/install/")
	}
	full := append([]string{"template", "perspectiv", chartDir(t), "--namespace", "perspectiv"}, args...)
	cmd := exec.CommandContext(t.Context(), helm, full...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), true
	case errors.As(err, &exit):
		return out.String(), false
	default:
		t.Fatalf("run helm: %v", err)
		return "", false
	}
}

// object is one rendered Kubernetes manifest.
type object = map[string]any

// manifests renders the chart with the base values plus extra and indexes
// the objects by "Kind/name".
func manifests(t *testing.T, extra ...string) map[string]object {
	t.Helper()
	out, ok := helmTemplate(t, append(append(setAllBut(""), "--set", "image.tag=1.2.3"), extra...)...)
	if !ok {
		t.Fatalf("helm template failed:\n%s", out)
	}
	objs := map[string]object{}
	dec := yaml.NewDecoder(strings.NewReader(out))
	for {
		var o object
		err := dec.Decode(&o)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode rendered yaml: %v\n%s", err, out)
		}
		if o == nil {
			continue
		}
		key := str(o, "kind") + "/" + str(o, "metadata", "name")
		if _, dup := objs[key]; dup {
			t.Fatalf("duplicate object %s", key)
		}
		objs[key] = o
	}
	return objs
}

// get walks a path of map keys and slice indexes through decoded YAML.
func get(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(object)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			s, ok := v.([]any)
			if !ok || k >= len(s) {
				return nil
			}
			v = s[k]
		}
	}
	return v
}

func str(v any, path ...any) string {
	s, _ := get(v, path...).(string)
	return s
}

func list(v any, path ...any) []any {
	s, _ := get(v, path...).([]any)
	return s
}

func must(t *testing.T, objs map[string]object, key string) object {
	t.Helper()
	o, ok := objs[key]
	if !ok {
		keys := make([]string, 0, len(objs))
		for k := range objs {
			keys = append(keys, k)
		}
		t.Fatalf("no %s rendered; have %v", key, keys)
	}
	return o
}

// container finds a container by name in a pod spec's containers or
// initContainers list.
func container(t *testing.T, podSpec any, field, name string) any {
	t.Helper()
	for _, c := range list(podSpec, field) {
		if str(c, "name") == name {
			return c
		}
	}
	t.Fatalf("no %s %q in pod spec", field, name)
	return nil
}

// env returns a container's env as name -> entry.
func env(c any) map[string]any {
	m := map[string]any{}
	for _, e := range list(c, "env") {
		m[str(e, "name")] = e
	}
	return m
}

func podSpec(o object) any {
	if str(o, "kind") == "CronJob" {
		return get(o, "spec", "jobTemplate", "spec", "template", "spec")
	}
	return get(o, "spec", "template", "spec")
}

// configEnv merges the container's literal env with the ConfigMap it pulls
// in through envFrom, the way the kubelet does.
func configEnv(t *testing.T, objs map[string]object, c any) map[string]string {
	t.Helper()
	vals := map[string]string{}
	for _, src := range list(c, "envFrom") {
		if name := str(src, "configMapRef", "name"); name != "" {
			cm := must(t, objs, "ConfigMap/"+name)
			for k, v := range get(cm, "data").(map[string]any) {
				vals[k], _ = v.(string)
			}
		}
	}
	for name, e := range env(c) {
		if v, ok := get(e, "value").(string); ok {
			vals[name] = v
		}
	}
	return vals
}

// requiredValues are the values without which the chart must not render.
var requiredValues = map[string]string{ //nolint:gosec // G101: value names, not credentials
	"image.repository":       "registry.example/perspectiv",
	"host":                   "perspectiv.example",
	"secrets.existingSecret": "perspectiv-secrets",
	"tls.acme.email":         "ops@perspectiv.example",
	"app.mail.from":          "Perspectiv <no-reply@perspectiv.example>",
}

// setAllBut returns --set arguments for every required value except skip.
func setAllBut(skip string) []string {
	var args []string
	for k, v := range requiredValues {
		if k != skip {
			args = append(args, "--set", k+"="+v)
		}
	}
	return args
}

func TestRequiredValuesFailClosed(t *testing.T) {
	t.Parallel()
	for missing := range requiredValues {
		t.Run("missing "+missing, func(t *testing.T) {
			t.Parallel()
			out, ok := helmTemplate(t, setAllBut(missing)...)
			if ok {
				t.Fatalf("helm template succeeded without %s", missing)
			}
			if !strings.Contains(out, missing) {
				t.Errorf("error does not name %s:\n%s", missing, out)
			}
		})
	}

	t.Run("acme email not needed with an external issuer", func(t *testing.T) {
		t.Parallel()
		args := append(setAllBut("tls.acme.email"),
			"--set", "tls.issuer.create=false", "--set", "tls.issuer.name=letsencrypt-prod", "--set", "tls.issuer.kind=ClusterIssuer")
		if out, ok := helmTemplate(t, args...); !ok {
			t.Fatalf("helm template failed:\n%s", out)
		}
	})

	t.Run("external issuer needs a name", func(t *testing.T) {
		t.Parallel()
		out, ok := helmTemplate(t, append(setAllBut(""), "--set", "tls.issuer.create=false")...)
		if ok || !strings.Contains(out, "tls.issuer.name") {
			t.Fatalf("helm template ok=%v without tls.issuer.name:\n%s", ok, out)
		}
	})
}

// One image runs as an api and a worker Deployment, and neither starts its
// main container before the migration Job of this release revision completes.
func TestOneImageTwoRolesAfterMigration(t *testing.T) {
	t.Parallel()
	objs := manifests(t)
	const image = "registry.example/perspectiv:1.2.3"
	checkMigrationJob(t, objs, image)
	sa := ""
	for _, role := range []string{"api", "worker"} {
		got := checkRoleDeployment(t, objs, role, image)
		if sa != "" && got != sa {
			t.Errorf("%s service account = %q, want %q", role, got, sa)
		}
		sa = got
	}
	checkWaiterRBAC(t, objs, sa)
}

func checkMigrationJob(t *testing.T, objs map[string]object, image string) {
	t.Helper()
	job := must(t, objs, "Job/perspectiv-migrate-1")
	jobMain := container(t, podSpec(job), "containers", "migrate")
	if got := str(jobMain, "image"); got != image {
		t.Errorf("migrate image = %q, want %q", got, image)
	}
	if args := list(jobMain, "args"); len(args) != 1 || args[0] != "migrate" {
		t.Errorf("migrate args = %v, want [migrate]", args)
	}
	if got := str(podSpec(job), "restartPolicy"); got != "Never" {
		t.Errorf("migrate restartPolicy = %q", got)
	}
	if get(job, "spec", "ttlSecondsAfterFinished") != nil {
		t.Error("migrate Job sets ttlSecondsAfterFinished; a restarted pod could not find it to wait on")
	}
	pg := container(t, podSpec(job), "initContainers", "wait-for-postgres")
	if !strings.Contains(strings.Join(anyStrings(list(pg, "command")), " "), "pg_isready") {
		t.Error("migrate Job does not wait for Postgres with pg_isready")
	}
	if get(env(jobMain)["DATABASE_URL"], "value") == nil {
		t.Error("migrate Job has no DATABASE_URL")
	}
}

// checkRoleDeployment checks one role's Deployment and returns its service account.
func checkRoleDeployment(t *testing.T, objs map[string]object, role, image string) string {
	t.Helper()
	dep := must(t, objs, "Deployment/perspectiv-"+role)
	spec := podSpec(dep)
	main := container(t, spec, "containers", "perspectiv")
	if got := str(main, "image"); got != image {
		t.Errorf("%s image = %q, want %q", role, got, image)
	}
	if got := configEnv(t, objs, main)["ROLE"]; got != role {
		t.Errorf("%s ROLE = %q", role, got)
	}
	wait := container(t, spec, "initContainers", "wait-for-migration")
	cmd := strings.Join(anyStrings(append(list(wait, "command"), list(wait, "args")...)), " ")
	for _, want := range []string{"kubectl wait", "--for=condition=complete", "job/perspectiv-migrate-1"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("%s wait-for-migration command %q lacks %q", role, cmd, want)
		}
	}
	if got := str(main, "readinessProbe", "httpGet", "path"); got != "/healthz" {
		t.Errorf("%s readiness path = %q, want /healthz", role, got)
	}
	if got := str(dep, "spec", "template", "metadata", "annotations", "prometheus.io/path"); got != "/metrics" {
		t.Errorf("%s scrape path = %q, want /metrics", role, got)
	}
	if got := get(spec, "securityContext", "runAsNonRoot"); got != true {
		t.Errorf("%s runAsNonRoot = %v", role, got)
	}
	return str(spec, "serviceAccountName")
}

// checkWaiterRBAC checks that the waiting service account may read Jobs in
// the namespace, and only that.
func checkWaiterRBAC(t *testing.T, objs map[string]object, sa string) {
	t.Helper()
	binding := must(t, objs, "RoleBinding/perspectiv-migration-waiter")
	if got := str(binding, "subjects", 0, "name"); got != sa {
		t.Errorf("RoleBinding subject = %q, want %q", got, sa)
	}
	rules := list(must(t, objs, "Role/"+str(binding, "roleRef", "name")), "rules")
	if len(rules) != 1 {
		t.Fatalf("waiter Role rules = %v", rules)
	}
	verbs := anyStrings(list(rules[0], "verbs"))
	if !containsAll(anyStrings(list(rules[0], "resources")), "jobs") || !containsAll(verbs, "get", "list", "watch") || len(verbs) != 3 {
		t.Errorf("waiter Role rules = %v", rules)
	}
}

// A rollout of the api answered 502 at the edge for a moment: the server
// stops listening on SIGTERM while ingress-nginx still routes to the old
// pod. The api must keep serving through a preStop sleep, and its grace
// period must cover that sleep plus the server's 20 s drain
// (shutdownTimeout in cmd/perspectiv/serve.go).
func TestAPIServesUntilTheIngressDropsIt(t *testing.T) {
	t.Parallel()
	const drain = 20
	spec := podSpec(must(t, manifests(t), "Deployment/perspectiv-api"))
	main := container(t, spec, "containers", "perspectiv")
	sleep, _ := strconv.Atoi(fmt.Sprint(get(main, "lifecycle", "preStop", "sleep", "seconds")))
	if sleep < 3 {
		t.Fatalf("api preStop sleep = %v, want at least 3 s so ingress-nginx drops the endpoint first", get(main, "lifecycle", "preStop"))
	}
	grace, _ := strconv.Atoi(fmt.Sprint(get(spec, "terminationGracePeriodSeconds")))
	if grace < sleep+drain {
		t.Errorf("api terminationGracePeriodSeconds = %d, want at least %d (preStop %d s + drain %d s)", grace, sleep+drain, sleep, drain)
	}
}

// Every pod's pool together stays under the connections Postgres allows,
// leaving room for the migration job, a backup and an operator's psql.
func TestPoolsFitPostgresConnections(t *testing.T) {
	t.Parallel()
	objs := manifests(t)
	pg := container(t, podSpec(must(t, objs, "StatefulSet/perspectiv-postgres")), "containers", "postgres")
	maxConns := 0
	for _, a := range list(pg, "args") {
		if v, ok := strings.CutPrefix(fmt.Sprint(a), "max_connections="); ok {
			maxConns, _ = strconv.Atoi(v)
		}
	}
	total := 0
	for _, name := range []string{"Deployment/perspectiv-api", "Deployment/perspectiv-worker"} {
		d := must(t, objs, name)
		replicas, _ := strconv.Atoi(fmt.Sprint(get(d, "spec", "replicas")))
		pool, _ := strconv.Atoi(configEnv(t, objs, container(t, podSpec(d), "containers", "perspectiv"))["DB_MAX_CONNS"])
		if replicas < 1 || pool < 1 {
			t.Fatalf("%s: replicas %d, DB_MAX_CONNS %d", name, replicas, pool)
		}
		total += replicas * pool
	}
	if maxConns == 0 || total > maxConns-10 {
		t.Fatalf("pools hold up to %d connections; postgres allows %d, and 10 are kept for jobs and operators", total, maxConns)
	}
}

func TestProductionConfig(t *testing.T) {
	t.Parallel()
	objs := manifests(t)
	main := container(t, podSpec(must(t, objs, "Deployment/perspectiv-api")), "containers", "perspectiv")
	vals := configEnv(t, objs, main)
	want := map[string]string{
		"COOKIE_SECURE":    "true",
		"PUBLIC_BASE_URL":  "https://perspectiv.example",
		"HTTP_ADDR":        ":8080",
		"MIGRATE_ON_START": "false",
		"MAIL_PROVIDER":    "resend",
		"MAIL_FROM":        "Perspectiv <no-reply@perspectiv.example>",
		// The beta: production starts invite-only.
		"SIGNUP_MODE":  "invite",
		"DB_MAX_CONNS": "25",
	}
	for k, v := range want {
		if vals[k] != v {
			t.Errorf("%s = %q, want %q", k, vals[k], v)
		}
	}
	e := env(main)
	for _, name := range []string{"JWT_SECRET", "ENCRYPTION_KEYS", "POSTGRES_PASSWORD", "RESEND_API_KEY"} {
		if got := str(e[name], "valueFrom", "secretKeyRef", "name"); got != "perspectiv-secrets" {
			t.Errorf("%s secret = %q", name, got)
		}
		if got := str(e[name], "valueFrom", "secretKeyRef", "key"); got != name {
			t.Errorf("%s key = %q", name, got)
		}
	}
	for _, name := range []string{"JWT_SECRET", "ENCRYPTION_KEYS"} {
		if got := get(e[name], "valueFrom", "secretKeyRef", "optional"); got == true {
			t.Errorf("%s is optional; a pod must not start without it", name)
		}
	}
	dbURL := str(e["DATABASE_URL"], "value")
	const wantURL = "postgres://perspectiv:$(POSTGRES_PASSWORD)@perspectiv-postgres:5432/perspectiv?sslmode=disable" //nolint:gosec // G101: an env reference, not a password
	if dbURL != wantURL {
		t.Errorf("DATABASE_URL = %q", dbURL)
	}
	// $(VAR) expansion needs the referenced variable defined earlier in env.
	if idxOf(list(main, "env"), "POSTGRES_PASSWORD") > idxOf(list(main, "env"), "DATABASE_URL") {
		t.Error("POSTGRES_PASSWORD is defined after DATABASE_URL, so $(POSTGRES_PASSWORD) would not expand")
	}
	if !hasMount(main, "/var/lib/perspectiv/avatars") || vals["AVATAR_DIR"] != "/var/lib/perspectiv/avatars" {
		t.Error("api does not mount AVATAR_DIR on a volume")
	}
	must(t, objs, "PersistentVolumeClaim/perspectiv-avatars")
}

func TestIngressTLS(t *testing.T) {
	t.Parallel()

	t.Run("chart-managed Let's Encrypt issuer", func(t *testing.T) {
		t.Parallel()
		objs := manifests(t)
		checkIngress(t, objs)
		checkIssuer(t, objs)
	})

	t.Run("external cluster issuer", func(t *testing.T) {
		t.Parallel()
		objs := manifests(t, "--set", "tls.issuer.create=false", "--set", "tls.issuer.kind=ClusterIssuer", "--set", "tls.issuer.name=letsencrypt-prod")
		if got := str(must(t, objs, "Ingress/perspectiv"), "metadata", "annotations", "cert-manager.io/cluster-issuer"); got != "letsencrypt-prod" {
			t.Errorf("cluster-issuer annotation = %q", got)
		}
		if _, ok := objs["Issuer/perspectiv-letsencrypt"]; ok {
			t.Error("chart created an Issuer although tls.issuer.create=false")
		}
	})
}

func checkIngress(t *testing.T, objs map[string]object) {
	t.Helper()
	ing := must(t, objs, "Ingress/perspectiv")
	if got := str(ing, "spec", "ingressClassName"); got != "nginx" {
		t.Errorf("ingressClassName = %q", got)
	}
	if got := str(ing, "metadata", "annotations", "cert-manager.io/issuer"); got != "perspectiv-letsencrypt" {
		t.Errorf("issuer annotation = %q", got)
	}
	if got := str(ing, "metadata", "annotations", "cert-manager.io/common-name"); got != "perspectiv.example" {
		t.Errorf("certificate common name = %q", got)
	}
	if str(ing, "metadata", "annotations", "nginx.ingress.kubernetes.io/force-ssl-redirect") != "true" {
		t.Error("ingress does not force HTTPS")
	}
	if got := anyStrings(list(ing, "spec", "tls", 0, "hosts")); len(got) != 1 || got[0] != "perspectiv.example" {
		t.Errorf("tls hosts = %v", got)
	}
	if str(ing, "spec", "tls", 0, "secretName") == "" {
		t.Error("tls has no secretName for cert-manager to fill")
	}
	paths := map[string]string{}
	for _, p := range list(ing, "spec", "rules", 0, "http", "paths") {
		paths[str(p, "path")] = str(p, "backend", "service", "name")
	}
	if paths["/"] != "perspectiv-api" {
		t.Errorf("/ routes to %q", paths["/"])
	}
	// /metrics stays reachable in-cluster for scraping but not from the
	// public edge: it routes to a Service with no endpoints.
	blackhole := paths["/metrics"]
	if blackhole == "" || blackhole == "perspectiv-api" {
		t.Fatalf("/metrics routes to %q", blackhole)
	}
	if get(must(t, objs, "Service/"+blackhole), "spec", "selector") != nil {
		t.Error("metrics blackhole Service has a selector")
	}
}

func checkIssuer(t *testing.T, objs map[string]object) {
	t.Helper()
	issuer := must(t, objs, "Issuer/perspectiv-letsencrypt")
	if got := str(issuer, "spec", "acme", "server"); got != "https://acme-v02.api.letsencrypt.org/directory" {
		t.Errorf("acme server = %q", got)
	}
	if got := str(issuer, "spec", "acme", "email"); got != "ops@perspectiv.example" {
		t.Errorf("acme email = %q", got)
	}
	if got := str(issuer, "spec", "acme", "solvers", 0, "http01", "ingress", "ingressClassName"); got != "nginx" {
		t.Errorf("http01 solver class = %q", got)
	}
}

// The ingress-nginx values the runbook installs turn HSTS on.
func TestIngressNginxValues(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(chartDir(t), "..", "k3s", "ingress-nginx-values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	cfg := get(v, "controller", "config")
	if str(cfg, "hsts") != "true" {
		t.Errorf("hsts = %v", get(cfg, "hsts"))
	}
	if str(cfg, "hsts-max-age") != "31536000" {
		t.Errorf("hsts-max-age = %v", get(cfg, "hsts-max-age"))
	}
	if str(cfg, "ssl-redirect") != "true" {
		t.Errorf("ssl-redirect = %v", get(cfg, "ssl-redirect"))
	}
	// The per-IP rate limits see each visitor's own address only while the
	// load balancer keeps the source address (Local) and nginx forwards it
	// without trusting a client-sent header; drift either way and the whole
	// site shares one budget, or a client chooses its own address.
	if got := str(v, "controller", "service", "externalTrafficPolicy"); got != "Local" {
		t.Errorf("externalTrafficPolicy = %q, want Local", got)
	}
	if str(cfg, "compute-full-forwarded-for") != "true" || str(cfg, "use-forwarded-headers") != "false" {
		t.Errorf("forwarding: compute-full-forwarded-for %v, use-forwarded-headers %v", get(cfg, "compute-full-forwarded-for"), get(cfg, "use-forwarded-headers"))
	}
}

func TestPostgres18OnPersistentVolume(t *testing.T) {
	t.Parallel()
	objs := manifests(t, "--set", "postgres.storageClass=local-path", "--set", "postgres.size=30Gi")
	sts := must(t, objs, "StatefulSet/perspectiv-postgres")
	pg := container(t, podSpec(sts), "containers", "postgres")
	if img := str(pg, "image"); !strings.HasPrefix(img, "postgres:18.") {
		t.Errorf("postgres image = %q, want postgres:18.x", img)
	}
	// Postgres 18 images keep PGDATA under /var/lib/postgresql/18/docker and
	// declare /var/lib/postgresql as the volume.
	if !hasMount(pg, "/var/lib/postgresql") {
		t.Error("postgres data volume is not mounted at /var/lib/postgresql")
	}
	claim := get(sts, "spec", "volumeClaimTemplates", 0)
	if str(claim, "spec", "storageClassName") != "local-path" || str(claim, "spec", "resources", "requests", "storage") != "30Gi" {
		t.Errorf("volume claim = %v", claim)
	}
	if str(sts, "spec", "persistentVolumeClaimRetentionPolicy", "whenDeleted") != "Retain" {
		t.Error("data PVC is not retained when the StatefulSet is deleted")
	}
	// Shared buffers sized for the places table, not the 128MB default.
	if args := list(pg, "args"); !slices.Contains(args, any("shared_buffers=512MB")) || !slices.Contains(args, any("effective_cache_size=1536MB")) {
		t.Errorf("postgres args = %v, want the tuned memory settings", args)
	}
	if str(pg, "readinessProbe", "exec", "command", 0) != "pg_isready" {
		t.Error("postgres readiness does not use pg_isready")
	}
	if got := str(must(t, objs, "Service/perspectiv-postgres"), "spec", "ports", 0, "name"); got != "postgres" {
		t.Errorf("postgres service port name = %q", got)
	}

	np := must(t, objs, "NetworkPolicy/perspectiv-postgres")
	if got := str(np, "spec", "podSelector", "matchLabels", "app.kubernetes.io/component"); got != "postgres" {
		t.Errorf("network policy selects %q", got)
	}
	allowed := map[string]bool{}
	for _, from := range list(np, "spec", "ingress", 0, "from") {
		for _, v := range anyStrings(list(from, "podSelector", "matchExpressions", 0, "values")) {
			allowed[v] = true
		}
	}
	for _, c := range []string{"api", "worker", "migrate", "backup"} {
		if !allowed[c] {
			t.Errorf("network policy does not admit %s", c)
		}
	}
}

// The backup schedule and retention decided for v1: a pg_dump custom-format
// archive every day at 03:15 UTC onto its own volume, keeping the newest 14.
func TestBackupCronJob(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		objs := manifests(t)
		checkBackupSchedule(t, must(t, objs, "CronJob/perspectiv-postgres-backup"))
		checkBackupStorage(t, objs)
		// local-path binds a claim on its first consumer, and nothing else
		// consumes the backups claim until the first run; helm --wait waits
		// for every claim to bind, so the database pod mounts it read-only.
		pg := container(t, podSpec(must(t, objs, "StatefulSet/perspectiv-postgres")), "containers", "postgres")
		if !hasMount(pg, "/backups") {
			t.Error("the postgres pod does not mount the backups claim, which then never binds under helm --wait")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		objs := manifests(t, "--set", "postgres.backup.enabled=false")
		if _, ok := objs["CronJob/perspectiv-postgres-backup"]; ok {
			t.Error("backup CronJob rendered while disabled")
		}
		pg := container(t, podSpec(must(t, objs, "StatefulSet/perspectiv-postgres")), "containers", "postgres")
		if hasMount(pg, "/backups") {
			t.Error("the postgres pod mounts a backups claim that is not rendered")
		}
	})
}

func checkBackupSchedule(t *testing.T, cj object) {
	t.Helper()
	for path, want := range map[string]string{"schedule": "15 3 * * *", "timeZone": "Etc/UTC", "concurrencyPolicy": "Forbid"} {
		if got := str(cj, "spec", path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	c := container(t, podSpec(cj), "containers", "backup")
	if img := str(c, "image"); !strings.HasPrefix(img, "postgres:18.") {
		t.Errorf("backup image = %q", img)
	}
	e := env(c)
	if got := str(e["BACKUP_KEEP"], "value"); got != "14" {
		t.Errorf("BACKUP_KEEP = %q", got)
	}
	if got := str(e["BACKUP_DIR"], "value"); !hasMount(c, got) {
		t.Errorf("BACKUP_DIR %q is not a mounted volume", got)
	}
	if got := str(e["PGHOST"], "value"); got != "perspectiv-postgres" {
		t.Errorf("PGHOST = %q", got)
	}
	if cmd := strings.Join(anyStrings(list(c, "command")), " "); !strings.Contains(cmd, "backup.sh") {
		t.Errorf("backup command = %q", cmd)
	}
}

func checkBackupStorage(t *testing.T, objs map[string]object) {
	t.Helper()
	// The script in the ConfigMap is files/backup.sh byte for byte, the file
	// the integration test runs against a real Postgres 18.
	want, err := os.ReadFile(filepath.Join(chartDir(t), "files", "backup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if got := str(must(t, objs, "ConfigMap/perspectiv-postgres-backup"), "data", "backup.sh"); got != string(want) {
		t.Error("backup ConfigMap does not carry files/backup.sh")
	}
	pvc := must(t, objs, "PersistentVolumeClaim/perspectiv-postgres-backups")
	if str(pvc, "metadata", "annotations", "helm.sh/resource-policy") != "keep" {
		t.Error("backup PVC is deleted on helm uninstall")
	}
}

func hasMount(c any, path string) bool {
	for _, m := range list(c, "volumeMounts") {
		if str(m, "mountPath") == path {
			return true
		}
	}
	return false
}

func idxOf(envs []any, name string) int {
	for i, e := range envs {
		if str(e, "name") == name {
			return i
		}
	}
	return -1
}

func anyStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
