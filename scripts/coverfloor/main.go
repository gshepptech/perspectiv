// Command coverfloor enforces per-package statement-coverage floors over one
// or more Go coverage profiles (the files `go test -coverprofile` writes).
//
// Usage:
//
//	coverfloor -floor <import/path>=<percent> [-floor ...] <profile> [<profile> ...]
//
// Measurement rules, decided here because make check depends on them:
//
//   - Coverage is Go's own statement coverage: the numStmt and count columns
//     of each profile block. No third-party coverage tool is involved.
//   - Profiles are merged block by block (file plus position range): a block
//     counts as covered when any profile hit it, so the unit and integration
//     tiers both contribute to one package's figure.
//   - A floor applies to the package directory itself. Statements in
//     sub-packages (internal/lore/rank under internal/lore, generated dbq
//     packages) neither help nor hurt the parent's figure.
//   - A package meets its floor when covered*100/total >= floor.
//   - A floored package with no statements in any profile fails: its coverage
//     cannot be shown, so the gate fails closed rather than passing silently.
//
// Exit status: 0 when every floor is met, 1 when any floor is missed, 2 on a
// usage error or an unreadable or malformed profile.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

const (
	exitOK    = 0
	exitBelow = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// floorList collects repeated -floor flags.
type floorList map[string]float64

func (f floorList) String() string { return "" }

func (f floorList) Set(v string) error {
	pkg, pct, ok := strings.Cut(v, "=")
	if !ok || pkg == "" {
		return fmt.Errorf("floor %q: want <import/path>=<percent>", v)
	}
	n, err := strconv.ParseFloat(pct, 64)
	if err != nil || n < 0 || n > 100 {
		return fmt.Errorf("floor %q: percent must be a number from 0 to 100", v)
	}
	f[pkg] = n
	return nil
}

// block is one merged profile block.
type block struct {
	stmts   int
	covered bool
}

func run(args []string, out io.Writer) int {
	floors := floorList{}
	fs := flag.NewFlagSet("coverfloor", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Var(floors, "floor", "package floor as <import/path>=<percent>; repeatable")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if len(floors) == 0 || fs.NArg() == 0 {
		return emit(out, []byte("usage: coverfloor -floor <import/path>=<percent> [-floor ...] <profile> [<profile> ...]\n"), exitUsage)
	}

	blocks := map[string]*block{}
	for _, p := range fs.Args() {
		if err := mergeProfile(p, blocks); err != nil {
			return emit(out, []byte(err.Error()+"\n"), exitUsage)
		}
	}
	text, code := report(floors, packageTotals(blocks))
	return emit(out, text, code)
}

// emit writes text and returns code, or the usage status when the write fails.
func emit(out io.Writer, text []byte, code int) int {
	if _, err := out.Write(text); err != nil {
		return exitUsage
	}
	return code
}

// mergeProfile reads one coverage profile into blocks.
func mergeProfile(name string, blocks map[string]*block) error {
	raw, err := os.ReadFile(name) //nolint:gosec // profile paths come from the Makefile, not from a request.
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}

	sc := bufio.NewScanner(bytes.NewReader(raw))
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if line == 1 {
			if !strings.HasPrefix(text, "mode: ") {
				return fmt.Errorf("%s:1: missing \"mode:\" header", name)
			}
			continue
		}
		if text == "" {
			continue
		}
		key, stmts, count, err := parseLine(text)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", name, line, err)
		}
		b, ok := blocks[key]
		if !ok {
			b = &block{stmts: stmts}
			blocks[key] = b
		}
		b.covered = b.covered || count > 0
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if line == 0 {
		return fmt.Errorf("%s: empty profile", name)
	}
	return nil
}

var errBadLine = errors.New("malformed profile line")

// parseLine splits "file:l.c,l.c numStmt count" into the block key, the
// statement count and the hit count.
func parseLine(text string) (key string, stmts, count int, err error) {
	fields := strings.Fields(text)
	if len(fields) != 3 || !strings.Contains(fields[0], ":") {
		return "", 0, 0, fmt.Errorf("%w: %q", errBadLine, text)
	}
	stmts, err = strconv.Atoi(fields[1])
	if err != nil || stmts < 0 {
		return "", 0, 0, fmt.Errorf("%w: statement count in %q", errBadLine, text)
	}
	count, err = strconv.Atoi(fields[2])
	if err != nil || count < 0 {
		return "", 0, 0, fmt.Errorf("%w: hit count in %q", errBadLine, text)
	}
	return fields[0], stmts, count, nil
}

// totals is the covered and total statement count of one package.
type totals struct{ covered, total int }

// packageTotals sums merged blocks per package directory (the import path of
// the file without its base name).
func packageTotals(blocks map[string]*block) map[string]totals {
	out := map[string]totals{}
	for key, b := range blocks {
		file := key[:strings.LastIndex(key, ":")]
		pkg := path.Dir(file)
		t := out[pkg]
		t.total += b.stmts
		if b.covered {
			t.covered += b.stmts
		}
		out[pkg] = t
	}
	return out
}

// report renders one line per floored package and returns the exit status.
func report(floors floorList, pkgs map[string]totals) ([]byte, int) {
	names := make([]string, 0, len(floors))
	for name := range floors {
		names = append(names, name)
	}
	sort.Strings(names)

	var text []byte
	code := exitOK
	for _, name := range names {
		floor := floors[name]
		t := pkgs[name]
		if t.total == 0 {
			text = fmt.Appendf(text, "FAIL %s: no coverage data (package missing or never tested); floor %.1f%%\n", name, floor)
			code = exitBelow
			continue
		}
		// Compare in integer space so 80.0 against an 80 floor never trips on
		// float rounding: covered*100 >= floor*total.
		pct := float64(t.covered) * 100 / float64(t.total)
		if float64(t.covered)*100 < floor*float64(t.total) {
			text = fmt.Appendf(text, "FAIL %s: %.1f%% below floor %.1f%% (%d of %d statements)\n",
				name, truncate1(pct), floor, t.covered, t.total)
			code = exitBelow
			continue
		}
		text = fmt.Appendf(text, "ok   %s: %.1f%% (floor %.1f%%)\n", name, truncate1(pct), floor)
	}
	return text, code
}

// truncate1 drops digits past the first decimal so a figure just under the
// floor never prints as the floor itself (79.95 prints 79.9, not 80.0).
func truncate1(pct float64) float64 {
	return float64(int(pct*10)) / 10
}
