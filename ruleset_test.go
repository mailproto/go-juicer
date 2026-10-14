package juicer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mailproto/go-juicer/internal/htmlparser2"
	"golang.org/x/net/html"
)

// TestIndexMatchesBruteForce is the permanent invariant behind ruleset.go.
//
// A wrong rightmost key does not fail loudly: it silently drops matches, so
// some elements just lose styles. Diffing the index against testing every
// rule at every node is the only cheap way to keep it honest.
func TestIndexMatchesBruteForce(t *testing.T) {
	docs := corpusDocuments(t)
	docs = append(docs, benchDoc())

	for i, doc := range docs {
		root := htmlparser2.Parse([]byte(doc), false)
		o := defaults()

		var rules []rule
		var ord uint32
		walk(root, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "style" &&
				n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
				var r []rule
				r, _, ord = parseStylesheet([]byte(n.FirstChild.Data), &o, ord)
				rules = append(rules, r...)
			}
		})
		if len(rules) == 0 {
			continue
		}

		rs := newRuleSet(rules)
		var sc scratch
		var indexed, brute []string
		walk(root, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			rs.forEach(n, &sc, func(idx int, r *rule) {
				indexed = append(indexed, n.Data+"|"+r.sel)
			})
			for j := range rules {
				if rules[j].match != nil && rules[j].match.Match(n) {
					brute = append(brute, n.Data+"|"+rules[j].sel)
				}
			}
		})

		if strings.Join(indexed, "\n") != strings.Join(brute, "\n") {
			t.Errorf("document %d: index and brute force disagree\n  indexed %d pairs\n  brute   %d pairs\n%s",
				i, len(indexed), len(brute), firstDivergence(indexed, brute))
		}
	}
}

func firstDivergence(a, b []string) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return "  first divergence at " + itoa(i) + ": indexed=" + a[i] + " brute=" + b[i]
		}
	}
	if len(a) < len(b) {
		return "  index is missing: " + b[len(a)]
	}
	if len(b) < len(a) {
		return "  index has extra: " + a[len(b)]
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func corpusDocuments(t *testing.T) []string {
	t.Helper()
	var out []string
	root := filepath.Join("testdata", "in")
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
