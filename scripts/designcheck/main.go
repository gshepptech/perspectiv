// Command designcheck holds the design pass ahead of component code.
//
// The project CLAUDE.md names the design system file on a line of the form
//
//	Design system file: `docs/design-system.md`
//
// and this check reads that line, not a hard-coded path. It passes when the
// named file exists and carries a level-one heading (the system's name). When
// the file does not exist yet it passes only while no component code exists:
// any .tsx file under web/src/components or web/src/routes means component
// code was written before the design pass, and the check fails.
//
// Exit status: 0 when the rule holds, 1 when it does not, 2 on a usage error.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	exitOK        = 0
	exitViolation = 1
	exitUsage     = 2
)

var declRE = regexp.MustCompile("(?m)^Design system file: `([^`]+)`")

// componentDirs are where component code lives in the web app.
var componentDirs = []string{"web/src/components", "web/src/routes"}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("designcheck", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	line, code := "ok   ", exitOK
	msg, err := check(*root)
	if err != nil {
		line, code, msg = "FAIL ", exitViolation, err.Error()
	}
	if _, werr := io.WriteString(out, line+msg+"\n"); werr != nil {
		return exitUsage
	}
	return code
}

func check(root string) (string, error) {
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md")) //nolint:gosec // root is the repository being checked.
	if err != nil {
		return "", fmt.Errorf("read project CLAUDE.md: %w", err)
	}
	m := declRE.FindSubmatch(claude)
	if m == nil {
		return "", errors.New("CLAUDE.md has no \"Design system file: `<path>`\" line naming the design system")
	}
	rel := filepath.Clean(filepath.FromSlash(string(m[1])))
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("design system file %q must be a path inside the repository", m[1])
	}

	name, err := heading(filepath.Join(root, rel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		comps, werr := componentFiles(root)
		if werr != nil {
			return "", werr
		}
		if len(comps) > 0 {
			return "", fmt.Errorf("%s does not exist but component code does (%s); run the design pass first",
				m[1], strings.Join(comps, ", "))
		}
		return fmt.Sprintf("design pass pending: %s not written yet and no component code exists", m[1]), nil
	case err != nil:
		return "", err
	case name == "":
		return "", fmt.Errorf("%s has no name: it needs a level-one heading", m[1])
	}
	return fmt.Sprintf("design system %q in %s", name, m[1]), nil
}

// heading returns the text of the first level-one Markdown heading in path.
func heading(path string) (string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path comes from the repository's own CLAUDE.md.
	if err != nil {
		return "", fmt.Errorf("read design system file: %w", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		if h, ok := strings.CutPrefix(sc.Text(), "# "); ok {
			return strings.TrimSpace(h), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read design system file: %w", err)
	}
	return "", nil
}

// componentFiles lists .tsx files under the component directories.
func componentFiles(root string) ([]string, error) {
	var found []string
	for _, dir := range componentDirs {
		base := filepath.Join(root, filepath.FromSlash(dir))
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".tsx") {
				rel, rerr := filepath.Rel(root, p)
				if rerr != nil {
					return rerr
				}
				found = append(found, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}
	sort.Strings(found)
	return found, nil
}
