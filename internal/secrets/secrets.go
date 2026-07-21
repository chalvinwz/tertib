// Package secrets resolves secret values from vendor-neutral sources declared
// in tertib configuration.
//
// A [Ref] names exactly one source. The environment-variable source keeps
// tertib CI-neutral: every CI system and secret store can inject an env var.
// The AWS Secrets Manager source fetches directly via the AWS SDK, using the
// standard credential chain (env keys, IAM role, OIDC web identity) so no
// credentials ever live in tertib config. Additional providers (Vault, GCP,
// Azure, 1Password) can be added behind the same [Ref] schema without breaking
// existing configs.
//
// Every resolved value is registered with a [Redactor] so it can be scrubbed
// from logs, error messages, and report output.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// DefaultTimeout bounds a single secret resolution so a wedged secret store
// or credential provider cannot hang the pipeline indefinitely.
const DefaultTimeout = 30 * time.Second

// Ref declares where a single secret value comes from. Exactly one source
// field must be set.
type Ref struct {
	Env               string                `yaml:"env,omitempty"`
	AWSSecretsManager *AWSSecretsManagerRef `yaml:"aws_secretsmanager,omitempty"`
}

// AWSSecretsManagerRef locates a secret in AWS Secrets Manager.
type AWSSecretsManagerRef struct {
	// SecretID is the secret name or full ARN.
	SecretID string `yaml:"secret_id"`
	// JSONKey, when set, selects one field from a secret whose value is a JSON
	// object (the common "store several keys in one secret" pattern). It may be
	// a dot-path to reach a nested field, e.g. "tertib.api_key"; a name with no
	// dots reads a top-level field. The selected value must be a string.
	JSONKey string `yaml:"json_key,omitempty"`
	// Region overrides the SDK default region for this lookup.
	Region string `yaml:"region,omitempty"`
}

// IsZero reports whether the ref declares no source at all. Callers use this
// to treat an omitted optional secret (e.g. a Discord webhook) as "disabled"
// rather than invalid.
func (r Ref) IsZero() bool {
	return r.Env == "" && r.AWSSecretsManager == nil
}

func (r Ref) sourceCount() int {
	n := 0
	if r.Env != "" {
		n++
	}
	if r.AWSSecretsManager != nil {
		n++
	}
	return n
}

// Validate checks that exactly one source is declared and that the chosen
// source has the fields it needs.
func (r Ref) Validate() error {
	switch r.sourceCount() {
	case 0:
		return errors.New("no source declared (set env or aws_secretsmanager)")
	case 1:
		// ok
	default:
		return errors.New("multiple sources declared; set exactly one")
	}
	if r.AWSSecretsManager != nil && strings.TrimSpace(r.AWSSecretsManager.SecretID) == "" {
		return errors.New("aws_secretsmanager.secret_id is required")
	}
	return nil
}

// smAPI is the slice of the AWS Secrets Manager client that tertib uses. Keeping
// it an interface lets tests inject a fake without real AWS calls.
type smAPI interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// Resolver fetches secret values and records them for redaction.
type Resolver struct {
	Timeout  time.Duration
	Redactor *Redactor
	// newSM builds an AWS Secrets Manager client for a region. Overridable in
	// tests; defaults to the real SDK client.
	newSM func(ctx context.Context, region string) (smAPI, error)
}

// NewResolver returns a Resolver wired to the real AWS SDK. Resolved values are
// registered with the given redactor (a nil redactor is tolerated).
func NewResolver(r *Redactor) *Resolver {
	return &Resolver{Timeout: DefaultTimeout, Redactor: r, newSM: defaultNewSM}
}

// Resolve fetches the value named by ref. It errors if the ref is invalid, the
// source lookup fails, or the resolved value is empty.
func (rv *Resolver) Resolve(ctx context.Context, ref Ref) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, rv.Timeout)
	defer cancel()

	var (
		val string
		err error
	)
	switch {
	case ref.Env != "":
		val, err = resolveEnv(ref.Env)
	case ref.AWSSecretsManager != nil:
		val, err = rv.resolveAWS(ctx, *ref.AWSSecretsManager)
	}
	if err != nil {
		return "", err
	}
	if val == "" {
		return "", errors.New("resolved secret is empty")
	}
	if rv.Redactor != nil {
		rv.Redactor.Add(val)
	}
	return val, nil
}

func resolveEnv(name string) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return v, nil
}

func (rv *Resolver) resolveAWS(ctx context.Context, ref AWSSecretsManagerRef) (string, error) {
	client, err := rv.newSM(ctx, ref.Region)
	if err != nil {
		return "", fmt.Errorf("load AWS config: %w", err)
	}
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(ref.SecretID),
	})
	if err != nil {
		return "", fmt.Errorf("get secret %q: %w", ref.SecretID, err)
	}
	if out.SecretString == nil {
		return "", fmt.Errorf("secret %q has no string value (binary secrets are unsupported)", ref.SecretID)
	}
	raw := *out.SecretString
	if ref.JSONKey == "" {
		return raw, nil
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return "", fmt.Errorf("secret %q is not a JSON object but json_key %q was requested: %w", ref.SecretID, ref.JSONKey, err)
	}
	// json_key may be a dot-path into nested objects, e.g. "tertib.api_key".
	// A single segment (no dots) reads a top-level field.
	segments := strings.Split(ref.JSONKey, ".")
	var cur any = obj
	for i, seg := range segments {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("json_key %q in secret %q: %q is not a JSON object", ref.JSONKey, ref.SecretID, strings.Join(segments[:i], "."))
		}
		cur, ok = m[seg]
		if !ok {
			return "", fmt.Errorf("json_key %q not found in secret %q", ref.JSONKey, ref.SecretID)
		}
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("json_key %q in secret %q is not a string", ref.JSONKey, ref.SecretID)
	}
	return s, nil
}

func defaultNewSM(ctx context.Context, region string) (smAPI, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return secretsmanager.NewFromConfig(cfg), nil
}

// Redactor scrubs known secret values out of arbitrary text. It is safe for
// concurrent use.
type Redactor struct {
	mu     sync.RWMutex
	values []string
}

// NewRedactor returns an empty redactor.
func NewRedactor() *Redactor { return &Redactor{} }

const mask = "***REDACTED***"

// Add registers a secret value to be scrubbed by [Redactor.Redact].
func (r *Redactor) Add(v string) {
	if v == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, v)
}

// Redact replaces every registered secret value found in s with a fixed mask.
func (r *Redactor) Redact(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.values {
		if v != "" {
			s = strings.ReplaceAll(s, v, mask)
		}
	}
	return s
}
