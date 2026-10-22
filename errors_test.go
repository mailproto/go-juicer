package juicer

import (
	"errors"
	"strings"
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
		{"unreadable content", `<style>p::before{content:var(--missing)}</style><p>x</p>`, []Option{InlinePseudoElements(true), CompatibilityMode(Juice12)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := mustNew(t, c.opts...).Inline(c.doc)
			if !errors.Is(err, ErrJuiceFails) {
				t.Errorf("got %v, want an error wrapping ErrJuiceFails", err)
			}
		})
	}
}

// The default mode renders content juice cannot read as a browser does.
func TestUnreadableContent(t *testing.T) {
	for doc, want := range map[string]string{
		`<style>p::before{content:var(--missing)}</style><p>x</p>`:                               `<p>x</p>`,
		`<style>p{counter-reset:n -1}p::before{content:counter(n, lower-alpha)}</style><p>x</p>`: `<p style="counter-reset: n -1;"><span>-1</span>x</p>`,
	} {
		got, err := mustNew(t, InlinePseudoElements(true)).Inline(doc)
		if err != nil || got != want {
			t.Errorf("%s\n got %q, %v\nwant %q", doc, got, err, want)
		}
	}
}

func TestErrLimitExceeded(t *testing.T) {
	deep := func(open string, n int) string { return strings.Repeat(open, n) }
	for _, c := range []struct {
		name, doc string
		opts      []Option
	}{
		{"input size", "<p>" + strings.Repeat("x", 100) + "</p>", []Option{MaxInputSize(64)}},
		{"element depth", "<style>b{color:red}</style>" + deep("<b>", 1025), nil},
		{"stylesheet depth", "<style>" + deep("@media print{", 1025) + "</style><p>x</p>", nil},
		{"selector depth", "<style>" + deep(":is(", 1025) + "p{color:red}</style><p>x</p>", nil},
		{"depth across stylesheets", "<style>" + deep("@media print{", 600) + "</style><style>" + deep("@media print{", 600) + "</style>", nil},
		{"extra CSS depth", "<p>x</p>", []Option{ExtraCSS(deep("a{", 1025))}},
		{"style attribute depth", `<style>p{color:red}</style><p style="` + deep("{", 1025) + `">x</p>`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := mustNew(t, c.opts...).Inline(c.doc)
			if !errors.Is(err, ErrLimitExceeded) {
				t.Errorf("got %v, want an error wrapping ErrLimitExceeded", err)
			}
		})
	}
	// At the limit, and with it removed, the document is inlined.
	for _, opts := range [][]Option{nil, {MaxDepth(0), MaxInputSize(0)}} {
		if _, err := mustNew(t, opts...).Inline(deep("<b>", 1024)); err != nil {
			t.Errorf("%v: %v", opts, err)
		}
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
		"negative limit":  MaxDepth(-1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(opt); !errors.Is(err, ErrInvalidOption) || errors.Is(err, ErrJuiceFails) {
				t.Errorf("got %v, want an error wrapping only ErrInvalidOption", err)
			}
		})
	}
}
