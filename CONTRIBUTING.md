# Contributing to tertib

## Trunk-based workflow

`main` is always releasable. **Never push directly to `main`** — every change lands through a short-lived branch and a pull request.

```
main ──●─────────────●──────────●──  (always green, always releasable)
        \           /          /
         feature/x         fix/y      (short-lived, squash-merged)
```

## Branch naming

Name branches `prefix/kebab-summary`, where `prefix` is one of:

| Prefix      | Use for                          |
|-------------|----------------------------------|
| `feature/`  | new functionality                |
| `fix/`      | bug fixes                        |
| `hotfix/`   | urgent production fixes          |
| `chore/`    | maintenance, deps, tooling       |
| `ci/`       | CI / build pipeline changes      |
| `docs/`     | documentation only               |
| `refactor/` | internal restructure, no behavior change |
| `test/`     | tests only                       |

Examples: `ci/add-golangci-lint`, `fix/hunk-parsing`, `feature/sarif-output`.

## Before opening a PR

Run the local gate — it must pass:

```sh
make check        # gofmt -l, go vet, go test
```

CI re-runs this plus a race detector, `golangci-lint`, `govulncheck`, and a 6-target cross-build. **All CI checks must be green before merge.**

## Design notes

- **stdlib first.** tertib deliberately avoids CLI frameworks (no cobra/viper) and keeps its dependency tree small. New dependencies need a clear justification.
- **Vendor-neutral by default.** No feature should assume a specific CI provider, model vendor, or secret store. Provider-specific code lives behind an interface (see `internal/secrets`).
- **Secrets never leak.** Any resolved secret must be registered with the redactor so it is scrubbed from logs, errors, and reports.

## Merging

Squash-merge only, keeping `main` history linear. As the sole maintainer you self-merge once CI is green; no separate approval is required.

## Releasing

The version is tag-driven (`git describe` / GoReleaser) — nothing to bump in code. Cut a release by pushing a **strict semver** tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `release` workflow then builds all-OS binaries (linux, darwin, windows × amd64, arm64), generates `checksums.txt`, and publishes a GitHub Release with an auto-generated changelog. Only tags matching `vX.Y.Z` trigger a release.

## Reporting security issues

Do **not** open a public issue for vulnerabilities. Follow the process in [SECURITY.md](SECURITY.md) (private reporting via GitHub Security advisories).
