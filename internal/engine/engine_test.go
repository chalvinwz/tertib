package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chalvinwz/tertib/internal/config"
	"github.com/chalvinwz/tertib/internal/gitdiff"
	"github.com/chalvinwz/tertib/internal/llm"
)

type fakeClient struct {
	resp string
	err  error
}

func (f *fakeClient) Complete(context.Context, string, string) (string, llm.Usage, error) {
	if f.err != nil {
		return "", llm.Usage{}, f.err
	}
	return f.resp, llm.Usage{TotalTokens: 1}, nil
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func baseConfig(rules ...config.Rule) *config.Config {
	return &config.Config{
		Version: 1,
		Rules:   rules,
		Checks:  config.Checks{FailOn: "error", MaxFileKB: 200},
	}
}

func TestRunProducesFindings(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "a.go", "package a\nvar x int\n")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "no vars"})
	e := New(cfg, &fakeClient{resp: `{"findings":[{"line":2,"explanation":"has a var","confidence":0.9}]}`})

	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "a.go", Status: "M"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(res.Findings))
	}
	f := res.Findings[0]
	if f.RuleID != "r1" || f.Severity != "error" || f.File != "a.go" || f.Line != 2 {
		t.Errorf("finding = %+v", f)
	}
	if res.Usage.TotalTokens != 1 {
		t.Errorf("usage total = %d, want 1", res.Usage.TotalTokens)
	}
}

func TestDiffModeDemotesOutsideHunks(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "a.go", "l1\nl2\nl3\nl4\nl5\n")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	e := New(cfg, &fakeClient{resp: `{"findings":[{"line":2,"explanation":"in hunk"},{"line":5,"explanation":"outside"}]}`})

	// Only line 2 changed.
	file := gitdiff.File{Path: "a.go", Status: "M", Hunks: []gitdiff.Hunk{{Start: 2, End: 2}}}
	res, err := e.Run(context.Background(), []gitdiff.File{file}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(res.Findings))
	}
	byLine := map[int]bool{}
	for _, f := range res.Findings {
		byLine[f.Line] = f.Context
	}
	if byLine[2] {
		t.Error("line 2 (in hunk) should NOT be a context note")
	}
	if !byLine[5] {
		t.Error("line 5 (outside hunk) should be a context note")
	}
}

func TestFullScanNoDemotion(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "a.go", "l1\nl2\n")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	e := New(cfg, &fakeClient{resp: `{"findings":[{"line":2,"explanation":"x"}]}`})

	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "a.go"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Findings[0].Context {
		t.Error("full-scan findings must never be context notes")
	}
}

func TestRulePathsScope(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "root.go", "x")
	writeFile(t, filepath.Join("src", "in.go"), "y")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Paths: []string{"src/**"}, Description: "d"})
	e := New(cfg, &fakeClient{resp: `{"findings":[{"line":1,"explanation":"x"}]}`})

	files := []gitdiff.File{{Path: "root.go"}, {Path: "src/in.go"}}
	res, err := e.Run(context.Background(), files, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks != 1 {
		t.Errorf("only src/in.go should match; tasks = %d", res.Tasks)
	}
	if len(res.Findings) != 1 || res.Findings[0].File != "src/in.go" {
		t.Errorf("finding should be on src/in.go, got %+v", res.Findings)
	}
}

func TestIgnoreExcludesFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "a.go", "x")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	cfg.Checks.Ignore = []string{"*.go"}
	e := New(cfg, &fakeClient{resp: `{"findings":[]}`})

	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "a.go"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks != 0 {
		t.Errorf("ignored file should produce no tasks, got %d", res.Tasks)
	}
}

func TestDeletedFileSkipped(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	e := New(cfg, &fakeClient{resp: `{"findings":[]}`})

	// No file on disk; status D means deleted and must be skipped without a
	// read error.
	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "gone.go", Status: "D"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks != 0 || len(res.Skipped) != 0 {
		t.Errorf("deleted file should be silently skipped; tasks=%d skipped=%v", res.Tasks, res.Skipped)
	}
}

func TestOversizeFileSkipped(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "big.go", strings.Repeat("x", 2048))

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	cfg.Checks.MaxFileKB = 1
	e := New(cfg, &fakeClient{resp: `{"findings":[]}`})

	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "big.go"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tasks != 0 {
		t.Errorf("oversize file should be skipped, tasks=%d", res.Tasks)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "exceeds") {
		t.Errorf("expected a skip note about size, got %v", res.Skipped)
	}
}

func TestMalformedResponseBecomesWarning(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "a.go", "x")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	e := New(cfg, &fakeClient{resp: "not json at all"})

	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "a.go"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		t.Errorf("malformed response should yield no findings")
	}
	if len(res.Warnings) != 1 {
		t.Errorf("malformed response should record one warning, got %v", res.Warnings)
	}
}

func TestClientErrorBecomesWarning(t *testing.T) {
	t.Chdir(t.TempDir())
	writeFile(t, "a.go", "x")

	cfg := baseConfig(config.Rule{ID: "r1", Severity: "error", Description: "d"})
	e := New(cfg, &fakeClient{err: errors.New("boom")})

	res, err := e.Run(context.Background(), []gitdiff.File{{Path: "a.go"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 {
		t.Errorf("client error should record one warning, got %v", res.Warnings)
	}
}
