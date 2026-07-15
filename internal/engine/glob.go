package engine

import (
	"regexp"
	"strings"
)

// matcher tests paths against a set of globs. It supports:
//
//   - any run of characters except "/"
//     ?   any single character except "/"
//     **  any characters including "/" (recursive)
//     **/ zero or more leading path segments
//
// so "vendor/**", "**/*.gen.go", and "internal/handlers/**" behave as expected.
type matcher struct {
	res []*regexp.Regexp
}

func newMatcher(patterns []string) *matcher {
	m := &matcher{}
	for _, p := range patterns {
		if p == "" {
			continue
		}
		m.res = append(m.res, regexp.MustCompile(globToRegexp(p)))
	}
	return m
}

// matchAny reports whether path matches any pattern. An empty matcher matches
// nothing.
func (m *matcher) matchAny(path string) bool {
	for _, re := range m.res {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++ // consume second '*'
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++ // consume '/': "**/" matches zero or more segments
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	return b.String()
}
