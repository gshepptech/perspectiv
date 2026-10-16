// Command layoutcheck enforces the Go module layout under internal/ and pkg/.
//
// Rules, as this tool reads them:
//
//   - The only top-level packages under internal/ are the ten domains (auth,
//     user, place, arc, perspective, lore, vote, react, notify, moderation),
//     internal/jobs, and the shared infrastructure packages the cross-slice
//     contract defines: internal/api (with apiutil, metrics, logging,
//     telemetry, buildinfo below it), internal/db and internal/testdb. The
//     only package under pkg/ is pkg/crypto.
//   - No package anywhere in the module is named util, utils, helper or
//     helpers. Narrow cross-domain helpers such as internal/api/apiutil are
//     fine: the rule is on the package name, not on a suffix.
//   - A domain package imports at most three sibling domains. The count is
//     per Go package, over its non-test imports only. Importing any package
//     inside a sibling's tree (internal/notify/mailer) counts as that sibling
//     once. internal/jobs, internal/api, internal/db and pkg/crypto are shared
//     infrastructure, not domains, and are not counted.
//
// The tool runs `go list -e -json ./...` in the module root, so a package
// that fails to compile is still checked.
//
// Exit status: 0 when the layout is clean, 1 on any violation, 2 when the
// module cannot be listed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	exitOK        = 0
	exitViolation = 1
	exitUsage     = 2
	maxSiblings   = 3
)

var (
	domains = set("auth", "user", "place", "arc", "perspective", "lore", "vote", "react", "notify", "moderation")
	// shared lists the non-domain top-level packages allowed under internal/.
	shared      = set("jobs", "api", "db", "testdb")
	pkgAllowed  = set("crypto")
	bannedNames = set("util", "utils", "helper", "helpers")
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// listedPackage is the subset of `go list -json` output this tool reads.
type listedPackage struct {
	ImportPath string
	Imports    []string
}

func run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("layoutcheck", flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", ".", "module root to check")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	module, pkgs, err := list(context.Background(), *root)
	if err != nil {
		_, _ = io.WriteString(out, err.Error()+"\n")
		return exitUsage
	}

	var problems []string
	for _, p := range pkgs {
		problems = append(problems, check(module, p)...)
	}
	sort.Strings(problems)
	code := exitOK
	if len(problems) > 0 {
		code = exitViolation
	}
	if _, err := io.WriteString(out, strings.Join(append(problems, ""), "\n")); err != nil {
		return exitUsage
	}
	return code
}

// list returns the module path and every package in the module.
func list(ctx context.Context, root string) (string, []listedPackage, error) {
	gomod, err := goCmd(ctx, root, "env", "GOMOD")
	if err != nil {
		return "", nil, err
	}
	if g := strings.TrimSpace(string(gomod)); g == "" || g == os.DevNull {
		return "", nil, fmt.Errorf("%s is not inside a Go module", root)
	}
	mod, err := goCmd(ctx, root, "list", "-m")
	if err != nil {
		return "", nil, err
	}
	module := strings.TrimSpace(string(mod))

	raw, err := goCmd(ctx, root, "list", "-e", "-json=ImportPath,Imports", "./...")
	if err != nil {
		return "", nil, err
	}
	var pkgs []listedPackage
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var p listedPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("decode go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return module, pkgs, nil
}

func goCmd(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // fixed binary; the arguments are this tool's own go list calls.
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return b, nil
}

// check returns every rule the package breaks.
func check(module string, p listedPackage) []string {
	rel, ok := strings.CutPrefix(p.ImportPath, module+"/")
	if !ok {
		return nil
	}
	parts := strings.Split(rel, "/")
	var problems []string

	if bannedNames[parts[len(parts)-1]] {
		problems = append(problems, fmt.Sprintf("%s: a %s package is not allowed; put the helper in the package that uses it",
			p.ImportPath, parts[len(parts)-1]))
	}

	if len(parts) < 2 {
		return problems
	}
	switch parts[0] {
	case "internal":
		if !domains[parts[1]] && !shared[parts[1]] {
			problems = append(problems, fmt.Sprintf("%s: internal/%s is not an allowed top-level package", p.ImportPath, parts[1]))
		}
		if domains[parts[1]] {
			problems = append(problems, siblingProblems(module, parts[1], p)...)
		}
	case "pkg":
		if !pkgAllowed[parts[1]] {
			problems = append(problems, fmt.Sprintf("%s: pkg/%s is not an allowed top-level package", p.ImportPath, parts[1]))
		}
	}
	return problems
}

// siblingProblems reports a domain package that imports too many sibling domains.
func siblingProblems(module, own string, p listedPackage) []string {
	seen := map[string]bool{}
	for _, imp := range p.Imports {
		rel, ok := strings.CutPrefix(imp, module+"/internal/")
		if !ok {
			continue
		}
		d, _, _ := strings.Cut(rel, "/")
		if domains[d] && d != own {
			seen[d] = true
		}
	}
	if len(seen) <= maxSiblings {
		return nil
	}
	names := make([]string, 0, len(seen))
	for d := range seen {
		names = append(names, d)
	}
	sort.Strings(names)
	return []string{fmt.Sprintf("%s: imports %d sibling domains (%s); the limit is %d",
		p.ImportPath, len(names), strings.Join(names, ", "), maxSiblings)}
}
