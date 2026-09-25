// Package docscheck provides pure-Go, offline helpers that detect drift
// between the harness source of truth and the documentation surface. It has no
// dependencies beyond the standard library and performs no network access.
//
// The helpers power a test-only drift gate: routes, environment variables,
// metrics counters, the version pin, and relative markdown links are all
// compared against the code. A mismatch fails the test with the file name and
// the missing or stale entry.
package docscheck

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Route is an HTTP method and path pattern exposed by internal/api.
type Route struct {
	Method string
	Path   string
}

// String renders a route as "GET /v1/sessions/{id}".
func (r Route) String() string { return r.Method + " " + r.Path }

var (
	handleFuncRe   = regexp.MustCompile(`mux\.HandleFunc\(\s*"([^"]+)"`)
	commentLineRe  = regexp.MustCompile(`^\s*//(.*)$`)
	commentRouteRe = regexp.MustCompile(`\b(GET|POST|PUT|DELETE|PATCH)\s+(/[^\s,.)\]` + "`" + `]+)`)
	envSourceRe    = regexp.MustCompile(`(?:os\.Getenv|os\.LookupEnv|envOr|envInt)\(\s*"([A-Z][A-Z0-9_]*)"`)
	envTableCellRe = regexp.MustCompile("^\\|\\s*`([A-Z][A-Z0-9_]*)`\\s*\\|")
	metricValueRe  = regexp.MustCompile(`"(harness_[a-z_]+_total)"`)
	// docMethodPathRe matches a method and path inside one backtick span, e.g.
	// `GET /v1/registry/agents`.
	docMethodPathRe = regexp.MustCompile("`(GET|POST|PUT|DELETE|PATCH)\\s+(/[^`]+)`")
	// readmeTableRe matches the README HTTP table where the method and the path
	// are separate backtick spans in adjacent cells.
	readmeTableRe = regexp.MustCompile("\\|\\s*`(GET|POST|PUT|DELETE|PATCH)`\\s*\\|\\s*`(/[^`]+)`")
	versionPinRe  = regexp.MustCompile(`VERSION=(v?\d+\.\d+\.\d+)`)
	mdLinkRe      = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	semverRe      = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// SourceRoutes returns every route registered in the internal/api package,
// combining mux.HandleFunc patterns with the method+path route comments that
// document them. Exact (non-prefix) patterns without a comment default to GET.
func SourceRoutes(apiDir string) ([]Route, error) {
	var prefixes []string
	var comments []Route
	err := filepath.WalkDir(apiDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		s := string(b)
		for _, line := range strings.Split(s, "\n") {
			cm := commentLineRe.FindStringSubmatch(line)
			if cm == nil {
				continue
			}
			for _, m := range commentRouteRe.FindAllStringSubmatch(cm[1], -1) {
				if balancedBraces(m[2]) {
					comments = append(comments, Route{Method: m[1], Path: m[2]})
				}
			}
		}
		for _, m := range handleFuncRe.FindAllStringSubmatch(s, -1) {
			prefixes = append(prefixes, m[1])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	set := map[string]Route{}
	for _, r := range comments {
		set[r.String()] = r
	}
	for _, p := range prefixes {
		if !strings.HasSuffix(p, "/") {
			found := false
			for _, r := range comments {
				if r.Path == p {
					set[r.String()] = r
					found = true
				}
			}
			if !found {
				r := Route{Method: "GET", Path: p}
				set[r.String()] = r
			}
			continue
		}
		covered := false
		for _, r := range comments {
			if strings.HasPrefix(r.Path, p) {
				covered = true
				break
			}
		}
		if !covered {
			return nil, fmt.Errorf("internal/api: prefix %q has no method+path route comment; document the routes so the drift check can verify them", p)
		}
	}
	return sortRoutes(set), nil
}

// DocumentedRoutes parses the routes a markdown file claims exist. It reads
// both the single-backtick form (`GET /v1/registry/agents`) and the README
// table form (| `GET` | `/healthz` |). Sections headed "Not implemented" are
// skipped: those deliberately name routes the server does not serve.
func DocumentedRoutes(path string) ([]Route, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := stripSection(string(b), "not implemented")
	set := map[string]Route{}
	add := func(method, p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		set[method+" "+p] = Route{Method: method, Path: p}
	}
	for _, m := range docMethodPathRe.FindAllStringSubmatch(s, -1) {
		add(m[1], m[2])
	}
	for _, m := range readmeTableRe.FindAllStringSubmatch(s, -1) {
		add(m[1], m[2])
	}
	return sortRoutes(set), nil
}

// SourceEnvVars returns every environment variable name referenced in non-test
// Go source under root.
func SourceEnvVars(root string) ([]string, error) {
	set := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "bin", "data", "node_modules", "docscheck":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range envSourceRe.FindAllStringSubmatch(string(b), -1) {
			set[m[1]] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortedKeys(set), nil
}

// DocumentedEnvVars parses the first column of the environment tables in the
// given markdown files. Only rows shaped like "| `NAME` | ..." count.
func DocumentedEnvVars(paths ...string) ([]string, error) {
	set := map[string]bool{}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if m := envTableCellRe.FindStringSubmatch(sc.Text()); m != nil {
				set[m[1]] = true
			}
		}
		if err := sc.Err(); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
	}
	return sortedKeys(set), nil
}

// SourceMetrics returns every counter value declared in the metrics package.
func SourceMetrics(metricsDir string) ([]string, error) {
	set := map[string]bool{}
	err := filepath.WalkDir(metricsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range metricValueRe.FindAllStringSubmatch(string(b), -1) {
			set[m[1]] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sortedKeys(set), nil
}

// DocumentedMetrics returns every harness_*_total token mentioned in a
// markdown file.
func DocumentedMetrics(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`harness_[a-z_]+_total`)
	set := map[string]bool{}
	for _, m := range re.FindAllString(string(b), -1) {
		set[m] = true
	}
	return sortedKeys(set), nil
}

// VersionPin extracts the first VERSION=<semver> literal from a file.
func VersionPin(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	m := versionPinRe.FindStringSubmatch(string(b))
	if m == nil {
		return "", fmt.Errorf("%s: no VERSION=<semver> literal found", path)
	}
	return m[1], nil
}

// MarkdownFiles returns every markdown file under root, excluding .git.
func MarkdownFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// CheckLinks verifies that every relative markdown link in file resolves to an
// existing path, and that any #fragment matches a heading in the target.
func CheckLinks(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, m := range mdLinkRe.FindAllStringSubmatch(string(b), -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		filePart, anchor, _ := strings.Cut(target, "#")
		if filePart == "" {
			continue
		}
		resolved := filepath.Join(filepath.Dir(path), filePart)
		info, err := os.Stat(resolved)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: relative link %q does not resolve (%s)", rel(path), target, rel(resolved)))
			continue
		}
		if anchor == "" || info.IsDir() {
			continue
		}
		ok, err := hasAnchor(resolved, anchor)
		if err != nil {
			return nil, err
		}
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: link %q has no matching heading #%s in %s", rel(path), target, anchor, rel(resolved)))
		}
	}
	return problems, nil
}

// stripSection removes the body of any heading whose text contains phrase
// (case-insensitive), up to the next heading at the same or a higher level.
func stripSection(doc, phrase string) string {
	lines := strings.Split(doc, "\n")
	var out []string
	skipLevel := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			heading := strings.ToLower(strings.TrimSpace(trimmed[level:]))
			if skipLevel > 0 && level <= skipLevel {
				skipLevel = 0
			}
			if skipLevel == 0 && strings.Contains(heading, phrase) {
				skipLevel = level
				continue
			}
		}
		if skipLevel == 0 {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// hasAnchor reports whether a markdown file contains a heading whose
// GitHub-style anchor equals want.
func hasAnchor(path, want string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if Slugify(heading) == want {
			return true, nil
		}
	}
	return false, sc.Err()
}

// Slugify converts a markdown heading to its GitHub-style anchor.
func Slugify(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

func balancedBraces(s string) bool {
	open, closed := 0, 0
	for _, r := range s {
		switch r {
		case '{':
			open++
		case '}':
			closed++
		}
	}
	return open == closed
}

func sortRoutes(set map[string]Route) []Route {
	out := make([]Route, 0, len(set))
	for _, r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func rel(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, path); err == nil {
			return r
		}
	}
	return path
}
