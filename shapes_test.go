package juicer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Document shapes for cross-implementation benchmarking. Mirrored in
// testdata/oracle/shapes.mjs; TestShapesAgree keeps the two identical.

func rep(n int, f func(int) string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(f(i))
	}
	return b.String()
}

var shapes = []struct {
	name string
	gen  func() string
}{
	// Smallest realistic unit: one rule, one element.
	{"tiny", func() string { return `<style>div{color:red}</style><div>x</div>` }},

	// Short transactional email.
	{"small", func() string {
		return `<style>` + rep(12, func(i int) string {
			return fmt.Sprintf(".c%d{color:#%06x;padding:%dpx}\n", i, i*111111, i)
		}) + `a{color:#06c}td{vertical-align:top}</style><table><tbody>` +
			rep(12, func(i int) string {
				return fmt.Sprintf(`<tr><td class="c%d"><a href="#">l%d</a>text %d</td></tr>`, i, i, i)
			}) + `</tbody></table>`
	}},

	// Many rules, few nodes: index construction cannot amortize.
	{"manyRulesFewNodes", func() string {
		return `<style>` + rep(800, func(i int) string {
			return fmt.Sprintf(".c%d span{color:#%06x}\n", i, i*7)
		}) + `</style><div class="c1"><span>x</span></div>`
	}},

	// Few rules, many nodes: the walk dominates.
	{"fewRulesManyNodes", func() string {
		return `<style>td{padding:2px}.a{color:red}</style><table><tbody>` +
			rep(3000, func(i int) string { return fmt.Sprintf(`<tr><td class="a">%d</td></tr>`, i) }) +
			`</tbody></table>`
	}},

	// Every selector is universal or attribute-keyed, so nothing buckets and
	// the index degenerates to brute force plus merge overhead.
	{"unbucketable", func() string {
		return `<style>` + rep(120, func(i int) string {
			return fmt.Sprintf(`*[data-k="%d"]{color:#%06x}`+"\n", i, i*9)
		}) + `</style><div>` +
			rep(400, func(i int) string { return fmt.Sprintf(`<p data-k="%d">x</p>`, i%120) }) + `</div>`
	}},

	// Deep descendant chains: many partial matches per node.
	{"deepDescendant", func() string {
		return `<style>` + rep(60, func(i int) string {
			return fmt.Sprintf("div div div .d%d span{color:#%06x}\n", i, i*13)
		}) + `</style><div><div><div>` +
			rep(300, func(i int) string { return fmt.Sprintf(`<p class="d%d"><span>x</span></p>`, i%60) }) +
			`</div></div></div>`
	}},

	// Large newsletter, where juice's O(rules x nodes) should hurt most.
	{"large", func() string {
		return `<style>` + rep(200, func(i int) string {
			return fmt.Sprintf(".c%d{color:#%06x;font-size:%dpx}\n", i, i*7919%0xffffff, 10+i%12)
		}) + `table{border-collapse:collapse}td{padding:4px}</style><table><tbody>` +
			rep(4000, func(i int) string {
				return fmt.Sprintf(`<tr id="r%d"><td class="c%d"><p>Row %d body copy here</p></td></tr>`, i, i%200, i)
			}) + `</tbody></table>`
	}},
}

func BenchmarkShapes(b *testing.B) {
	in := mustNew(b)
	for _, s := range shapes {
		doc := []byte(s.gen())
		b.Run(s.name, func(b *testing.B) {
			b.SetBytes(int64(len(doc)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := in.InlineBytes(doc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestShapesAgree keeps the Go and Node shape corpora identical. Without it
// the cross-implementation comparison is unfalsifiable.
func TestShapesAgree(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	if _, err := os.Stat("testdata/oracle/node_modules"); err != nil {
		t.Skip("oracle not installed; run `make goldens`")
	}
	out, err := exec.Command(node, "--input-type=module", "-e",
		`import {shapes} from "./testdata/oracle/shapes.mjs";
		 const o = {}; for (const [k,g] of Object.entries(shapes)) o[k] = g();
		 process.stdout.write(JSON.stringify(o))`).Output()
	if err != nil {
		t.Fatalf("running shapes.mjs: %v", err)
	}
	var js map[string]string
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatal(err)
	}
	if len(js) != len(shapes) {
		t.Errorf("shape count differs: Go has %d, Node has %d", len(shapes), len(js))
	}
	for _, s := range shapes {
		want, ok := js[s.name]
		if !ok {
			t.Errorf("shape %q missing from shapes.mjs", s.name)
			continue
		}
		if got := s.gen(); got != want {
			t.Errorf("shape %q differs: Go %d bytes, Node %d bytes", s.name, len(got), len(want))
		}
	}
}
