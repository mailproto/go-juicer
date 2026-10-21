package juicer

// Differential test against juice, the reference implementation.
//
// Goldens in testdata/out/<target>/<variant> are produced ONLY by
// testdata/oracle/generate.mjs, one target per pinned juice version and one
// variant per option set in testdata/variants.json.
// There is deliberately no -update flag here: regenerating expectations from
// the code under test would certify this implementation against itself and
// destroy the oracle. Run `make goldens` instead.
//
// The assertion is raw byte equality with no normalization. juice's output is
// what a mail client receives, so differences in entity encoding or element
// structure are real differences, not formatting. Failures run a classifier
// to label the difference, but that only shapes the message.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type fixtureOptions struct {
	Options map[string]any `json:"options"`
	Client  map[string]any `json:"client"`
}

// parityRuns pairs each set of goldens with the mode it is checked under.
// juice 12's goldens are checked twice: in Juice12 mode, which must match
// them, and in the default mode, which differs only where testdata/skip/
// default.txt says juice disagrees with browsers.
var parityRuns = []struct {
	name, target string
	mode         Compatibility
	skipFiles    []string
	readme       string // the run's row in the README table
}{
	{"juice-12", "juice-12", Juice12, []string{"juice-12"}, "juice 12.1.3, with `CompatibilityMode(Juice12)`"},
	{"juice-9", "juice-9", Juice9, []string{"juice-9"}, "juice 9.1.0, with `CompatibilityMode(Juice9)`"},
	{"default", "juice-12", standard, []string{"juice-12", "default"}, "juice 12.1.3, default mode"},
}

func TestParity(t *testing.T) {
	variants := loadVariants(t)
	for _, run := range parityRuns {
		t.Run(run.name, func(t *testing.T) {
			skips := map[string]string{}
			for _, f := range run.skipFiles {
				maps.Copy(skips, loadSkips(t, f))
			}
			n := tally{freed: map[string]bool{}, still: map[string][]string{}}
			for _, variant := range slices.Sorted(maps.Keys(variants)) {
				t.Run(variant, func(t *testing.T) {
					parityVariant(t, run.target, run.mode, variant, variants[variant], skips, &n)
				})
			}

			// A skip naming a variant or fixture that no longer exists is stale.
			own := loadSkips(t, run.skipFiles[len(run.skipFiles)-1])
			for name, reason := range own {
				variant, fixture, _ := strings.Cut(name, "/")
				_, okVariant := variants[variant]
				_, err := os.Stat(filepath.Join("testdata", "in", fixture+".html"))
				if !(okVariant || variant == "*") || err != nil {
					t.Errorf("testdata/skip/%s.txt names missing case %q (%s)", run.skipFiles[len(run.skipFiles)-1], name, reason)
				}
			}
			if total := n.pass + n.skipped + n.failed; total > 0 {
				t.Logf("%s parity: %d/%d cases (%.1f%%), %d skipped, %d failed",
					run.name, n.pass, total, 100*float64(n.pass)/float64(total), n.skipped, n.failed)
			}
			// Only a whole, passing run has the counts the README states.
			if n.failed == 0 && n.pass+n.skipped == len(variants)*countFixtures(t) {
				if ratcheting() {
					own := run.skipFiles[len(run.skipFiles)-1]
					rewriteSkips(t, own, n.ratchet(loadSkips(t, own)))
				}
				skipped := "none"
				if n.skipped > 0 {
					skipped = strconv.Itoa(n.skipped)
				}
				re := regexp.MustCompile(`\| ` + regexp.QuoteMeta(run.readme) + ` \| (\d+ \| \d+ \| (?:\d+|none))`)
				checkREADME(t, re, fmt.Sprintf("%d | %d | %s", n.pass+n.skipped, n.pass, skipped))
			}
		})
	}
}

// tally counts a run's cases. For `make ratchet` it also records which skip
// lines had a case pass, and the variants each still fails in.
type tally struct {
	pass, skipped, failed int
	freed                 map[string]bool
	still                 map[string][]string
}

// parityVariant runs every fixture under one option variant. A skip line
// names "<variant>/<fixture>", or "*/<fixture>" for every variant.
// ratchet is the lines of the skip list own to rewrite: dropped where every
// case now passes, and a "*/" line narrowed to the variants still failing.
func (n tally) ratchet(own map[string]string) map[string][]string {
	with := map[string][]string{}
	for key := range n.freed {
		reason, ok := own[key]
		if !ok {
			continue
		}
		var lines []string
		for _, v := range n.still[key] {
			lines = append(lines, v+"/"+strings.TrimPrefix(key, "*/")+"  "+reason)
		}
		with[key] = lines
	}
	return with
}

func parityVariant(t *testing.T, target string, mode Compatibility, variant string, variantOpts map[string]any, skips map[string]string, n *tally) {
	root := filepath.Join("testdata", "in")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fixture := filepath.ToSlash(strings.TrimSuffix(rel, ".html"))
		name := variant + "/" + fixture
		skipKey := name
		if _, ok := skips[skipKey]; !ok {
			skipKey = "*/" + fixture
		}

		t.Run(fixture, func(t *testing.T) {
			in, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			// Fixture sidecar options are deliberate, so they win over the variant.
			opts, unsupported := mapOptions(t, variantOpts)
			fixtureOpts, fixtureUnsupported := loadOptions(t, strings.TrimSuffix(p, ".html")+".json")
			opts = append(opts, fixtureOpts...)
			unsupported = append(unsupported, fixtureUnsupported...)
			if mode != standard {
				opts = append(opts, CompatibilityMode(mode))
			}
			want, wantErr := loadGolden(t, filepath.Join(target, name))

			got, gotErr := mustNew(t, opts...).InlineBytes(in)
			ok := len(unsupported) == 0 && (gotErr != nil) == wantErr && (wantErr || bytes.Equal(got, want))

			reason, isSkipped := skips[skipKey]
			switch {
			case isSkipped && ok && ratcheting():
				n.freed[skipKey] = true
				n.pass++
			case isSkipped && ok:
				t.Errorf("case now passes; drop its skip line (`make ratchet`)\n  was: %s", reason)
			case isSkipped:
				n.still[skipKey] = append(n.still[skipKey], variant)
				n.skipped++
				t.Skipf("known gap: %s", reason)
			case !ok:
				n.failed++
				if len(unsupported) > 0 {
					t.Fatalf("uses juice options this package does not support: %v", unsupported)
				}
				if gotErr != nil {
					t.Fatalf("Inline returned an error, juice did not: %v", gotErr)
				}
				t.Errorf("%s\n%s", classify(got, want), contextDiff(got, want))
			default:
				n.pass++
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadVariants(t *testing.T) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "variants.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("bad testdata/variants.json: %v", err)
	}
	return v
}

func TestRatchetNarrows(t *testing.T) {
	n := tally{
		freed: map[string]bool{"*/a/fixed": true, "*/a/mixed": true, "v1/a/one": true, "*/a/elsewhere": true},
		still: map[string][]string{"*/a/mixed": {"v1", "v3"}},
	}
	own := map[string]string{"*/a/fixed": "r1", "*/a/mixed": "r2", "v1/a/one": "r3"}
	want := map[string][]string{"*/a/fixed": nil, "*/a/mixed": {"v1/a/mixed  r2", "v3/a/mixed  r2"}, "v1/a/one": nil}
	if got := n.ratchet(own); !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ratcheting reports whether skip lines whose cases now pass are rewritten
// (`make ratchet`) rather than failing the test.
func ratcheting() bool { return os.Getenv("JUICER_RATCHET") != "" }

// rewriteSkips replaces each named line of testdata/skip/<target>.txt with
// the given lines, dropping it when there are none.
func rewriteSkips(t *testing.T, target string, with map[string][]string) {
	t.Helper()
	if len(with) == 0 {
		return
	}
	path := filepath.Join("testdata", "skip", target+".txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if lines, ok := with[name]; ok && !strings.HasPrefix(name, "#") {
			out = append(out, lines...)
			continue
		}
		out = append(out, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadSkips(t *testing.T, target string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "skip", target+".txt"))
	if os.IsNotExist(err) {
		return map[string]string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, reason, _ := strings.Cut(line, " ")
		out[name] = strings.TrimSpace(reason)
	}
	return out
}

func loadOptions(t *testing.T, path string) (opts []Option, unsupported []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var f fixtureOptions
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("bad options sidecar: %v", err)
	}
	for k, v := range f.Client {
		if k != "codeBlocks" {
			t.Fatalf("juice client setting %q is not mapped in parity_test.go", k)
		}
		var blocks []CodeBlock
		for _, name := range slices.Sorted(maps.Keys(v.(map[string]any))) {
			d := v.(map[string]any)[name].(map[string]any)
			blocks = append(blocks, CodeBlock{d["start"].(string), d["end"].(string)})
		}
		opts = append(opts, CodeBlocks(blocks))
	}
	more, unsupported := mapOptions(t, f.Options)
	return append(opts, more...), unsupported
}

// mapOptions maps juice options onto Go Options. Only the options a fixture
// or variant actually uses need a case here; an unmapped one is a test bug,
// not a silent pass. Options juice has and this package lacks are returned as
// unsupported, which fails the case unless it is skip-listed.
func mapOptions(t *testing.T, juiceOpts map[string]any) (opts []Option, unsupported []string) {
	t.Helper()
	for k, v := range juiceOpts {
		b, _ := v.(bool)
		s, _ := v.(string)
		switch k {
		case "extraCss":
			opts = append(opts, ExtraCSS(s))
		case "applyStyleTags":
			opts = append(opts, ApplyStyleTags(b))
		case "removeStyleTags":
			opts = append(opts, RemoveStyleTags(b))
		case "preserveMediaQueries":
			opts = append(opts, PreserveMediaQueries(b))
		case "preserveFontFaces":
			opts = append(opts, PreserveFontFaces(b))
		case "preserveKeyFrames":
			opts = append(opts, PreserveKeyFrames(b))
		case "preservePseudos":
			opts = append(opts, PreservePseudos(b))
		case "preserveImportant":
			opts = append(opts, PreserveImportant(b))
		case "applyWidthAttributes":
			opts = append(opts, ApplyWidthAttributes(b))
		case "applyHeightAttributes":
			opts = append(opts, ApplyHeightAttributes(b))
		case "applyAttributesTableElements":
			opts = append(opts, ApplyAttributesTableElements(b))
		case "resolveCSSVariables":
			opts = append(opts, ResolveCSSVariables(b))
		case "inlinePseudoElements":
			opts = append(opts, InlinePseudoElements(b))
		case "styleAttributeName":
			opts = append(opts, StyleAttributeName(s))
		case "preserveContainerQueries":
			opts = append(opts, PreserveContainerQueries(b))
		case "preserveLayers":
			opts = append(opts, PreserveLayers(b))
		case "insertPreservedExtraCss":
			if s != "" {
				opts = append(opts, InsertPreservedExtraCSSInto(s))
			} else {
				opts = append(opts, InsertPreservedExtraCSS(b))
			}
		case "xmlMode":
			opts = append(opts, XMLMode(b))
		case "inlineDuplicateProperties":
			opts = append(opts, InlineDuplicateProperties(b))
		case "removeInlinedSelectors":
			opts = append(opts, RemoveInlinedSelectors(b))
		case "preservedSelectors":
			var patterns []string
			for _, p := range v.([]any) {
				patterns = append(patterns, p.(string))
			}
			opts = append(opts, PreservedSelectors(patterns...))
		default:
			t.Fatalf("juice option %q is not mapped in parity_test.go", k)
		}
	}
	slices.Sort(unsupported)
	return opts, unsupported
}

func loadGolden(t *testing.T, name string) (content []byte, isErr bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "out", name+".html"))
	if err == nil {
		return b, false
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join("testdata", "out", name+".err")); err == nil {
		return nil, true
	}
	t.Fatalf("no golden for %q; run `make goldens`", name)
	return nil, false
}

// classify labels a mismatch for the failure message. It must never decide
// whether a test passes -- a normalization that silences a diff here would
// grow until it hides real bugs.
func classify(got, want []byte) string {
	for _, c := range []struct {
		name string
		norm func([]byte) []byte
	}{
		{"VOID_SLASH", func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("/>"), []byte(">")) }},
		{"EMPTY_ATTR", func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(`=""`), nil) }},
		{"DOC_WRAPPER", stripWrapper},
	} {
		if bytes.Equal(c.norm(got), c.norm(want)) {
			return "DIFF: " + c.name + " only"
		}
	}
	if bytes.Contains(want, []byte("style=")) || bytes.Contains(got, []byte("style=")) {
		return "DIFF: SEMANTIC (style attribute or cascade)"
	}
	return "DIFF: SEMANTIC (structural)"
}

func stripWrapper(b []byte) []byte {
	for _, s := range []string{"<html>", "</html>", "<head>", "</head>", "<body>", "</body>", "<tbody>", "</tbody>"} {
		b = bytes.ReplaceAll(b, []byte(s), nil)
	}
	return b
}

// contextDiff reports the first differing byte with surrounding context.
// Email HTML is one long line, so a line-oriented diff is useless.
func contextDiff(got, want []byte) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	lo := i - 60
	if lo < 0 {
		lo = 0
	}
	return fmt.Sprintf("first difference at byte %d of %d (want %d)\nwant: %s\n      %s^\ngot:  %s",
		i, len(got), len(want),
		clip(want, lo, i+60), strings.Repeat(" ", i-lo), clip(got, lo, i+60))
}

func clip(b []byte, lo, hi int) string {
	if lo > len(b) {
		lo = len(b)
	}
	if hi > len(b) {
		hi = len(b)
	}
	s := string(b[lo:hi])
	if lo > 0 {
		s = "…" + s
	}
	if hi < len(b) {
		s += "…"
	}
	return s
}
