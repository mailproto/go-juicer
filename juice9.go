package juicer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mailproto/go-juicer/internal/mensch"
)

// Where the Juice9 profile reads stylesheets and selectors differently from
// juice 12.

// balanced reports whether every parenthesis and bracket in a selector
// closes, outside strings.
func balanced(sel []byte) bool {
	var stack []byte
	var quote byte
	for i := 0; i < len(sel); i++ {
		c := sel[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(' || c == '[':
			stack = append(stack, c)
		case c == ')' || c == ']':
			open := byte('(')
			if c == ']' {
				open = '['
			}
			if len(stack) == 0 || stack[len(stack)-1] != open {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0 && quote == 0
}

// parseMensch is parseStylesheet for the Juice9 profile: juice 9 reads
// stylesheets with mensch and preserves rules through its stringifier.
func parseMensch(src []byte, opts *options, ord uint32) ([]rule, []preserved, uint32) {
	var rules []rule
	var keep []preserved
	for _, n := range mensch.Parse(mensch.Lex(string(src))) {
		if n.Type != "rule" {
			if preserveKind(n.Type, opts) {
				keep = append(keep, preserved{n.Type, mensch.Stringify(n)})
			}
			continue
		}
		var decls []decl
		var throws bool
		decls, throws, ord = menschDecls(n, ord)
		// An empty rule still counts: juice 9 rewrites the style attribute of
		// every element a rule matches.
		for _, sel := range n.Selectors {
			if sel == "" {
				continue
			}
			armRules, _ := compileRule([]byte(sel), decls, opts)
			for k := range armRules {
				armRules[k].group = int32(len(rules))
				armRules[k].throws = throws
			}
			rules = append(rules, armRules...)
		}
		if opts.preservePseudos && menschPseudo(n.Selectors[0]) {
			keep = append(keep, preserved{"pseudo", mensch.Stringify(n)})
		}
	}
	return rules, keep, ord
}

// menschDecls reads a rule's declarations as juice 9's addProps does. bang
// holds the value as written, which juice 9 keeps when preserving !important.
func menschDecls(n mensch.Node, ord uint32) (decls []decl, throws bool, _ uint32) {
	for _, d := range n.Decls {
		if d.Type != "property" {
			continue
		}
		if d.Value == "" {
			// mensch reads "a{b{" as a declaration b with no value, which
			// juice 9 reads with value.match and throws on.
			throws = true
			continue
		}
		v, important := strings.CutSuffix(d.Value, "!important")
		if important {
			v = strings.TrimRightFunc(v, mensch.JSSpace)
		} else {
			v = d.Value
		}
		name := d.Name
		if name == "" {
			// A word with no colon, which juice 9 writes out as it reads it.
			name = "undefined"
		}
		decls = append(decls, decl{prop: name, value: []byte(v), important: important, bang: []byte(d.Value), ord: ord})
		ord++
	}
	return decls, throws, ord
}

// menschStyle reads a style attribute as juice 9 does, through mensch as the
// body of a "*" rule.
func menschStyle(style []byte) ([]decl, error) {
	for _, n := range mensch.Parse(mensch.Lex("* { " + string(style) + " } ")) {
		if n.Type != "rule" {
			continue
		}
		decls, throws, _ := menschDecls(n, 0)
		if throws {
			break
		}
		return decls, nil
	}
	return nil, fmt.Errorf("%w: juice 9 cannot read the style attribute %q", ErrJuiceFails, style)
}

// menschPseudo is juice 9's matchesPseudo, which looks for "hover" and the
// rest anywhere in the selector, without the colon.
func menschPseudo(sel string) bool {
	for p := range ignoredPseudos {
		if strings.Contains(sel, p) {
			return true
		}
	}
	return false
}

// juice9Arguments rewrites the pseudo-classes inside arguments that juice 9
// leaves to css-select, since it skips only those outside them: :hover,
// :active and :visited match nothing, :link is a link with an href, and
// css-select rejects :focus, so the rule is dropped.
func juice9Arguments(arm []byte) ([]byte, bool) {
	var out []byte
	depth := 0
	for i := 0; i < len(arm); {
		c := arm[i]
		switch {
		case c == '[':
			j := attrEnd(arm, i)
			out = append(out, arm[i:j]...)
			i = j
			continue
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ':' && depth > 0 && i+1 < len(arm) && arm[i+1] != ':':
			ne := identEnd(arm, i+1)
			switch strings.ToLower(string(arm[i+1 : ne])) {
			case "hover", "active", "visited":
				out = append(out, ":not(*)"...)
				i = ne
				continue
			case "link":
				out = append(out, ":is(a,area,link)[href]"...)
				i = ne
				continue
			case "focus":
				return nil, false
			}
		}
		out = append(out, c)
		i++
	}
	return out, true
}

// juice9PseudoLast reports whether any ::before or ::after is where juice 9
// looks for one: on the last compound, outside arguments. Anywhere else it
// reaches css-select, which rejects it, and the rule is dropped.
func juice9PseudoLast(arm []byte) bool {
	t := trimCSS(arm)
	tail := rightmostCompound(string(t))
	if _, _, p, _ := scanSelector(t[:len(t)-len(tail)], false); p != pseudoNone {
		return false
	}
	if _, _, p, _ := scanSelector([]byte(tail), false); p == pseudoNone {
		return true
	}
	depth := 0
	for i := 0; i < len(tail); i++ {
		switch tail[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ':':
			j := i + 1
			if j < len(tail) && tail[j] == ':' {
				j++
			}
			name := strings.ToLower(tail[j:identEnd([]byte(tail), j)])
			if depth == 0 && (name == "before" || name == "after") {
				return true
			}
		}
	}
	return false
}

// juice9Tail is a selector as juice 9 matches it once its trailing
// pseudo-element is stripped: what precedes it is kept as a combinator, so
// ".c ::before" is ".c *" and "::after" alone is "*".
func juice9Tail(variant []byte, text string) string {
	tail := rightmostCompound(string(trimCSS(variant)))
	if t, _, p, _ := scanSelector([]byte(tail), false); p == pseudoNone || t != "" || strings.HasSuffix(text, "*") {
		return text
	}
	return strings.TrimSpace(text + " *")
}

// juice9Specificity is juice 9's slick-based score: every pseudo-class and
// pseudo-element counts as a type selector, :is() and :where() included,
// except :not(), which scores as the first selector in its argument.
func juice9Specificity(arm []byte) [3]int {
	var spec [3]int
	// slick keeps one tag and one id per compound, and each class once.
	var tag, id bool
	var classes []string
	for i := 0; i < len(arm); {
		switch c := arm[i]; {
		case c == '#':
			if !id {
				spec[0]++
			}
			id = true
			i = identEnd(arm, i+1)
		case c == '.':
			j := identEnd(arm, i+1)
			if name := string(arm[i+1 : j]); !slices.Contains(classes, name) {
				classes = append(classes, name)
				spec[1]++
			}
			i = j
		case c == '[':
			spec[1]++
			i = attrEnd(arm, i)
		case c == ':':
			j := i + 1
			if j < len(arm) && arm[j] == ':' {
				j++
			}
			ne := identEnd(arm, j)
			name := strings.ToLower(string(arm[j:ne]))
			e := ne
			var arg []byte
			if ne < len(arm) && arm[ne] == '(' {
				e = parenEnd(arm, ne)
				arg = arm[ne+1 : max(e-1, ne+1)]
			}
			if name == "not" {
				if arms := splitSelector(arg); len(arms) > 0 {
					s := juice9Specificity(arms[0])
					spec[0], spec[1], spec[2] = spec[0]+s[0], spec[1]+s[1], spec[2]+s[2]
				}
			} else {
				spec[2]++
			}
			i = e
		case c == '*':
			i++
		case isSelectorNameStart(c):
			if !tag {
				spec[2]++
			}
			tag = true
			i = identEnd(arm, i)
		default:
			if isCSSSpace(c) || c == '>' || c == '+' || c == '~' {
				tag, id, classes = false, false, classes[:0]
			}
			i++
		}
	}
	return spec
}
