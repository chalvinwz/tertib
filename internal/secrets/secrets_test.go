package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fakeSM is a stub AWS Secrets Manager client for tests.
type fakeSM struct {
	value string
	err   error
}

func (f fakeSM) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(f.value)}, nil
}

func newTestResolver(sm smAPI) (*Resolver, *Redactor) {
	r := NewRedactor()
	rv := NewResolver(r)
	rv.newSM = func(context.Context, string) (smAPI, error) { return sm, nil }
	return rv, r
}

func TestResolveEnv(t *testing.T) {
	t.Setenv("TERTIB_TEST_KEY", "sk-secret-123")
	rv, red := newTestResolver(nil)

	got, err := rv.Resolve(context.Background(), Ref{Env: "TERTIB_TEST_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-secret-123" {
		t.Errorf("got %q, want sk-secret-123", got)
	}
	if red.Redact("token=sk-secret-123 end") != "token="+mask+" end" {
		t.Error("resolved value should be redacted")
	}
}

func TestResolveEnvMissing(t *testing.T) {
	rv, _ := newTestResolver(nil)
	_, err := rv.Resolve(context.Background(), Ref{Env: "TERTIB_DEFINITELY_UNSET"})
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
}

func TestResolveEnvEmptyIsError(t *testing.T) {
	t.Setenv("TERTIB_EMPTY", "")
	rv, _ := newTestResolver(nil)
	if _, err := rv.Resolve(context.Background(), Ref{Env: "TERTIB_EMPTY"}); err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestResolveAWSPlainString(t *testing.T) {
	rv, _ := newTestResolver(fakeSM{value: "plain-secret"})
	got, err := rv.Resolve(context.Background(), Ref{
		AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "tertib/key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain-secret" {
		t.Errorf("got %q, want plain-secret", got)
	}
}

func TestResolveAWSJSONKey(t *testing.T) {
	rv, _ := newTestResolver(fakeSM{value: `{"api_key":"json-secret","other":"x"}`})
	got, err := rv.Resolve(context.Background(), Ref{
		AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "tertib/key", JSONKey: "api_key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "json-secret" {
		t.Errorf("got %q, want json-secret", got)
	}
}

func TestResolveAWSJSONKeyMissing(t *testing.T) {
	rv, _ := newTestResolver(fakeSM{value: `{"api_key":"x"}`})
	_, err := rv.Resolve(context.Background(), Ref{
		AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "tertib/key", JSONKey: "absent"},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestResolveAWSError(t *testing.T) {
	rv, _ := newTestResolver(fakeSM{err: errors.New("access denied")})
	_, err := rv.Resolve(context.Background(), Ref{
		AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "tertib/key"},
	})
	if err == nil {
		t.Fatal("expected error from AWS client")
	}
}

func TestRefValidate(t *testing.T) {
	cases := []struct {
		name    string
		ref     Ref
		wantErr bool
	}{
		{"env only", Ref{Env: "X"}, false},
		{"aws only", Ref{AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "s"}}, false},
		{"none", Ref{}, true},
		{"both", Ref{Env: "X", AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "s"}}, true},
		{"aws no secret_id", Ref{AWSSecretsManager: &AWSSecretsManagerRef{}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.ref.Validate(); (err != nil) != tc.wantErr {
				t.Errorf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestRedactMultipleAndEmpty(t *testing.T) {
	r := NewRedactor()
	r.Add("aaa")
	r.Add("bbb")
	r.Add("") // ignored
	out := r.Redact("aaa and bbb and ccc")
	if strings.Contains(out, "aaa") || strings.Contains(out, "bbb") {
		t.Errorf("secrets leaked: %q", out)
	}
	if !strings.Contains(out, "ccc") {
		t.Errorf("non-secret text should survive: %q", out)
	}
}

func TestResolveAWSJSONKeyNested(t *testing.T) {
	rv, _ := newTestResolver(fakeSM{value: `{"tertib":{"api_key":"nested-secret"},"other":"x"}`})
	got, err := rv.Resolve(context.Background(), Ref{
		AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "planpalasix/control/secrets", JSONKey: "tertib.api_key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "nested-secret" {
		t.Errorf("got %q, want nested-secret", got)
	}
}

func TestResolveAWSJSONKeyNestedNotObject(t *testing.T) {
	rv, _ := newTestResolver(fakeSM{value: `{"tertib":"flat"}`})
	_, err := rv.Resolve(context.Background(), Ref{
		AWSSecretsManager: &AWSSecretsManagerRef{SecretID: "s", JSONKey: "tertib.api_key"},
	})
	if err == nil {
		t.Fatal("expected error descending into non-object, got nil")
	}
}
