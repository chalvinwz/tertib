package ci

import "testing"

func TestBaseRefPriority(t *testing.T) {
	// Clear all so leftover CI env from the host doesn't leak in.
	for _, e := range baseRefEnvs {
		t.Setenv(e, "")
	}
	if got := BaseRef(); got != "" {
		t.Errorf("BaseRef with no vars = %q, want empty", got)
	}

	t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_NAME", "develop")
	if got := BaseRef(); got != "develop" {
		t.Errorf("BaseRef = %q, want develop", got)
	}

	// GitHub var has higher priority.
	t.Setenv("GITHUB_BASE_REF", "main")
	if got := BaseRef(); got != "main" {
		t.Errorf("BaseRef = %q, want main (higher priority)", got)
	}
}

func TestIsCI(t *testing.T) {
	t.Setenv("CI", "")
	if IsCI() {
		t.Error("IsCI should be false when CI is empty")
	}
	t.Setenv("CI", "true")
	if !IsCI() {
		t.Error("IsCI should be true when CI is set")
	}
}
