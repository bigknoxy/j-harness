package docscheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func TestRoutesDocumented(t *testing.T) {
	root := repoRoot(t)
	src, err := SourceRoutes(filepath.Join(root, "internal", "api"))
	if err != nil {
		t.Fatalf("read source routes: %v", err)
	}
	if len(src) == 0 {
		t.Fatal("no routes parsed from internal/api; the parser is broken")
	}

	docs := []string{"README.md", "docs/API.md"}
	srcSet := map[string]bool{}
	for _, r := range src {
		srcSet[r.String()] = true
	}

	// Every route must be named in each doc surface, and no doc may name a
	// route the server does not serve.
	docSet := map[string]map[string]bool{}
	for _, d := range docs {
		got, err := DocumentedRoutes(filepath.Join(root, d))
		if err != nil {
			t.Fatalf("parse %s: %v", d, err)
		}
		docSet[d] = map[string]bool{}
		for _, r := range got {
			docSet[d][r.String()] = true
			if !srcSet[r.String()] {
				t.Errorf("%s documents stale route %q: not registered in internal/api", d, r.String())
			}
		}
	}

	for _, r := range src {
		for _, d := range docs {
			if !docSet[d][r.String()] {
				t.Errorf("route %q is registered in internal/api but missing from %s", r.String(), d)
			}
		}
	}
}

func TestEnvVarsDocumented(t *testing.T) {
	root := repoRoot(t)
	src, err := SourceEnvVars(root)
	if err != nil {
		t.Fatalf("read source env vars: %v", err)
	}
	if len(src) == 0 {
		t.Fatal("no environment variables parsed from Go source")
	}

	docPaths := []string{filepath.Join(root, "docs", "API.md"), filepath.Join(root, "docs", "DEPLOY.md")}
	docs, err := DocumentedEnvVars(docPaths...)
	if err != nil {
		t.Fatalf("read documented env vars: %v", err)
	}
	docSet := map[string]bool{}
	for _, v := range docs {
		docSet[v] = true
	}

	for _, v := range src {
		if !docSet[v] {
			t.Errorf("env var %q is read in Go source but missing from docs/API.md and docs/DEPLOY.md", v)
		}
	}
	srcSet := map[string]bool{}
	for _, v := range src {
		srcSet[v] = true
	}
	for _, v := range docs {
		if !srcSet[v] && !documentedAllowlist[v] {
			t.Errorf("docs document env var %q but no Go source reads it (stale, or add it to documentedAllowlist with a reason)", v)
		}
	}
}

func TestMetricsDocumented(t *testing.T) {
	root := repoRoot(t)
	src, err := SourceMetrics(filepath.Join(root, "internal", "metrics"))
	if err != nil {
		t.Fatalf("read source metrics: %v", err)
	}
	if len(src) == 0 {
		t.Fatal("no metric counters parsed from internal/metrics")
	}
	docs, err := DocumentedMetrics(filepath.Join(root, "docs", "API.md"))
	if err != nil {
		t.Fatalf("read documented metrics: %v", err)
	}
	docSet := map[string]bool{}
	for _, m := range docs {
		docSet[m] = true
	}
	for _, m := range src {
		if !docSet[m] {
			t.Errorf("metric %q is declared in internal/metrics but missing from the docs/API.md counters table", m)
		}
	}
	srcSet := map[string]bool{}
	for _, m := range src {
		srcSet[m] = true
	}
	for _, m := range docs {
		if !srcSet[m] {
			t.Errorf("docs/API.md documents metric %q but internal/metrics does not declare it", m)
		}
	}
}

// TestVersionPinConsistent keeps the README install pin and the DEPLOY image
// build argument on the same release. It avoids git tags (not guaranteed in a
// shallow CI checkout) and is fully deterministic.
func TestVersionPinConsistent(t *testing.T) {
	root := repoRoot(t)
	readme, err := VersionPin(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("README version pin: %v", err)
	}
	deploy, err := VersionPin(filepath.Join(root, "docs", "DEPLOY.md"))
	if err != nil {
		t.Fatalf("DEPLOY version pin: %v", err)
	}
	norm := func(v string) string { return strings.TrimPrefix(v, "v") }
	if norm(readme) != norm(deploy) {
		t.Errorf("version drift: README.md pins VERSION=%s but docs/DEPLOY.md builds VERSION=%s", readme, deploy)
	}
	if !semverRe.MatchString(norm(readme)) {
		t.Errorf("README.md version pin %q is not a bare semver like v0.1.0", readme)
	}
}

// TestRelativeLinks checks that every relative markdown link resolves to a real
// file and that any #anchor matches a heading. Offline and deterministic.
func TestRelativeLinks(t *testing.T) {
	root := repoRoot(t)
	files, err := MarkdownFiles(root)
	if err != nil {
		t.Fatalf("list markdown files: %v", err)
	}
	for _, f := range files {
		problems, err := CheckLinks(f)
		if err != nil {
			t.Fatalf("check links in %s: %v", f, err)
		}
		for _, p := range problems {
			t.Error(p)
		}
	}
}

// TestDocsASCIIOnly enforces the repo documentation rule: no em-dashes, no
// emoji, no smart quotes. Any non-ASCII byte in a documentation surface fails
// with the file name.
func TestDocsASCIIOnly(t *testing.T) {
	root := repoRoot(t)
	surfaces := []string{"README.md", "index.html", "AGENTS.md"}
	for _, g := range []string{"docs/*.md", "docs/research/*.md", "tasks/*.md"} {
		matches, err := filepath.Glob(filepath.Join(root, g))
		if err != nil {
			t.Fatalf("glob %s: %v", g, err)
		}
		for _, m := range matches {
			surfaces = append(surfaces, strings.TrimPrefix(m, root+string(filepath.Separator)))
		}
	}
	for _, s := range surfaces {
		b, err := os.ReadFile(filepath.Join(root, s))
		if err != nil {
			t.Fatalf("read %s: %v", s, err)
		}
		for i, r := range string(b) {
			if r > 127 {
				t.Errorf("%s contains non-ASCII rune %q (U+%04X) at byte %d; docs must be ASCII-only", s, r, r, i)
				break
			}
		}
	}
}

// documentedAllowlist holds env vars that are intentionally documented even
// though their read site is not caught by the source regex. Keep this empty if
// possible; accuracy is the point.
var documentedAllowlist = map[string]bool{}
