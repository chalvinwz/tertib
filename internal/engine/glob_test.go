package engine

import "testing"

func TestGlobMatching(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"vendor/**", "vendor/foo/bar.go", true},
		{"vendor/**", "vendorx/bar.go", false},
		{"**/*.gen.go", "a/b/x.gen.go", true},
		{"**/*.gen.go", "x.gen.go", true},
		{"**/*.gen.go", "x.go", false},
		{"internal/handlers/**", "internal/handlers/user.go", true},
		{"internal/handlers/**", "internal/services/user.go", false},
		{"*.go", "a.go", true},
		{"*.go", "sub/a.go", false},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/b", true},
	}
	for _, tc := range cases {
		m := newMatcher([]string{tc.pattern})
		if got := m.matchAny(tc.path); got != tc.want {
			t.Errorf("pattern %q vs %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMatcherMultiplePatterns(t *testing.T) {
	m := newMatcher([]string{"vendor/**", "dist/**"})
	if !m.matchAny("dist/app") {
		t.Error("should match second pattern")
	}
	if m.matchAny("src/app.go") {
		t.Error("should not match unrelated path")
	}
}

func TestEmptyMatcherMatchesNothing(t *testing.T) {
	if newMatcher(nil).matchAny("anything") {
		t.Error("empty matcher should match nothing")
	}
}
