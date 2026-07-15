package config

import (
	"strings"
	"testing"
	"time"
)

const minimalValid = `
version: 1
model:
  base_url: https://api.example.com/v1
  name: test-model
  api_key:
    env: TERTIB_API_KEY
rules:
  - id: r1
    severity: error
    description: some rule
`

func TestParseMinimalAppliesDefaults(t *testing.T) {
	c, err := Parse([]byte(minimalValid))
	if err != nil {
		t.Fatal(err)
	}
	if c.Model.Timeout.Duration() != 60*time.Second {
		t.Errorf("timeout default = %v, want 60s", c.Model.Timeout.Duration())
	}
	if c.Model.MaxRetries != 3 {
		t.Errorf("max_retries default = %d, want 3", c.Model.MaxRetries)
	}
	if c.Model.MaxTokens != 2048 {
		t.Errorf("max_tokens default = %d, want 2048", c.Model.MaxTokens)
	}
	if c.Checks.FailOn != SeverityError {
		t.Errorf("fail_on default = %q, want error", c.Checks.FailOn)
	}
	if c.Checks.MaxFileKB != 200 {
		t.Errorf("max_file_kb default = %d, want 200", c.Checks.MaxFileKB)
	}
}

func TestParseDurationString(t *testing.T) {
	src := strings.Replace(minimalValid, "env: TERTIB_API_KEY", "env: TERTIB_API_KEY\n  timeout: 90s", 1)
	c, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if c.Model.Timeout.Duration() != 90*time.Second {
		t.Errorf("timeout = %v, want 90s", c.Model.Timeout.Duration())
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	src := minimalValid + "\nbogus_top_level: true\n"
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "wrong version",
			src:  strings.Replace(minimalValid, "version: 1", "version: 2", 1),
			want: "version must be 1",
		},
		{
			name: "missing base_url",
			src:  strings.Replace(minimalValid, "  base_url: https://api.example.com/v1\n", "", 1),
			want: "model.base_url is required",
		},
		{
			name: "no rules",
			src: `
version: 1
model:
  base_url: https://api.example.com/v1
  name: test-model
  api_key:
    env: X
rules: []
`,
			want: "at least one rule is required",
		},
		{
			name: "bad severity",
			src:  strings.Replace(minimalValid, "severity: error", "severity: critical", 1),
			want: "severity must be error|warning",
		},
		{
			name: "duplicate rule id",
			src: minimalValid + `  - id: r1
    severity: warning
    description: dup
`,
			want: "duplicate rule id",
		},
		{
			name: "api_key no source",
			src:  strings.Replace(minimalValid, "    env: TERTIB_API_KEY", "    {}", 1),
			want: "model.api_key",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src))
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestEmbeddedExampleIsValid(t *testing.T) {
	if _, err := Parse(Example); err != nil {
		t.Fatalf("embedded example config must be valid: %v", err)
	}
}
