package juicer

import (
	"errors"
	"testing"
)

// mustNew is New for options a test knows are valid.
func mustNew(tb testing.TB, opts ...Option) *Inliner {
	tb.Helper()
	in, err := New(opts...)
	if err != nil {
		tb.Fatal(err)
	}
	return in
}

func TestErrJuiceFails(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		opts      []Option
	}{
		{"juice 9 nested rule", `<style>.a{color:red;.b{margin:0}}</style><p class="a">x</p>`, []Option{CompatibilityMode(Juice9)}},
		{"juice 9 style attribute", `<style>p{color:red}</style><p style="a{">x</p>`, []Option{CompatibilityMode(Juice9)}},
		{"unreadable content", `<style>p::before{content:var(--missing)}</style><p>x</p>`, []Option{InlinePseudoElements(true)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := mustNew(t, c.opts...).Inline(c.doc)
			if !errors.Is(err, ErrJuiceFails) {
				t.Errorf("got %v, want an error wrapping ErrJuiceFails", err)
			}
		})
	}
}

func TestErrInvalidOption(t *testing.T) {
	for name, opt := range map[string]Option{
		// juice throws on a selector cheerio cannot parse, but only once it
		// has preserved extra CSS to insert.
		"selector":        InsertPreservedExtraCSSInto("p["),
		"compatibility":   CompatibilityMode(Compatibility(99)),
		"code block":      CodeBlocks([]CodeBlock{{"", ""}}),
		"style attribute": StyleAttributeName(""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(opt); !errors.Is(err, ErrInvalidOption) || errors.Is(err, ErrJuiceFails) {
				t.Errorf("got %v, want an error wrapping only ErrInvalidOption", err)
			}
		})
	}
}
