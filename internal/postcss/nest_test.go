package postcss

import (
	"strings"
	"testing"
)

// TestNest checks flattening against postcss-nesting 14.0.1, which juice 12
// runs: each want is its result, written by compact.
func TestNest(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{".a{color:red; & .b{x:1}}", ".a{color:red;}\n.a .b{x:1;}"},
		{".a{color:red; .b{x:1}}", ".a{color:red;}\n.a .b{x:1;}"},
		{".a{& > .b{x:1}}", ".a > .b{x:1;}"},
		{".a{> .b{x:1}}", ".a  > .b{x:1;}"},
		{".a, .c{& .b{x:1}}", ":is(.a,.c) .b{x:1;}"},
		{".a{.b, .c{x:1}}", ".a .b,.a .c{x:1;}"},
		{".a{&.b{x:1}}", ".a.b{x:1;}"},
		{".a{.b &{x:1}}", ".b .a{x:1;}"},
		{".a{& &{x:1}}", ".a .a{x:1;}"},
		{"div{&:hover{x:1}}", "div:hover{x:1;}"},
		{".a .b{&:hover{x:1}}", ":is(.a .b):hover{x:1;}"},
		{".a{x:1; .b{y:2} z:3}", ".a{x:1;}\n.a .b{y:2;}\n.a{z:3;}"},
		{".a{x:1; .b{y:2; .c{z:3}}}", ".a{x:1;}\n.a .b{y:2;}\n:is(.a .b) .c{z:3;}"},
		{"body{color:black; @media (min-width:600px){color:navy}}", "body{color:black;}\n@media (min-width:600px){body{color:navy;}}"},
		{"body{@media (a){.x{c:1}}}", "@media (a){body .x{c:1;}}"},
		{"@media (a){.p{.q{c:1}}}", "@media (a){.p .q{c:1;}}"},
		{".a{@media (a){@media (b){c:1}}}", "@media (a){@media (b){.a{c:1;}}}"},
		{".a{@font-face{c:1}}", ".a{@font-face{c:1;}}"},
		{".a{&{c:1}}", ".a{c:1;}"},
		{".a::before{&:hover{c:1}}", ":is(.a::before):hover{c:1;}"},
		{".a{h1&{c:1}}", "h1.a{c:1;}"},
		{".a:is(.b,.c){& .d{c:1}}", ".a:is(.b,.c) .d{c:1;}"},
		{".a{/*c*/ & .b{x:1}}", "/*c*/\n.a .b{x:1;}"},
		{"div{span&{c:1}}", "span:is(div){c:1;}"},
		{"*{&*{c:1}}", "*{c:1;}"},
		{".a{:has(&){c:1}}", ":has(:is(.a)){c:1;}"},
		{".a{:not(&) .b{c:1}}", ":not(.a) .b{c:1;}"},
		{".a, .b{& + &{c:1}}", ":is(.a,.b) + :is(.a,.b){c:1;}"},
		{"li{&:nth-child( 2n + 1 ){c:1}}", "li:nth-child(2n + 1){c:1;}"},
		{".a > .b{&.c{c:1}}", ".c:is(.a > .b){c:1;}"},
		{"#x{&[data-x=\"a b\"]{c:1}}", "#x[data-x=\"a b\"]{c:1;}"},
		{".a{c:1;&{d:2} e:3}", ".a{c:1;}\n.a{d:2;e:3;}"},
		{".a{@layer x;}", "@layer x;"},
		{".a{@media print{.b{c:1} d:2}}", "@media print{.a .b{c:1;}.a{d:2;}}"},
		{"& .x{c:1}", ":scope .x{c:1;}"},
		{".a{.b|c{c:1}}", ".a{.b|c{c:1;}}"},
		{".a{&.b.c#d:hover::before{c:1}}", "#d.a.b.c:hover::before{c:1;}"},
		{".a{.b{}}", ".a .b{}"},
		{".a{&{}}", ".a{}"},
		{".a{@scope (.x){& .y{c:1}}}", ".a{@scope (.x){& .y{c:1;}}}"},
		{"p:hover{a:b; .x{c:d} e:f};", "p:hover{a:b;}\np:hover .x{c:d;}\np:hover{e:f;}"},
		{"p:hover{@media print{c:d}};", "@media print{p:hover{c:d;}}"},
		{"p{.x:hover{c:d} ;}", "p .x:hover{c:d;}"},
		{"a{background:url(x?a=1&b=2)} @media (a){p{c:1}}", "a{background:url(x?a=1&b=2);}\n@media (a){p{c:1;}}"},
		// Selectors postcss-selector-parser rejects stay where they are.
		{".a{!x{c:1}}", ".a{!x{c:1;}}"},
		{".a{b;c{d:1}}", ".a c{d:1;}"},
		{"&:is(a;){c:1}", "&:is(a;){c:1;}"},
	} {
		root := Parse(c.in)
		Nest(root)
		var parts []string
		for _, n := range root.Nodes {
			parts = append(parts, compact(n))
		}
		if got := strings.Join(parts, "\n"); got != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestParsesSelector(t *testing.T) {
	for sel, want := range map[string]bool{
		".a .b":         true,
		"a > b, c ~ d":  true,
		":is(.a,.b) p":  true,
		"[x=\"1\"] *|a": true,
		"!x":            false,
		"a;b":           false,
		"a]":            false,
		"a)":            false,
		":is(a":         false,
		"[a":            false,
		"[a^]":          false,
		"'a":            false,
		"a/*":           false,
		"|":             false,
		"a / b":         false,
		":":             false,
	} {
		if got := parsesSelector(sel); got != want {
			t.Errorf("parsesSelector(%q) = %v, want %v", sel, got, want)
		}
	}
}

// compact writes a node with no raws: selector{declarations}.
func compact(n *Node) string {
	var b strings.Builder
	switch n.Type {
	case "rule":
		b.WriteString(n.Selector + "{")
		for _, c := range n.Nodes {
			b.WriteString(compact(c))
		}
		b.WriteString("}")
	case "atrule":
		b.WriteString("@" + n.Name)
		if n.Params != "" {
			b.WriteString(" " + n.Params)
		}
		if !n.hasNodes {
			return b.String() + ";"
		}
		b.WriteString("{")
		for _, c := range n.Nodes {
			b.WriteString(compact(c))
		}
		b.WriteString("}")
	case "decl":
		b.WriteString(n.Prop + ":" + n.Value)
		if n.Important {
			b.WriteString("!important")
		}
		b.WriteString(";")
	case "comment":
		b.WriteString("/*" + n.Text + "*/")
	}
	return b.String()
}
