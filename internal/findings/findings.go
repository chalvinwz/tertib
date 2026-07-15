// Package findings defines the violation type tertib produces and the severity
// logic that decides whether a run should fail CI.
package findings

import "sort"

// Severity levels, matching the config vocabulary.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Finding is a single rule violation reported against a file.
type Finding struct {
	RuleID      string  `json:"rule_id"`
	Severity    string  `json:"severity"`
	File        string  `json:"file"`
	Line        int     `json:"line"`
	Snippet     string  `json:"snippet,omitempty"`
	Explanation string  `json:"explanation"`
	Suggestion  string  `json:"suggestion,omitempty"`
	Confidence  float64 `json:"confidence"`
	// Context marks a finding that falls outside the changed lines in diff
	// mode. Context findings are informational and never fail the gate.
	Context bool `json:"context"`
}

func rank(severity string) int {
	switch severity {
	case SeverityError:
		return 2
	case SeverityWarning:
		return 1
	default:
		return 0
	}
}

// FailsGate reports whether any non-context finding meets or exceeds the
// fail_on threshold. failOn of "never" never fails.
func FailsGate(fs []Finding, failOn string) bool {
	if failOn == "never" {
		return false
	}
	threshold := rank(failOn)
	for _, f := range fs {
		if f.Context {
			continue
		}
		if rank(f.Severity) >= threshold {
			return true
		}
	}
	return false
}

// Counts summarizes a finding set for reporting.
type Counts struct {
	Errors   int
	Warnings int
	Context  int
}

// Count tallies findings by severity, separating demoted context findings.
func Count(fs []Finding) Counts {
	var c Counts
	for _, f := range fs {
		switch {
		case f.Context:
			c.Context++
		case f.Severity == SeverityError:
			c.Errors++
		case f.Severity == SeverityWarning:
			c.Warnings++
		}
	}
	return c
}

// Sort orders findings deterministically: real findings before context notes,
// errors before warnings, then by file and line. This keeps reports stable.
func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Context != b.Context {
			return !a.Context // real findings first
		}
		if ra, rb := rank(a.Severity), rank(b.Severity); ra != rb {
			return ra > rb // higher severity first
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}
