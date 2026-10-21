package mensch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMensch checks the port against mensch's own test inputs, with goldens
// from pinned mensch (testdata/mensch).
func TestMensch(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "mensch", "in", "*.css"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no mensch inputs: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".css")
		t.Run(name, func(t *testing.T) {
			css, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mensch", "out", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var want struct {
				Tokens []struct{ Type, Name, Value, Text, Prefix string }
				Text   []string
			}
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}

			toks := Lex(string(css))
			if len(toks) != len(want.Tokens) {
				t.Errorf("%d tokens, want %d", len(toks), len(want.Tokens))
			}
			for i := range min(len(toks), len(want.Tokens)) {
				g, w := toks[i], want.Tokens[i]
				if g != (Token{w.Type, w.Name, w.Value, w.Text, w.Prefix}) {
					t.Fatalf("token %d: got %+v, want %+v", i, g, w)
				}
			}

			nodes := Parse(toks)
			if len(nodes) != len(want.Text) {
				t.Fatalf("%d nodes, want %d", len(nodes), len(want.Text))
			}
			for i, n := range nodes {
				if got := string(Stringify(n)); got != want.Text[i] {
					t.Errorf("node %d:\n got %q\nwant %q", i, got, want.Text[i])
				}
			}
		})
	}
}
