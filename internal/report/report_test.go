package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chalvinwz/tertib/internal/engine"
	"github.com/chalvinwz/tertib/internal/findings"
)

func TestMarkdownContainsSections(t *testing.T) {
	var buf bytes.Buffer
	r := &engine.Result{
		Findings: []findings.Finding{
			{RuleID: "handler-naming", Severity: "error", File: "h.go", Line: 12,
				Snippet: "func x() {}", Explanation: "bad name", Suggestion: "rename", Confidence: 0.9},
			{RuleID: "layering", Severity: "warning", File: "s.go", Line: 3, Explanation: "leaky", Confidence: 0.6},
			{RuleID: "layering", Severity: "warning", File: "old.go", Line: 99, Explanation: "pre", Context: true},
		},
		Skipped: []string{"big.bin (binary)"},
		Tasks:   3,
	}
	meta := Meta{Model: "m1", BaseRef: "main", Files: 2, Duration: 250 * time.Millisecond}
	if err := Markdown(&buf, r, meta); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"# tertib report",
		"1 error(s), 1 warning(s)", // headline (the context warning is counted separately)
		"## Errors",
		"## Warnings",
		"## Context notes",
		"handler-naming",
		"h.go:12",
		"**Suggestion:** rename",
		"big.bin (binary)",
		"model: m1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, out)
		}
	}
}

func TestMarkdownCleanPass(t *testing.T) {
	var buf bytes.Buffer
	if err := Markdown(&buf, &engine.Result{}, Meta{Model: "m", Files: 3}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "No violations found") {
		t.Errorf("clean run should say no violations:\n%s", out)
	}
	if strings.Contains(out, "## Errors") {
		t.Errorf("clean run should not render an Errors section:\n%s", out)
	}
}

func TestJSONShape(t *testing.T) {
	var buf bytes.Buffer
	r := &engine.Result{
		Findings: []findings.Finding{{RuleID: "r1", Severity: "error", File: "a.go", Line: 1, Explanation: "x"}},
		Tasks:    1,
	}
	if err := JSON(&buf, r, Meta{Model: "m1", BaseRef: "main", Files: 1, Duration: time.Second}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	meta := doc["meta"].(map[string]any)
	if meta["model"] != "m1" || meta["mode"] != "diff" {
		t.Errorf("meta = %+v", meta)
	}
	if meta["duration_ms"].(float64) != 1000 {
		t.Errorf("duration_ms = %v, want 1000", meta["duration_ms"])
	}
	fs := doc["findings"].([]any)
	if len(fs) != 1 {
		t.Errorf("want 1 finding in JSON, got %d", len(fs))
	}
}

func TestJSONEmptyFindingsIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, &engine.Result{}, Meta{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"findings": []`) {
		t.Errorf("empty findings should marshal as [], got:\n%s", buf.String())
	}
}
