// Package report renders an engine result as a human-readable markdown report
// or machine-readable JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/chalvinwz/tertib/internal/engine"
	"github.com/chalvinwz/tertib/internal/findings"
)

// Meta carries run context that is not part of the findings themselves.
type Meta struct {
	Model    string
	BaseRef  string // empty in full-scan mode
	AllMode  bool
	Files    int
	Duration time.Duration
}

// Markdown writes a report suitable for a CI step summary or PR comment.
func Markdown(w io.Writer, r *engine.Result, m Meta) error {
	bw := &errWriter{w: w}
	counts := findings.Count(r.Findings)

	bw.printf("# tertib report\n\n")
	bw.printf("%s\n\n", headline(counts, m))

	bw.printf("## Summary\n\n")
	bw.printf("| Rule | Severity | Count |\n|------|----------|-------|\n")
	for _, row := range ruleRows(r.Findings) {
		bw.printf("| %s | %s | %d |\n", row.rule, row.severity, row.count)
	}
	if len(r.Findings) == 0 {
		bw.printf("| _(none)_ | | 0 |\n")
	}
	bw.printf("\n")

	writeSection(bw, "Errors", filter(r.Findings, func(f findings.Finding) bool {
		return !f.Context && f.Severity == findings.SeverityError
	}))
	writeSection(bw, "Warnings", filter(r.Findings, func(f findings.Finding) bool {
		return !f.Context && f.Severity == findings.SeverityWarning
	}))
	writeSection(bw, "Context notes (outside changed lines)", filter(r.Findings, func(f findings.Finding) bool {
		return f.Context
	}))

	bw.printf("---\n\n")
	bw.printf("%s\n", footer(r, m))
	if len(r.Skipped) > 0 {
		bw.printf("\nSkipped files:\n")
		for _, s := range r.Skipped {
			bw.printf("- %s\n", s)
		}
	}
	if len(r.Warnings) > 0 {
		bw.printf("\nWarnings:\n")
		for _, wn := range r.Warnings {
			bw.printf("- %s\n", wn)
		}
	}
	return bw.err
}

// JSON writes the full result as JSON for scripting and tooling.
func JSON(w io.Writer, r *engine.Result, m Meta) error {
	counts := findings.Count(r.Findings)
	doc := jsonDoc{
		Meta: jsonMeta{
			Model:       m.Model,
			BaseRef:     m.BaseRef,
			Mode:        modeString(m.AllMode),
			Files:       m.Files,
			DurationMS:  m.Duration.Milliseconds(),
			Tasks:       r.Tasks,
			TokensTotal: r.Usage.TotalTokens,
		},
		Summary:  jsonSummary{Errors: counts.Errors, Warnings: counts.Warnings, Context: counts.Context},
		Findings: r.Findings,
		Skipped:  r.Skipped,
		Warnings: r.Warnings,
	}
	if doc.Findings == nil {
		doc.Findings = []findings.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

type jsonDoc struct {
	Meta     jsonMeta           `json:"meta"`
	Summary  jsonSummary        `json:"summary"`
	Findings []findings.Finding `json:"findings"`
	Skipped  []string           `json:"skipped,omitempty"`
	Warnings []string           `json:"warnings,omitempty"`
}

type jsonMeta struct {
	Model       string `json:"model"`
	BaseRef     string `json:"base_ref,omitempty"`
	Mode        string `json:"mode"`
	Files       int    `json:"files"`
	DurationMS  int64  `json:"duration_ms"`
	Tasks       int    `json:"tasks"`
	TokensTotal int    `json:"tokens_total"`
}

type jsonSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Context  int `json:"context"`
}

func headline(c findings.Counts, m Meta) string {
	if c.Errors == 0 && c.Warnings == 0 {
		return fmt.Sprintf("**No violations found** across %d file(s).", m.Files)
	}
	return fmt.Sprintf("**%d error(s), %d warning(s)** across %d file(s) (%d context note(s)).",
		c.Errors, c.Warnings, m.Files, c.Context)
}

func footer(r *engine.Result, m Meta) string {
	return fmt.Sprintf("model: %s · mode: %s · %d task(s) · tokens: %d prompt / %d completion / %d total · %s",
		m.Model, modeString(m.AllMode), r.Tasks,
		r.Usage.PromptTokens, r.Usage.CompletionTokens, r.Usage.TotalTokens,
		m.Duration.Round(time.Millisecond))
}

func modeString(all bool) string {
	if all {
		return "all"
	}
	return "diff"
}

type row struct {
	rule     string
	severity string
	count    int
}

func ruleRows(fs []findings.Finding) []row {
	type key struct{ rule, sev string }
	counts := map[key]int{}
	var order []key
	for _, f := range fs {
		sev := f.Severity
		if f.Context {
			sev += " (context)"
		}
		k := key{f.RuleID, sev}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].rule != order[j].rule {
			return order[i].rule < order[j].rule
		}
		return order[i].sev < order[j].sev
	})
	rows := make([]row, 0, len(order))
	for _, k := range order {
		rows = append(rows, row{rule: k.rule, severity: k.sev, count: counts[k]})
	}
	return rows
}

func writeSection(bw *errWriter, title string, fs []findings.Finding) {
	if len(fs) == 0 {
		return
	}
	bw.printf("## %s\n\n", title)
	for _, f := range fs {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		bw.printf("### `%s` — %s\n\n", f.RuleID, loc)
		bw.printf("%s\n\n", f.Explanation)
		if f.Snippet != "" {
			bw.printf("```\n%s\n```\n\n", f.Snippet)
		}
		if f.Suggestion != "" {
			bw.printf("**Suggestion:** %s\n\n", f.Suggestion)
		}
		bw.printf("_confidence: %.2f_\n\n", f.Confidence)
	}
}

func filter(fs []findings.Finding, keep func(findings.Finding) bool) []findings.Finding {
	var out []findings.Finding
	for _, f := range fs {
		if keep(f) {
			out = append(out, f)
		}
	}
	return out
}

// errWriter defers write-error handling so the render functions stay readable.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
