# tertib

**Enforce your team's code conventions in CI — with AI, from a plain-language YAML file.**

Linters catch what an AST rule can express. tertib catches the rest: the
conventions you write down in wikis and `CONTRIBUTING.md` but can't encode as a
rule — naming styles, file-structure expectations, layering boundaries ("handlers
must not contain business logic"). You describe each convention in plain language;
tertib sends the changed code to an AI model and reports any violations, failing
CI when it matters.

- **Vendor-neutral.** Any CI (GitHub, GitLab, Bitbucket, Jenkins, …). Any model
  behind an OpenAI-compatible endpoint — a hosted gateway, a self-hosted proxy,
  or a local model. No lock-in.
- **Language-agnostic.** Rules are prose, not per-language parsers. tertib reads
  whatever your repo contains.
- **Diff-first.** By default it reviews only what changed on the branch, so PR
  checks stay fast and cheap. `--all` scans everything.
- **Single static binary** (plus a Docker image). Drop it into a pipeline in two
  lines.

> **Status:** pre-1.0 but functional end-to-end — `init`, `validate`, and the
> full `check` pipeline (config → secrets → diff → AI evaluation → report →
> gate) all work today. APIs and config may still shift before 1.0.

## How it works

```
.tertib.yml (your conventions)
        │
        ▼
  tertib check ──► git diff vs base ──► changed files
        │                                   │
        │           per rule, matched files │
        ▼                                   ▼
  resolve secrets                   OpenAI-compatible model endpoint
  (env / AWS SM)                            │
        │                        structured JSON findings
        ▼                                   │
   markdown / JSON report ◄─────────────────┘
        │
        ▼
   exit code: 0 pass · 1 violations · 2 error
```

## Install

```sh
# From source
go install github.com/chalvinwz/tertib/cmd/tertib@latest

# Or grab a release binary from the Releases page, or use the Docker image.
```

## Quickstart

```sh
tertib init            # writes a commented .tertib.yml
# edit .tertib.yml to describe your conventions
tertib validate        # sanity-check the config
export TERTIB_API_KEY=...        # your model endpoint's key
tertib check --base main         # review changes vs main
```

## Configuration

`tertib init` writes a fully commented starter file. The essentials:

```yaml
version: 1

model:
  base_url: https://your-gateway.example.com/v1   # any OpenAI-compatible endpoint
  name: your-model-name
  api_key:
    env: TERTIB_API_KEY            # see "Secrets" below

checks:
  fail_on: error                  # error | warning | never
  ignore: ["vendor/**", "**/*.gen.go"]
  max_file_kb: 200

rules:
  - id: handler-naming
    severity: error
    paths: ["internal/handlers/**"]   # scope to matching files; omit for all
    description: |
      Handler files use snake_case. Handler functions are named
      <Verb><Resource>Handler, e.g. CreateUserHandler.
```

## Secrets

The model API key (and the optional Discord webhook) come from a **secret
source**, never inline. Pick one source per field:

```yaml
# Environment variable — works with every CI and secret store.
api_key:
  env: TERTIB_API_KEY

# AWS Secrets Manager — fetched via the standard AWS credential chain
# (env keys, IAM role, OIDC). No AWS credentials live in this file.
api_key:
  aws_secretsmanager:
    secret_id: tertib/api-key      # name or ARN
    json_key: api_key             # optional: pick a field from a JSON secret
    region: ap-southeast-1        # optional
```

More providers (HashiCorp Vault, GCP Secret Manager, Azure Key Vault, 1Password)
are on the roadmap behind the same schema. Every resolved secret is scrubbed from
logs, errors, and report output.

### Loading a `.env` file

For local development you can keep secrets in a file instead of exporting them.
Point tertib at it with the flag or in config:

```sh
tertib check --env-file .env      # flag: the file must exist, or it's an error
```

```yaml
# .tertib.yml — loaded automatically, and skipped without error if absent
env_file: .env
```

The file is plain `KEY=VALUE` lines (`#` comments and `export` prefixes allowed).
Already-set environment variables are **never** overwritten, so a `.env` can't
shadow a secret your CI injected — the file only fills gaps. `--env-file`
overrides `env_file` in config. Add your `.env` to `.gitignore`.

## CI examples

### GitHub Actions

```yaml
name: conventions
on: pull_request
jobs:
  tertib:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0          # tertib needs history to diff against the base
      - run: go install github.com/chalvinwz/tertib/cmd/tertib@latest
      - run: tertib check
        env:
          TERTIB_API_KEY: ${{ secrets.TERTIB_API_KEY }}
```

The base branch is detected automatically from `GITHUB_BASE_REF`.

### GitLab CI

```yaml
conventions:
  image: ghcr.io/chalvinwz/tertib:latest
  variables:
    GIT_DEPTH: "0"
  script:
    - tertib check
```

### Jenkins

```groovy
pipeline {
  agent { docker { image 'ghcr.io/chalvinwz/tertib:latest' } }
  environment {
    // Jenkins credential of kind "Secret text".
    TERTIB_API_KEY = credentials('tertib-api-key')
  }
  stages {
    stage('conventions') {
      steps {
        // On multibranch PR builds tertib reads CHANGE_TARGET automatically;
        // otherwise pass --base explicitly.
        sh 'tertib check'
      }
    }
  }
}
```

Fetch full history (uncheck "shallow clone" in the job's Git settings, or set
the clone depth to 0) so tertib can diff against the base branch.

### Docker (any CI)

```sh
docker run --rm -v "$PWD:/repo" -e TERTIB_API_KEY \
  ghcr.io/chalvinwz/tertib:latest check --base main
```

## Commands & flags

```
tertib init [--config PATH] [--force]     scaffold a .tertib.yml
tertib validate [--config PATH]           validate a config file
tertib check [flags]                      check code against conventions
tertib version                            print version

check flags:
  --config PATH     config file (default .tertib.yml)
  --all             scan all tracked files instead of the diff
  --base REF        diff base (default: CI target branch, else origin/main)
  --format FORMAT   markdown | json (default markdown)
  --output PATH     write the report to a file (default stdout)
  --fail-on LEVEL   error | warning | never (overrides config)
  --env-file PATH   load env vars from a file before resolving secrets
```

**Exit codes:** `0` passed · `1` violations at or above `fail_on` · `2`
config or runtime error. A pipeline can tell a broken convention (`1`) apart
from a broken run (`2`).

## Roadmap

Hybrid deterministic engine (mechanical naming/structure rules run free and
instant) · SARIF output · inline PR comments · response caching · a baseline
file for adopting tertib on legacy code · more secret-store providers.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues: [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 2026 chalvinwz
