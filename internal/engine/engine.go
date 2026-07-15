// Package engine orchestrates convention checking: it matches rules to files,
// evaluates each rule/file pair against the model concurrently, and demotes
// findings that fall outside the changed lines in diff mode.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/chalvinwz/tertib/internal/config"
	"github.com/chalvinwz/tertib/internal/findings"
	"github.com/chalvinwz/tertib/internal/gitdiff"
	"github.com/chalvinwz/tertib/internal/llm"
)

// defaultConcurrency bounds simultaneous model requests.
const defaultConcurrency = 4

// LLMClient is the slice of the model client the engine needs. An interface
// keeps the engine testable without real network calls.
type LLMClient interface {
	Complete(ctx context.Context, system, user string) (string, llm.Usage, error)
}

// Engine evaluates a config's rules against a set of files.
type Engine struct {
	cfg         *config.Config
	client      LLMClient
	Concurrency int
}

// New builds an engine for the given config and model client.
func New(cfg *config.Config, client LLMClient) *Engine {
	return &Engine{cfg: cfg, client: client, Concurrency: defaultConcurrency}
}

// Result is the outcome of a run.
type Result struct {
	Findings []findings.Finding
	Usage    llm.Usage
	Skipped  []string // files not reviewed, with the reason
	Warnings []string // per-task failures that did not abort the run
	Tasks    int      // number of rule/file evaluations attempted
}

type task struct {
	rule    config.Rule
	file    gitdiff.File
	content string
}

// Run evaluates every rule against every matching file. In diff mode, findings
// outside the changed hunks are demoted to context notes. The run continues
// past individual task failures, recording them as warnings; it returns an
// error only if the context is cancelled.
func (e *Engine) Run(ctx context.Context, files []gitdiff.File, diffMode bool) (*Result, error) {
	res := &Result{}
	candidates := e.readCandidates(files, res)
	tasks := e.buildTasks(candidates)
	res.Tasks = len(tasks)
	if len(tasks) == 0 {
		return res, nil
	}

	conc := e.Concurrency
	if conc < 1 {
		conc = defaultConcurrency
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, t := range tasks {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(t task) {
			defer wg.Done()
			defer func() { <-sem }()

			fs, usage, err := e.evalOne(ctx, t, diffMode)
			mu.Lock()
			defer mu.Unlock()
			res.Usage = res.Usage.Add(usage)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("rule %q on %s: %v", t.rule.ID, t.file.Path, err))
				return
			}
			res.Findings = append(res.Findings, fs...)
		}(t)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return res, err
	}
	findings.Sort(res.Findings)
	return res, nil
}

// fileData caches a candidate file's content so it is read once and reused
// across every rule that matches it.
type fileData struct {
	file    gitdiff.File
	content string
}

func (e *Engine) readCandidates(files []gitdiff.File, res *Result) []fileData {
	ignore := newMatcher(e.cfg.Checks.Ignore)
	maxBytes := e.cfg.Checks.MaxFileKB * 1024

	var out []fileData
	for _, f := range files {
		if f.Status == "D" {
			continue // deleted files have no new content to review
		}
		if ignore.matchAny(f.Path) {
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s (read error: %v)", f.Path, err))
			continue
		}
		if maxBytes > 0 && len(data) > maxBytes {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s (exceeds %d KB)", f.Path, e.cfg.Checks.MaxFileKB))
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			res.Skipped = append(res.Skipped, f.Path+" (binary)")
			continue
		}
		out = append(out, fileData{file: f, content: string(data)})
	}
	return out
}

func (e *Engine) buildTasks(candidates []fileData) []task {
	var tasks []task
	for _, rule := range e.cfg.Rules {
		var paths *matcher
		if len(rule.Paths) > 0 {
			paths = newMatcher(rule.Paths)
		}
		for _, c := range candidates {
			if paths != nil && !paths.matchAny(c.file.Path) {
				continue
			}
			tasks = append(tasks, task{rule: rule, file: c.file, content: c.content})
		}
	}
	return tasks
}

type modelFinding struct {
	Line        int     `json:"line"`
	Snippet     string  `json:"snippet"`
	Explanation string  `json:"explanation"`
	Suggestion  string  `json:"suggestion"`
	Confidence  float64 `json:"confidence"`
}

type modelResponse struct {
	Findings []modelFinding `json:"findings"`
}

func (e *Engine) evalOne(ctx context.Context, t task, diffMode bool) ([]findings.Finding, llm.Usage, error) {
	user := buildUserPrompt(t.rule, t.file.Path, t.content)
	content, usage, err := e.client.Complete(ctx, systemPrompt, user)
	if err != nil {
		return nil, usage, err
	}
	raw, err := llm.ExtractJSON(content)
	if err != nil {
		return nil, usage, err
	}
	var mr modelResponse
	if err := json.Unmarshal([]byte(raw), &mr); err != nil {
		return nil, usage, fmt.Errorf("parse model findings: %w", err)
	}

	out := make([]findings.Finding, 0, len(mr.Findings))
	for _, mf := range mr.Findings {
		f := findings.Finding{
			RuleID:      t.rule.ID,
			Severity:    t.rule.Severity,
			File:        t.file.Path,
			Line:        mf.Line,
			Snippet:     trimSnippet(mf.Snippet),
			Explanation: mf.Explanation,
			Suggestion:  mf.Suggestion,
			Confidence:  mf.Confidence,
		}
		if diffMode {
			// A finding we cannot place inside a changed hunk is informational,
			// so unrelated pre-existing code never fails a PR.
			f.Context = !lineInHunks(mf.Line, t.file.Hunks)
		}
		out = append(out, f)
	}
	return out, usage, nil
}

func lineInHunks(line int, hunks []gitdiff.Hunk) bool {
	if line <= 0 {
		return false
	}
	for _, h := range hunks {
		if h.Contains(line) {
			return true
		}
	}
	return false
}

func trimSnippet(s string) string {
	const max = 200
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
