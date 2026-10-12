package juicer

import "testing"

func TestAtRuleKind(t *testing.T) {
	for in, want := range map[string]string{
		"@media print":          "media",
		"@MEDIA print":          "MEDIA",
		"@container(min-width)": "container",
		"@-webkit-keyframes k":  "keyframes",
		"@-x-keyframes k":       "keyframes",
		"@KEYFRAMES k":          "KEYFRAMES",
		"@layer a, b":           "layer",
	} {
		if got := atRuleKind([]byte(in)); got != want {
			t.Errorf("atRuleKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInsertPreservedExtraCSSIntoInvalidSelector(t *testing.T) {
	// juice throws on a selector cheerio cannot parse.
	_, err := New(ExtraCSS("@media print{p{color:red}}"), InsertPreservedExtraCSSInto("p[")).Inline("<p>x</p>")
	if err == nil {
		t.Fatal("want an error for an invalid selector")
	}
}

func TestTokenizerInput(t *testing.T) {
	for in, want := range map[string]string{
		"p{a:b}; @media x{}":                "p{a:b}  @media x{}",
		"@container (x){p{}}":               "@media     (x){p{}}",
		"@starting-style{p{}}":              "@media         {p{}}",
		"p{content:'@container'}":           "p{content:'@container'}",
		"/* @container */@Container x{p{}}": "/* @container */@media     x{p{}}",
	} {
		if got, _ := tokenizerInput([]byte(in)); string(got) != want {
			t.Errorf("tokenizerInput(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"p{)a:b}":       "p{ a:b}",
		"p{a:b)]}":      "p{a:b  }",
		"p{a:f(x)}":     "p{a:f(x)}",
		"p{a:'('}q{)}":  "p{a:'('}q{ }",
		"p{a:[(])}q{)}": "p{a:[(])}q{)}",
		"/*)*/p{a:\\)}": "/*)*/p{a:\\)}",
	} {
		if _, got := tokenizerInput([]byte(in)); string(got) != want {
			t.Errorf("tokenizerInput(%q) tok = %q, want %q", in, got, want)
		}
	}
}
