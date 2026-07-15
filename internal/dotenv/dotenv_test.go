package dotenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `
# a comment
FOO=bar
export BAZ=qux
QUOTED="hello world"
SINGLE='single'
EMPTY=
  SPACED  =  trimmed
`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FOO":    "bar",
		"BAZ":    "qux",
		"QUOTED": "hello world",
		"SINGLE": "single",
		"EMPTY":  "",
		"SPACED": "trimmed",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("parsed %d keys, want %d: %v", len(got), len(want), got)
	}
}

func TestParseMalformedLine(t *testing.T) {
	if _, err := Parse(strings.NewReader("FOO=bar\nNOEQUALS\n")); err == nil {
		t.Fatal("expected error for a line without '='")
	}
}

func TestParseEmptyKey(t *testing.T) {
	if _, err := Parse(strings.NewReader("=value")); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestLoadSetsMissingVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TERTIB_NEW_VAR=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure it starts unset and is cleaned up.
	os.Unsetenv("TERTIB_NEW_VAR")
	t.Cleanup(func() { os.Unsetenv("TERTIB_NEW_VAR") })

	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TERTIB_NEW_VAR"); got != "fromfile" {
		t.Errorf("TERTIB_NEW_VAR = %q, want fromfile", got)
	}
}

func TestLoadDoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TERTIB_EXISTING=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERTIB_EXISTING", "fromenv") // pre-set: real env must win

	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TERTIB_EXISTING"); got != "fromenv" {
		t.Errorf("existing env was overwritten: got %q, want fromenv", got)
	}
}

func TestLoadMissingFileIsNotExist(t *testing.T) {
	err := Load(filepath.Join(t.TempDir(), "absent.env"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error should satisfy fs.ErrNotExist, got %v", err)
	}
}
