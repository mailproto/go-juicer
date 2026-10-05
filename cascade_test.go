package juicer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCascade asks a browser whether inlining preserved the cascade: every
// fixture is rendered in headless Chrome as written and as this package
// inlines it (testdata/oracle/cascade.mjs), and each element's computed style
// must match. It checks juice's semantics, which this package follows, as
// well as the port. Documents that differ are listed with the reason in
// testdata/skip/cascade.txt, which only shrinks.
//
// It needs Node and Chrome, so it runs only with JUICER_CASCADE set and
// CHROME_PATH pointing at Chrome; `make cascade` does both.
func TestCascade(t *testing.T) {
	if os.Getenv("JUICER_CASCADE") == "" {
		t.Skip("set JUICER_CASCADE=1 and CHROME_PATH to compare against headless Chrome (`make cascade`)")
	}

	type pair struct {
		Name     string `json:"name"`
		Original string `json:"original"`
		Inlined  string `json:"inlined"`
	}
	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	root := filepath.Join("testdata", "in")
	var names []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, ".html"))
		in, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		opts, unsupported := loadOptions(t, strings.TrimSuffix(p, ".html")+".json")
		if len(unsupported) > 0 {
			return nil
		}
		out, err := New(opts...).Inline(string(in))
		if err != nil {
			return nil
		}
		// The browser has to see extraCss too: juice applies it after the
		// document's own <style> blocks, as a trailing block would be.
		original := string(in)
		if b, err := os.ReadFile(strings.TrimSuffix(p, ".html") + ".json"); err == nil {
			var f fixtureOptions
			if json.Unmarshal(b, &f) == nil {
				if css, ok := f.Options["extraCss"].(string); ok {
					original += "<style>" + css + "</style>"
				}
			}
		}
		names = append(names, name)
		return enc.Encode(pair{name, original, out})
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("node", "cascade.mjs")
	cmd.Dir = filepath.Join("testdata", "oracle")
	cmd.Stdin = &input
	cmd.Stderr = os.Stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("cascade.mjs: %v", err)
	}
	results := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var r struct {
			Name  string
			Diffs []string
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		results[r.Name] = r.Diffs
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatalf("cascade.mjs returned no results (%d bytes of output)", len(stdout))
	}

	known := loadSkips(t, "cascade")
	var agree, listed int
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			diffs, ok := results[name]
			if !ok {
				t.Fatal("no result from cascade.mjs")
			}
			reason, isKnown := known[name]
			switch {
			case isKnown && len(diffs) == 0:
				t.Errorf("now preserves the cascade; drop its line from testdata/skip/cascade.txt\n  was: %s", reason)
			case isKnown:
				listed++
				t.Skipf("known difference: %s", reason)
			case len(diffs) > 0:
				t.Errorf("computed style changed by inlining:\n  %s", strings.Join(diffs, "\n  "))
			default:
				agree++
			}
		})
	}
	for name, reason := range known {
		if _, ok := results[name]; !ok {
			t.Errorf("testdata/skip/cascade.txt names %q, which was not checked (%s)", name, reason)
		}
	}
	t.Logf("cascade: %d documents render the same inlined, %d known differences", agree, listed)
}
