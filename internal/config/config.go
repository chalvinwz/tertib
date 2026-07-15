// Package config loads, validates, and applies defaults to the tertib
// configuration file (.tertib.yml). Validation is strict — unknown keys and
// missing required fields are reported precisely so `tertib validate` can guide
// users to a correct config.
package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chalvinwz/tertib/internal/secrets"
	"gopkg.in/yaml.v3"
)

// CurrentVersion is the config schema version tertib understands.
const CurrentVersion = 1

// DefaultPath is the config file tertib looks for when --config is not given.
const DefaultPath = ".tertib.yml"

// Example is the commented starter config written by `tertib init`.
//
//go:embed tertib.example.yml
var Example []byte

// Config is the root of a parsed .tertib.yml.
type Config struct {
	Version int `yaml:"version"`
	// EnvFile optionally names a KEY=VALUE file whose variables are loaded
	// before secrets are resolved. Missing files are ignored (convenient for
	// local development); already-set environment variables are never
	// overwritten. Overridden by the --env-file flag.
	EnvFile string `yaml:"env_file"`
	Model   Model  `yaml:"model"`
	Checks  Checks `yaml:"checks"`
	Rules   []Rule `yaml:"rules"`
	Output  Output `yaml:"output"`
}

// Model configures the OpenAI-compatible endpoint tertib calls.
type Model struct {
	BaseURL     string      `yaml:"base_url"`
	Name        string      `yaml:"name"`
	APIKey      secrets.Ref `yaml:"api_key"`
	Temperature float64     `yaml:"temperature"`
	Timeout     Duration    `yaml:"timeout"`
	MaxRetries  int         `yaml:"max_retries"`
	// MaxTokens caps the output tokens per request. It bounds cost (important
	// when an endpoint enforces a budget) and is a standard OpenAI parameter.
	MaxTokens int `yaml:"max_tokens"`
}

// Checks holds cross-cutting evaluation settings.
type Checks struct {
	FailOn    string   `yaml:"fail_on"`
	Ignore    []string `yaml:"ignore"`
	MaxFileKB int      `yaml:"max_file_kb"`
}

// Rule is one convention expressed in plain language.
type Rule struct {
	ID          string   `yaml:"id"`
	Severity    string   `yaml:"severity"`
	Paths       []string `yaml:"paths"`
	Description string   `yaml:"description"`
}

// Output configures optional result delivery beyond stdout.
type Output struct {
	Notify Notify `yaml:"notify"`
}

// Notify configures optional notifiers.
type Notify struct {
	DiscordWebhook secrets.Ref `yaml:"discord_webhook"`
}

// Duration is a time.Duration that unmarshals from a Go duration string such
// as "60s" or "2m".
type Duration time.Duration

// UnmarshalYAML parses a duration string like "60s".
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the value as a time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Load reads and validates the config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes, defaults, and validates config bytes. Unknown keys are
// rejected so typos surface as errors instead of being silently ignored.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Model.Timeout == 0 {
		c.Model.Timeout = Duration(60 * time.Second)
	}
	if c.Model.MaxRetries == 0 {
		c.Model.MaxRetries = 3
	}
	if c.Model.MaxTokens == 0 {
		c.Model.MaxTokens = 2048
	}
	if c.Checks.FailOn == "" {
		c.Checks.FailOn = SeverityError
	}
	if c.Checks.MaxFileKB == 0 {
		c.Checks.MaxFileKB = 200
	}
}

// Severity levels and the fail-on sentinel.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	FailOnNever     = "never"
)

var validSeverity = map[string]bool{SeverityError: true, SeverityWarning: true}
var validFailOn = map[string]bool{SeverityError: true, SeverityWarning: true, FailOnNever: true}

// Validate reports every problem it finds in one error, so users can fix the
// whole config in a single pass.
func (c *Config) Validate() error {
	var errs []string

	if c.Version != CurrentVersion {
		errs = append(errs, fmt.Sprintf("version must be %d, got %d", CurrentVersion, c.Version))
	}
	if strings.TrimSpace(c.Model.BaseURL) == "" {
		errs = append(errs, "model.base_url is required")
	}
	if strings.TrimSpace(c.Model.Name) == "" {
		errs = append(errs, "model.name is required")
	}
	if err := c.Model.APIKey.Validate(); err != nil {
		errs = append(errs, "model.api_key: "+err.Error())
	}
	if !validFailOn[c.Checks.FailOn] {
		errs = append(errs, fmt.Sprintf("checks.fail_on must be error|warning|never, got %q", c.Checks.FailOn))
	}

	if len(c.Rules) == 0 {
		errs = append(errs, "at least one rule is required")
	}
	seen := make(map[string]bool, len(c.Rules))
	for i, r := range c.Rules {
		if strings.TrimSpace(r.ID) == "" {
			errs = append(errs, fmt.Sprintf("rules[%d].id is required", i))
			continue
		}
		if seen[r.ID] {
			errs = append(errs, fmt.Sprintf("duplicate rule id %q", r.ID))
		}
		seen[r.ID] = true
		if !validSeverity[r.Severity] {
			errs = append(errs, fmt.Sprintf("rule %q: severity must be error|warning, got %q", r.ID, r.Severity))
		}
		if strings.TrimSpace(r.Description) == "" {
			errs = append(errs, fmt.Sprintf("rule %q: description is required", r.ID))
		}
	}

	if !c.Output.Notify.DiscordWebhook.IsZero() {
		if err := c.Output.Notify.DiscordWebhook.Validate(); err != nil {
			errs = append(errs, "output.notify.discord_webhook: "+err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
