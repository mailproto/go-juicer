package juicer

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The benchmark document: ~99 KB, 1500 elements, 185 selectors, shaped like a
// table-based marketing email.
func benchDoc() string { return GenEmail(60, 213) }

func BenchmarkInline(b *testing.B) {
	doc := []byte(benchDoc())
	in := New()
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := in.InlineBytes(doc); err != nil {
			b.Fatal(err)
		}
	}
}

// Component baselines, so an end-to-end regression points at a stage.

func BenchmarkParse(b *testing.B) {
	doc := []byte(benchDoc())
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for b.Loop() {
		parseDocument(doc)
	}
}

func BenchmarkRender(b *testing.B) {
	doc := []byte(benchDoc())
	root := parseDocument(doc)
	b.SetBytes(int64(len(doc)))
	b.ReportAllocs()
	for b.Loop() {
		renderDocument(root)
	}
}

func BenchmarkParseCSS(b *testing.B) {
	doc := benchDoc()
	css := doc[strings.Index(doc, "<style type=\"text/css\">")+23 : strings.Index(doc, "</style>")]
	o := defaults()
	b.SetBytes(int64(len(css)))
	b.ReportAllocs()
	for b.Loop() {
		parseStylesheet([]byte(css), &o, 0)
	}
}

// TestAllocBudget gates on allocations rather than wall time: benchmark
// timings on shared CI runners are noisy enough that any threshold is either
// useless or a flake generator, while allocation counts are deterministic.
//
// Every benchmark document has a budget. Raise one deliberately, in a reviewed
// commit; lower it when an improvement lands so the gain cannot quietly erode.
func TestAllocBudget(t *testing.T) {
	budgets := map[string]float64{
		"inline":            31000,
		"tiny":              75,
		"small":             950,
		"manyRulesFewNodes": 18500,
		"fewRulesManyNodes": 79500,
		"unbucketable":      8200,
		"deepDescendant":    8400,
		"large":             152000,
	}
	docs := map[string]string{"inline": benchDoc()}
	for _, s := range shapes {
		docs[s.name] = s.gen()
	}
	in := New()
	for name, doc := range docs {
		budget, ok := budgets[name]
		if !ok {
			t.Errorf("%s: no allocation budget", name)
			continue
		}
		b := []byte(doc)
		n := testing.AllocsPerRun(3, func() { in.InlineBytes(b) })
		if n > budget {
			t.Errorf("%s: %.0f allocs/op, over the budget of %.0f", name, n, budget)
		} else {
			t.Logf("%s: %.0f allocs/op (budget %.0f)", name, n, budget)
		}
	}
}

// TestGeneratorsAgree keeps the Go and Node benchmark corpora identical.
// Without it the cross-language comparison is unfalsifiable.
func TestGeneratorsAgree(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	if _, err := os.Stat("testdata/oracle/node_modules"); err != nil {
		t.Skip("oracle not installed; run `make goldens`")
	}
	out, err := exec.Command(node, "--input-type=module", "-e",
		`import {genEmail} from "./testdata/oracle/gen.mjs"; process.stdout.write(genEmail(60,213))`).Output()
	if err != nil {
		t.Fatalf("running gen.mjs: %v", err)
	}
	if got := benchDoc(); got != string(out) {
		t.Fatalf("bench_gen_test.go and testdata/oracle/gen.mjs have diverged: %d vs %d bytes",
			len(got), len(out))
	}
}
