package findings

import "testing"

func TestFailsGate(t *testing.T) {
	fs := []Finding{
		{RuleID: "a", Severity: SeverityWarning},
		{RuleID: "b", Severity: SeverityError, Context: true}, // demoted, ignored
	}
	cases := []struct {
		failOn string
		want   bool
	}{
		{SeverityError, false},  // only a warning (non-context) present
		{SeverityWarning, true}, // warning trips the warning threshold
		{"never", false},        // never fails
	}
	for _, tc := range cases {
		if got := FailsGate(fs, tc.failOn); got != tc.want {
			t.Errorf("FailsGate(failOn=%s) = %v, want %v", tc.failOn, got, tc.want)
		}
	}
}

func TestFailsGateErrorTripsError(t *testing.T) {
	fs := []Finding{{Severity: SeverityError}}
	if !FailsGate(fs, SeverityError) {
		t.Error("an error finding must fail a fail_on=error gate")
	}
}

func TestContextNeverFails(t *testing.T) {
	fs := []Finding{{Severity: SeverityError, Context: true}}
	if FailsGate(fs, SeverityError) {
		t.Error("a context finding must never fail the gate")
	}
}

func TestCount(t *testing.T) {
	fs := []Finding{
		{Severity: SeverityError},
		{Severity: SeverityWarning},
		{Severity: SeverityWarning},
		{Severity: SeverityError, Context: true},
	}
	c := Count(fs)
	if c.Errors != 1 || c.Warnings != 2 || c.Context != 1 {
		t.Errorf("Count = %+v, want {Errors:1 Warnings:2 Context:1}", c)
	}
}

func TestSortOrder(t *testing.T) {
	fs := []Finding{
		{RuleID: "z", Severity: SeverityWarning, File: "b.go", Line: 5},
		{RuleID: "y", Severity: SeverityError, Context: true, File: "a.go", Line: 1},
		{RuleID: "x", Severity: SeverityError, File: "a.go", Line: 9},
		{RuleID: "w", Severity: SeverityError, File: "a.go", Line: 2},
	}
	Sort(fs)
	// Expect: errors (a.go:2, a.go:9), then warning (b.go:5), then context last.
	if fs[0].Line != 2 || fs[1].Line != 9 {
		t.Errorf("errors not ordered by line: %+v", fs)
	}
	if fs[2].Severity != SeverityWarning {
		t.Errorf("warning should follow errors: %+v", fs)
	}
	if !fs[3].Context {
		t.Errorf("context finding should sort last: %+v", fs)
	}
}
