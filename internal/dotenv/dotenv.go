// Package dotenv loads KEY=VALUE pairs from a file into the process
// environment. It is a convenience for local development; in CI the secret is
// normally injected as a real environment variable and no file is needed.
//
// Existing environment variables always win: a value already present in the
// environment is never overwritten, so a stray committed .env cannot shadow a
// secret the CI system injected.
package dotenv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Load reads path and sets each variable that is not already present in the
// environment. A missing file surfaces as an error that satisfies
// errors.Is(err, fs.ErrNotExist), so callers can choose to tolerate it.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	vars, err := Parse(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for k, v := range vars {
		if _, ok := os.LookupEnv(k); ok {
			continue // real environment wins
		}
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	return nil
}

// Parse reads KEY=VALUE lines. Blank lines and lines beginning with "#" are
// ignored, an optional leading "export " is stripped, and a value wrapped in
// matching single or double quotes is unquoted. A line without "=" is an
// error, so typos are caught rather than silently dropped.
func Parse(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		raw = strings.TrimPrefix(raw, "export ")

		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", line)
		}
		key := strings.TrimSpace(raw[:eq])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", line)
		}
		out[key] = unquote(strings.TrimSpace(raw[eq+1:]))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
