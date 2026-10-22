package selector

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	for _, c := range []struct {
		sel      string
		relative bool
		want     bool
	}{
		{"p", false, true},
		{".a > p + i ~ b", false, true},
		{"*", false, true},
		{"* a", false, true},
		{"*a", false, false},
		{"svg|a", false, true},
		{"*|a", false, true},
		{"td[width=600]", false, false},
		{"td[width='600']", false, true},
		{"td[width=a600 i]", false, true},
		{`a[href="x"i]`, false, true},
		{"[data-x]", false, true},
		{"[x=-1]", false, false},
		{"p +", false, false},
		{"+ p", false, false},
		{"+ p", true, true},
		{"p + > i", false, false},
		{";a", false, false},
		{")a", false, false},
		{"&", false, true},
		{"& p", false, true},
		{"div&", true, true},
		{".a :not(p +)", false, false},
		{".a :is(p +, i)", false, true},
		{".a :where(+ e)", false, true},
		{":has(> p)", false, true},
		{":not(> p)", false, false},
		{"p:nth-child(2n + 1)", false, true},
		{"a::before", false, true},
		{".5x", false, false},
		{`.\31 0`, false, true},
		{`#\31 23`, false, true},
		{`:not(.\31 0)`, false, true},
		{"a/**/b", false, false},
		{"[a", false, false},
		{"a:", false, false},
	} {
		if got := Valid([]byte(c.sel), c.relative); got != c.want {
			t.Errorf("Valid(%q, %v) = %v, want %v", c.sel, c.relative, got, c.want)
		}
	}
}

func TestPrune(t *testing.T) {
	for sel, want := range map[string]string{
		".b :where(p +)":        "",
		".b :is(i, p >)":        ".b i",
		".c :is(+ e)":           "",
		".a :not(:is(p ~))":     ".a *",
		".a :is(p, :is(i +))":   ".a p|.a :not(*)",
		":is(.a, .b) :is(i, p)": ".a i|.a p|.b i|.b p",
	} {
		var got []string
		for _, v := range Compile([]byte(sel), Mode{}).Variants {
			got = append(got, v.Text)
		}
		if g := strings.Join(got, "|"); g != want {
			t.Errorf("Compile(%q) = %q, want %q", sel, g, want)
		}
	}
}
