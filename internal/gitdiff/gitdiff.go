// Package gitdiff enumerates the files (and changed line ranges) tertib should
// review. In diff mode it asks git for changes on the branch since it diverged
// from a base ref; in full-scan mode it lists all tracked files.
package gitdiff

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// File is a changed (or tracked) file tertib may review.
type File struct {
	Path   string
	Status string // git name-status letter: A, M, D, ... ("" for full scan)
	Hunks  []Hunk // changed line ranges in the new file (empty in full scan)
}

// Hunk is an inclusive range of new-file line numbers that changed.
type Hunk struct {
	Start int
	End   int
}

// Contains reports whether line n falls within the hunk.
func (h Hunk) Contains(n int) bool { return n >= h.Start && n <= h.End }

// Changed returns files changed between the merge base of base..HEAD and HEAD.
// Deleted files carry no hunks (there is no new-file content to review).
func Changed(base string) ([]File, error) {
	files, err := changedNames(base)
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].Status == "D" {
			continue
		}
		h, err := hunks(base, files[i].Path)
		if err != nil {
			return nil, err
		}
		files[i].Hunks = h
	}
	return files, nil
}

// AllFiles lists every tracked file in the repository, for full-scan mode.
func AllFiles() ([]File, error) {
	out, err := run("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var files []File
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p != "" {
			files = append(files, File{Path: p})
		}
	}
	return files, nil
}

func changedNames(base string) ([]File, error) {
	// --merge-base diffs from the merge base of base and HEAD, so unrelated
	// commits already on the base branch are excluded. --no-renames keeps the
	// output to simple status+path pairs (a rename shows as delete + add).
	out, err := run("diff", "--merge-base", base, "HEAD", "--name-status", "--no-renames", "-z")
	if err != nil {
		return nil, err
	}
	// -z output is NUL-separated: STATUS \x00 PATH \x00 STATUS \x00 PATH ...
	fields := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	var files []File
	for i := 0; i+1 < len(fields); i += 2 {
		status := fields[i]
		path := fields[i+1]
		if status == "" || path == "" {
			continue
		}
		files = append(files, File{Path: path, Status: status[:1]})
	}
	return files, nil
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func hunks(base, path string) ([]Hunk, error) {
	// --unified=0 yields headers with no surrounding context, so each @@ range
	// is exactly the changed lines.
	out, err := run("diff", "--merge-base", base, "HEAD", "--unified=0", "--", path)
	if err != nil {
		return nil, err
	}
	var hs []Hunk
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		m := hunkHeader.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		if count == 0 {
			// Pure deletion: no new-file lines to review.
			continue
		}
		hs = append(hs, Hunk{Start: start, End: start + count - 1})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return hs, nil
}

func run(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
