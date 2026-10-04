package juicer

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestDifferential replays documents generated and inlined by
// testdata/oracle/differential.mjs and requires byte-identical output. It
// needs a corpus, so it skips unless JUICER_DIFFERENTIAL names one; run
// `make differential` locally, and CI runs it nightly with a fresh seed.
func TestDifferential(t *testing.T) {
	path := os.Getenv("JUICER_DIFFERENTIAL")
	if path == "" {
		t.Skip("set JUICER_DIFFERENTIAL to a corpus from `make differential`")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var oracle *juiceProcess
	if os.Getenv("JUICER_MINIMIZE") != "" {
		oracle = startJuice(t)
	}
	// JUICER_DIFFERENTIAL_DUMP writes each mismatch as a JSON line, for triage.
	var dump *json.Encoder
	if p := os.Getenv("JUICER_DIFFERENTIAL_DUMP"); p != "" {
		w, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer w.Close()
		dump = json.NewEncoder(w)
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	limit := 20
	if n, err := strconv.Atoi(os.Getenv("JUICER_DIFFERENTIAL_LIMIT")); err == nil {
		limit = n
	}
	var total, failed int
	for sc.Scan() {
		var c struct {
			Seed, I int
			HTML    string
			Options map[string]any
			Out     *string
			Err     *string
		}
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		total++
		opts, unsupported := mapOptions(t, c.Options)
		if len(unsupported) > 0 {
			t.Fatalf("generator used unsupported options %v", unsupported)
		}
		got, gotErr := New(opts...).Inline(c.HTML)
		switch {
		case c.Err != nil && gotErr != nil:
			continue
		case c.Err != nil:
			// juice failing where we succeed is a documented divergence at
			// best, so report it but do not count it as a mismatch.
			t.Logf("seed %d #%d: juice threw %s; we inlined it", c.Seed, c.I, *c.Err)
			continue
		case gotErr != nil:
			t.Errorf("seed %d #%d: Inline failed where juice did not: %v\n  html: %q", c.Seed, c.I, gotErr, c.HTML)
		case got != *c.Out:
			html, want := c.HTML, *c.Out
			if oracle != nil {
				html, want, got = oracle.minimize(t, html, c.Options)
			}
			if dump != nil {
				dump.Encode(map[string]any{"seed": c.Seed, "i": c.I, "options": c.Options, "html": html, "want": want, "got": got})
			}
			t.Errorf("seed %d #%d options %v\n  html: %q\n  %s", c.Seed, c.I, c.Options, html,
				contextDiff([]byte(got), []byte(want)))
		default:
			continue
		}
		if failed++; failed >= limit {
			t.Fatalf("stopping after %d mismatches", failed)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("differential: %d/%d documents match juice", total-failed, total)
}

// juiceProcess is a long-lived testdata/oracle/serve.mjs.
type juiceProcess struct {
	in  *json.Encoder
	out *bufio.Scanner
}

func startJuice(t *testing.T) *juiceProcess {
	t.Helper()
	cmd := exec.Command("node", "serve.mjs")
	cmd.Dir = filepath.Join("testdata", "oracle")
	cmd.Stderr = os.Stderr
	w, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	r, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close(); cmd.Wait() })
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<24)
	return &juiceProcess{json.NewEncoder(w), sc}
}

func (j *juiceProcess) inline(t *testing.T, html string, options map[string]any) (out string, ok bool) {
	t.Helper()
	if err := j.in.Encode(map[string]any{"html": html, "options": options}); err != nil {
		t.Fatal(err)
	}
	if !j.out.Scan() {
		t.Fatalf("juice process died: %v", j.out.Err())
	}
	var res struct{ Out, Err *string }
	if err := json.Unmarshal(j.out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Out == nil {
		return "", false
	}
	return *res.Out, true
}

// minimize shrinks html while Go and juice still disagree and neither fails,
// deleting ever smaller chunks (ddmin). It returns the reduced document with
// both outputs.
func (j *juiceProcess) minimize(t *testing.T, html string, options map[string]any) (doc, want, got string) {
	t.Helper()
	opts, _ := mapOptions(t, options)
	in := New(opts...)
	differs := func(doc string) (string, string, bool) {
		// Shrinking drifts toward broken input, which has bugs of its own: keep
		// it valid UTF-8, style elements closed with balanced braces, and no
		// tag left open at the end.
		l := strings.ToLower(doc)
		if !utf8.ValidString(doc) || strings.Count(l, "<style") != strings.Count(l, "</style>") {
			return "", "", false
		}
		for _, css := range strings.Split(l, "<style")[1:] {
			css, _, _ = strings.Cut(css, "</style>")
			depth := 0
			for _, c := range css {
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
				}
				if depth < 0 {
					return "", "", false
				}
			}
			if depth != 0 {
				return "", "", false
			}
		}
		if i := strings.LastIndexByte(doc, '<'); i >= 0 && !strings.Contains(doc[i:], ">") {
			return "", "", false
		}
		w, ok := j.inline(t, doc, options)
		if !ok {
			return "", "", false
		}
		g, err := in.Inline(doc)
		return w, g, err == nil && g != w
	}
	want, got, _ = differs(html)
	for n := 2; len(html) > 1; {
		chunk := max(1, len(html)/n)
		shrunk := false
		for i := 0; i < len(html); i += chunk {
			cand := html[:i] + html[min(i+chunk, len(html)):]
			if w, g, ok := differs(cand); ok {
				html, want, got, shrunk = cand, w, g, true
				n = max(n-1, 2)
				break
			}
		}
		if !shrunk {
			if chunk == 1 {
				break
			}
			n = min(n*2, len(html))
		}
	}
	return html, want, got
}
