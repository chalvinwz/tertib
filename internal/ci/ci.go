// Package ci detects the surrounding CI environment in a vendor-neutral way,
// so tertib can pick a sensible diff base without per-platform configuration.
package ci

import "os"

// DefaultBase is used when no CI env var and no --base flag identify a target
// branch. It assumes the conventional primary branch on a fetched remote.
const DefaultBase = "origin/main"

// baseRefEnvs lists the env vars that name a pull/merge request's target branch
// across common CI systems, in priority order.
var baseRefEnvs = []string{
	"GITHUB_BASE_REF",                     // GitHub Actions (pull_request)
	"CI_MERGE_REQUEST_TARGET_BRANCH_NAME", // GitLab CI (merge request)
	"BITBUCKET_PR_DESTINATION_BRANCH",     // Bitbucket Pipelines (PR)
	"CHANGE_TARGET",                       // Jenkins multibranch
}

// BaseRef returns the diff target branch discovered from CI environment
// variables, or "" if none is set.
func BaseRef() string {
	for _, env := range baseRefEnvs {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return ""
}

// IsCI reports whether tertib appears to be running inside a CI system. Most CI
// providers set CI=true.
func IsCI() bool {
	return os.Getenv("CI") != ""
}
