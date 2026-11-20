// Package leakcheck holds the check that public pong names nothing that is not in it: no folder
// of the private repository that sits beside it at private/, no internal document or plan by its
// number, no endpoint of Pong's server, and no one's home folder or personal address.
// Run it with go test ./internal/leakcheck/.
package leakcheck

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var rules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"a private folder", regexp.MustCompile(`(^|[^a-zA-Z0-9_/.-])(private|ios|web|docs|specs|infra|deploy|prototype|spikes)/|internal/(server|servertest|phonetest)\b|cmd/pong-server|server/(schema|config)`)},
	{"an internal document", regexp.MustCompile(`GLOSSARY\.md|SPEC\.md|(?i:\bspecs? ?[0-9]{2}\b)|\bADR ?[0-9]+|(?i:\btickets? [0-9]+)`)},
	{"an endpoint of Pong's server", regexp.MustCompile(`/api/(relay|sign-in|devices|voice|account|stripe)`)},
	{"someone's home folder", regexp.MustCompile(`/Users/[a-z][a-z0-9]*`)},
	{"a personal address", regexp.MustCompile(`(?i)[a-z0-9._%+-]+@(gmail|googlemail|icloud|me|mac|outlook|hotmail|yahoo|proton(mail)?|hey|pong)\.[a-z.]+`)},
}

// allowed is what matches a rule but names nothing private: made-up home folders in tests.
var allowed = regexp.MustCompile(`/Users/(me|you|dev|test|user|alice|bob|someone|example|name)\b`)

// skip is what the check does not read: the private repository, local agent settings,
// build output and dependencies. Binary files are skipped as they are read.
var skip = map[string]bool{".git": true, "private": true, ".claude": true, "CLAUDE.local.md": true, "build": true, ".build": true, "node_modules": true, ".swiftpm": true}

func TestPublicNamesNothingPrivate(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if skip[d.Name()] {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "internal/leakcheck/leakcheck_test.go" || rel == "scripts/hooks/pre-commit" { // these two must name private/ to guard it
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.IndexByte(b, 0) >= 0 {
			return nil // binary
		}
		files++
		s := bufio.NewScanner(bytes.NewReader(b))
		s.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for n := 1; s.Scan(); n++ {
			line := allowed.ReplaceAllString(s.Text(), "")
			for _, r := range rules {
				if m := r.re.FindString(line); m != "" {
					t.Errorf("%s:%d: names %s: %q", rel, n, r.name, strings.TrimSpace(m))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 {
		t.Fatalf("read only %d files; the repository is not being found", files)
	}
}
