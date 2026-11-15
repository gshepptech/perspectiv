package rank_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// canonWrites are the statement shapes that write canon state: the arc's
// canon pointer, canon score or member count, a lore's standing, score or
// upvotes, and the canon audit trail. [^;`]* keeps a match inside one SQL
// statement or one Go string literal.
var canonWrites = map[string]*regexp.Regexp{
	"arcs canon columns": regexp.MustCompile(
		`(?is)update\s+arcs\s+(?:\w+\s+)?set\s[^;` + "`" + `]*?\b(?:canon_lore_id|canon_score|member_count)\s*=`),
	"lores standing or score": regexp.MustCompile(
		`(?is)update\s+lores\s+(?:\w+\s+)?set\s[^;` + "`" + `]*?\b(?:status|rank_score|upvotes)\s*=`),
	"canon_changes insert": regexp.MustCompile(`(?is)insert\s+into\s+canon_changes\b`),
}

// moduleRoot is three levels up from this package's directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// stripSQLComments drops "--" line comments so prose about canon writes in a
// comment is not mistaken for one.
func stripSQLComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if j := strings.Index(l, "--"); j >= 0 {
			lines[i] = l[:j]
		}
	}
	return strings.Join(lines, "\n")
}

// canonWritesIn returns "file: shape" for every canon write in production
// SQL and Go under dir, skipping tests, migrations and the skip directory.
func canonWritesIn(t *testing.T, dir, skip string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == skip || d.Name() == "migrations" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		hits, err := canonWritesInFile(path)
		found = append(found, hits...)
		return err
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}

// canonWritesInFile scans one production .sql or non-test .go file.
func canonWritesInFile(path string) ([]string, error) {
	isSQL := strings.HasSuffix(path, ".sql")
	isGo := strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
	if !isSQL && !isGo {
		return nil, nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec // paths come from walking this module's own tree.
	if err != nil {
		return nil, err
	}
	text := string(raw)
	if isSQL {
		text = stripSQLComments(text)
	}
	var found []string
	for shape, re := range canonWrites {
		if re.MatchString(text) {
			found = append(found, path+": "+shape)
		}
	}
	return found, nil
}

// No handler and no domain outside the rank job writes canon state: the vote,
// react, perspective and arc domains, every other domain, and the lore
// service itself hold no statement that writes it.
func TestOnlyTheRankJobWritesCanon(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	rankDir := filepath.Join(root, "internal", "lore", "rank")
	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if found := canonWritesIn(t, dir, rankDir); len(found) > 0 {
			t.Fatalf("canon state written outside the rank job:\n%s", strings.Join(found, "\n"))
		}
	}
}

// The scan recognises every shape: the rank job's own queries hold each one,
// so an empty result above is not a blind scanner.
func TestCanonWriteScanFindsTheRankJob(t *testing.T) {
	t.Parallel()
	rankDir := filepath.Join(moduleRoot(t), "internal", "lore", "rank")
	raw, err := os.ReadFile(filepath.Join(rankDir, "queries.sql"))
	if err != nil {
		t.Fatalf("read rank queries: %v", err)
	}
	text := stripSQLComments(string(raw))
	for shape, re := range canonWrites {
		if !re.MatchString(text) {
			t.Fatalf("scanner does not recognise the rank job's own %s write", shape)
		}
	}
}
