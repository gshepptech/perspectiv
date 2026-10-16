//go:build integration

package chart

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"go.yaml.in/yaml/v3"
)

// backupEnv is the environment the CronJob gives the script, pointed at the
// server inside the same container.
func backupEnv(dir, keep string) []string {
	return []string{
		"PGHOST=127.0.0.1", "PGUSER=perspectiv", "PGDATABASE=perspectiv", "PGPASSWORD=perspectiv",
		"BACKUP_DIR=" + dir, "BACKUP_KEEP=" + keep,
	}
}

// chartPostgresImage is the postgres image values.yaml deploys, so the script
// is exercised against the same pg_dump and server the cluster runs.
func chartPostgresImage(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(chartDir(t), "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	img := str(v, "postgres", "image")
	if img == "" {
		t.Fatal("values.yaml has no postgres.image")
	}
	return img
}

type pgBox struct {
	t   *testing.T
	ctr *postgres.PostgresContainer
}

// run executes cmd in the container as the postgres user, the uid the
// CronJob runs as, and returns its exit code and output.
func (b pgBox) run(env []string, cmd ...string) (int, string) {
	b.t.Helper()
	code, r, err := b.ctr.Exec(b.t.Context(), cmd, tcexec.WithUser("postgres"), tcexec.WithEnv(env), tcexec.Multiplexed())
	if err != nil {
		b.t.Fatalf("exec %v: %v", cmd, err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		b.t.Fatalf("read output: %v", err)
	}
	return code, string(out)
}

func (b pgBox) mustRun(env []string, cmd ...string) string {
	b.t.Helper()
	code, out := b.run(env, cmd...)
	if code != 0 {
		b.t.Fatalf("%v exited %d:\n%s", cmd, code, out)
	}
	return out
}

func (b pgBox) psql(sql string) string {
	b.t.Helper()
	return strings.TrimSpace(b.mustRun(backupEnv("", ""), "psql", "-tA", "-v", "ON_ERROR_STOP=1", "-c", sql))
}

func (b pgBox) files(dir string) []string {
	b.t.Helper()
	out := strings.Fields(b.mustRun(nil, "ls", "-1A", dir))
	slices.Sort(out)
	return out
}

func (b pgBox) newDir(name string, seed ...string) string {
	b.t.Helper()
	dir := "/backups/" + name
	args := append([]string{"sh", "-c", `mkdir -p "$0" && cd "$0" && for f in "$@"; do echo stale > "$f"; done`, dir}, seed...)
	b.mustRun(nil, args...)
	return dir
}

func dumps(names []string) []string {
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, "perspectiv-") && strings.HasSuffix(n, ".dump") {
			out = append(out, n)
		}
	}
	return out
}

func TestBackupScriptAgainstPostgres18(t *testing.T) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, chartPostgresImage(t),
		postgres.WithDatabase("perspectiv"), postgres.WithUsername("perspectiv"), postgres.WithPassword("perspectiv"),
		postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(chartDir(t), "files", "backup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ctr.CopyToContainer(ctx, script, "/scripts/backup.sh", 0o755); err != nil {
		t.Fatalf("copy script: %v", err)
	}
	root := pgBox{t: t, ctr: ctr}
	if code, _, err := ctr.Exec(ctx, []string{"sh", "-c", "mkdir -p /backups && chown postgres:postgres /backups"}); err != nil || code != 0 {
		t.Fatalf("prepare /backups: code %d err %v", code, err)
	}
	root.psql("CREATE TABLE canary (id int PRIMARY KEY, note text); INSERT INTO canary VALUES (1, 'kept')")

	// Subtests share one server and run in order: the restore case drops and
	// restores the table the others dump.
	t.Run("writes a verified archive that restores", func(t *testing.T) { backupRestores(pgBox{t: t, ctr: ctr}) })
	t.Run("keeps only the newest archives", func(t *testing.T) { backupPrunes(pgBox{t: t, ctr: ctr}) })
	t.Run("a failed dump leaves no archive and prunes nothing", func(t *testing.T) { backupFailsClean(pgBox{t: t, ctr: ctr}) })
	t.Run("rejects a retention that is not a positive integer", func(t *testing.T) { backupRejectsKeep(pgBox{t: t, ctr: ctr}) })
}

func backupRestores(b pgBox) {
	t := b.t
	dir := b.newDir("restore")
	out := b.mustRun(backupEnv(dir, "14"), "sh", "/scripts/backup.sh")
	got := b.files(dir)
	if len(got) != 1 || len(dumps(got)) != 1 {
		t.Fatalf("backup dir holds %v, want one archive", got)
	}
	if !strings.Contains(out, "backup: wrote "+dir+"/"+got[0]) {
		t.Errorf("output does not name the archive:\n%s", out)
	}
	b.psql("DROP TABLE canary")
	b.mustRun(backupEnv("", ""), "pg_restore", "--clean", "--if-exists", "--no-owner", "-d", "perspectiv", dir+"/"+got[0])
	if note := b.psql("SELECT note FROM canary WHERE id = 1"); note != "kept" {
		t.Errorf("restored row note = %q, want kept", note)
	}
}

func backupPrunes(b pgBox) {
	t := b.t
	dir := b.newDir("prune",
		"perspectiv-20200101T000000Z.dump", "perspectiv-20200102T000000Z.dump",
		"perspectiv-20200103T000000Z.dump", "notes.txt")
	b.mustRun(backupEnv(dir, "2"), "sh", "/scripts/backup.sh")
	got := b.files(dir)
	d := dumps(got)
	if len(d) != 2 || d[0] != "perspectiv-20200103T000000Z.dump" || d[1] < "perspectiv-2026" {
		t.Errorf("archives after prune = %v, want 20200103 and the new one", d)
	}
	if !slices.Contains(got, "notes.txt") {
		t.Error("prune removed a file that is not an archive")
	}
}

func backupFailsClean(b pgBox) {
	t := b.t
	seed := []string{"perspectiv-20200101T000000Z.dump", "perspectiv-20200102T000000Z.dump"}
	dir := b.newDir("fail", seed...)
	env := append(backupEnv(dir, "1"), "PGDATABASE=does_not_exist")
	code, out := b.run(env, "sh", "/scripts/backup.sh")
	if code == 0 {
		t.Fatalf("backup of a missing database exited 0:\n%s", out)
	}
	if got := b.files(dir); !slices.Equal(got, seed) {
		t.Errorf("backup dir after failure = %v, want only %v", got, seed)
	}
}

func backupRejectsKeep(b pgBox) {
	t := b.t
	dir := b.newDir("badkeep")
	for _, keep := range []string{"0", "-1", "two", ""} {
		code, out := b.run(backupEnv(dir, keep), "sh", "/scripts/backup.sh")
		if code == 0 {
			t.Errorf("BACKUP_KEEP=%q exited 0:\n%s", keep, out)
		}
	}
	if got := b.files(dir); len(got) != 0 {
		t.Errorf("rejected runs wrote %v", got)
	}
}
