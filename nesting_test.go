package juicer

import "testing"

// Expectations are postcss-nesting 14.0.1's output, which juice 12 uses.
func TestFlattenNesting(t *testing.T) {
	cases := []struct{ in, want string }{
		{`.a{color:red; & .b{x:1}}`, ".a{color:red;}\n.a .b{x:1}\n"},
		{`.a{color:red; .b{x:1}}`, ".a{color:red;}\n.a .b{x:1}\n"},
		{`.a{& > .b{x:1}}`, ".a > .b{x:1}\n"},
		{`.a{> .b{x:1}}`, ".a  > .b{x:1}\n"},
		{`.a, .c{& .b{x:1}}`, ":is(.a,.c) .b{x:1}\n"},
		{`.a{.b, .c{x:1}}`, ".a .b,.a .c{x:1}\n"},
		{`.a{&.b{x:1}}`, ".a.b{x:1}\n"},
		{`.a{.b &{x:1}}`, ".b .a{x:1}\n"},
		{`.a{& &{x:1}}`, ".a .a{x:1}\n"},
		{`div{&:hover{x:1}}`, "div:hover{x:1}\n"},
		{`.a .b{&:hover{x:1}}`, ":is(.a .b):hover{x:1}\n"},
		{`.a{x:1; .b{y:2} z:3}`, ".a{x:1;}\n.a .b{y:2}\n.a{z:3}\n"},
		{`.a{x:1; .b{y:2; .c{z:3}}}`, ".a{x:1;}\n.a .b{y:2;}\n:is(.a .b) .c{z:3}\n"},
		{`body{color:black; @media (min-width:600px){color:navy}}`, "body{color:black;}\n@media (min-width:600px){body{color:navy}}\n"},
		{`body{@media (a){.x{c:1}}}`, "@media (a){body .x{c:1}}\n"},
		{`@media (a){.p{.q{c:1}}}`, "@media (a){.p .q{c:1}}\n"},
		{`.a{@media (a){@media (b){c:1}}}`, "@media (a){@media (b){.a{c:1}}}\n"},
		{`.a{@font-face{c:1}}`, ".a{@font-face{c:1}}\n"},
		{`.a{&{c:1}}`, ".a{c:1}\n"},
		{`.a::before{&:hover{c:1}}`, ":is(.a::before):hover{c:1}\n"},
		{`.a{h1&{c:1}}`, "h1.a{c:1}\n"},
		{`.a:is(.b,.c){& .d{c:1}}`, ".a:is(.b,.c) .d{c:1}\n"},
		{`.a{/*c*/ & .b{x:1}}`, "/*c*/\n.a .b{x:1}\n"},
		{`div{span&{c:1}}`, "span:is(div){c:1}\n"},
		{`*{&*{c:1}}`, "*{c:1}\n"},
		{`.a{:has(&){c:1}}`, ":has(:is(.a)){c:1}\n"},
		{`.a{:not(&) .b{c:1}}`, ":not(.a) .b{c:1}\n"},
		{`.a, .b{& + &{c:1}}`, ":is(.a,.b) + :is(.a,.b){c:1}\n"},
		{`li{&:nth-child( 2n + 1 ){c:1}}`, "li:nth-child(2n + 1){c:1}\n"},
		{`.a > .b{&.c{c:1}}`, ".c:is(.a > .b){c:1}\n"},
		{`#x{&[data-x="a b"]{c:1}}`, "#x[data-x=\"a b\"]{c:1}\n"},
		{`.a{c:1;&{d:2} e:3}`, ".a{c:1;}\n.a{d:2;e:3}\n"},
		{`.a{@layer x;}`, "@layer x;\n"},
		{`.a{@media print{.b{c:1} d:2}}`, "@media print{.a .b{c:1}.a{d:2}}\n"},
		{`& .x{c:1}`, ":scope .x{c:1}\n"},
		{`.a{.b|c{c:1}}`, ".a{.b|c{c:1}}\n"},
		{`.a{&.b.c#d:hover::before{c:1}}`, "#d.a.b.c:hover::before{c:1}\n"},
		{`.a{.b{}}`, ".a .b{}\n"},
		{`.a{&{}}`, ".a{}\n"},
		{`.a{@scope (.x){& .y{c:1}}}`, ".a{@scope (.x){& .y{c:1}}}\n"},
		{`p:hover{a:b; .x{c:d} e:f};`, "p:hover{a:b;};\np:hover .x{c:d}\np:hover{e:f};\n"},
		{`p:hover{@media print{c:d}};`, "@media print{p:hover{c:d};}\n"},
		{`p{.x:hover{c:d} ;}`, "p .x:hover{c:d} ;\n"},
		// Nothing nested: returned as is.
		{`a{background:url(x?a=1&b=2)} @media (a){p{c:1}}`, `a{background:url(x?a=1&b=2)} @media (a){p{c:1}}`},
	}
	for _, c := range cases {
		if got := string(flattenNesting([]byte(c.in))); got != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}
