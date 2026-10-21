package juicer

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// checkREADME keeps a count in README.md in step with the test that measures
// it: the first group of re must read want. With JUICER_UPDATE_README set it
// rewrites the count instead (`make readme`).
func checkREADME(t *testing.T, re *regexp.Regexp, want string) {
	t.Helper()
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := re.FindSubmatchIndex(b)
	if m == nil {
		t.Errorf("README.md has nothing matching %s", re)
		return
	}
	switch got := string(b[m[2]:m[3]]); {
	case got == want:
		return
	case os.Getenv("JUICER_UPDATE_README") == "":
		t.Errorf("README.md says %q where the count is %q; run `make readme`", got, want)
		return
	}
	out := append(append(append([]byte{}, b[:m[2]]...), want...), b[m[3]:]...)
	if err := os.WriteFile("README.md", out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func countFixtures(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join("testdata", "in"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
