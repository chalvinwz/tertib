package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInRepo runs a git command in the current working directory, failing the
// test on error.
func gitInRepo(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupRepo creates a repo with one commit on main and a feature branch that
// modifies a.txt and adds b.txt.
func setupRepo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	gitInRepo(t, "init", "-b", "main")
	gitInRepo(t, "config", "user.email", "test@example.com")
	gitInRepo(t, "config", "user.name", "test")
	gitInRepo(t, "config", "commit.gpgsign", "false")

	write(t, filepath.Join(dir, "a.txt"), "line1\nline2\n")
	gitInRepo(t, "add", "a.txt")
	gitInRepo(t, "commit", "-m", "initial")

	gitInRepo(t, "checkout", "-b", "feature")
	write(t, filepath.Join(dir, "a.txt"), "line1\nline2\nline3\n")
	write(t, filepath.Join(dir, "b.txt"), "new\n")
	gitInRepo(t, "add", "a.txt", "b.txt")
	gitInRepo(t, "commit", "-m", "feature work")
}

func TestChangedFilesAndHunks(t *testing.T) {
	setupRepo(t)

	files, err := Changed("main")
	if err != nil {
		t.Fatal(err)
	}

	byPath := map[string]File{}
	for _, f := range files {
		byPath[f.Path] = f
	}

	a, ok := byPath["a.txt"]
	if !ok {
		t.Fatal("a.txt should be reported as changed")
	}
	if a.Status != "M" {
		t.Errorf("a.txt status = %q, want M", a.Status)
	}
	if len(a.Hunks) != 1 || a.Hunks[0].Start != 3 || a.Hunks[0].End != 3 {
		t.Errorf("a.txt hunks = %+v, want [{3 3}]", a.Hunks)
	}

	b, ok := byPath["b.txt"]
	if !ok {
		t.Fatal("b.txt should be reported as added")
	}
	if b.Status != "A" {
		t.Errorf("b.txt status = %q, want A", b.Status)
	}
	if len(b.Hunks) != 1 || !b.Hunks[0].Contains(1) {
		t.Errorf("b.txt hunks = %+v, want a hunk containing line 1", b.Hunks)
	}
}

func TestAllFiles(t *testing.T) {
	setupRepo(t)

	files, err := AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 tracked files, got %d: %+v", len(files), files)
	}
}

func TestHunkContains(t *testing.T) {
	h := Hunk{Start: 10, End: 12}
	for _, tc := range []struct {
		n    int
		want bool
	}{{9, false}, {10, true}, {11, true}, {12, true}, {13, false}} {
		if h.Contains(tc.n) != tc.want {
			t.Errorf("Contains(%d) = %v, want %v", tc.n, !tc.want, tc.want)
		}
	}
}
