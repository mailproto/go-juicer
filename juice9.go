package juicer

import "strings"

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
	for _, n := range menschParse(menschLex(string(src))) {
		if n.typ != "rule" {
			if preserveKind(n.typ, opts) {
				keep = append(keep, preserved{n.typ, menschStringify(n)})
			}
			continue
		}
		var decls []decl
		for _, d := range n.decls {
			if d.typ != "property" {
				continue
			}
			v, important := strings.CutSuffix(d.value, "!important")
			if important {
				v = strings.TrimRight(v, " \t\n\r\f\v")
			} else {
				v = d.value
			}
			decls = append(decls, decl{prop: d.name, value: []byte(v), important: important, bang: []byte(d.value), ord: ord})
			ord++
		}
		if len(decls) > 0 {
			for _, sel := range n.selectors {
				if sel == "" {
					continue
				}
				armRules, _ := compileRule([]byte(sel), decls, true)
				for k := range armRules {
					armRules[k].group = int32(len(rules))
				}
				rules = append(rules, armRules...)
			}
		}
		if opts.preservePseudos && menschPseudo(n.selectors[0]) {
			keep = append(keep, preserved{"pseudo", menschStringify(n)})
		}
	}
	return rules, keep, ord
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

// juice9Specificity is juice 9's slick-based score: every pseudo-class and
// pseudo-element counts as a type selector, :is() and :where() included,
// except :not(), which scores as the first selector in its argument.
func juice9Specificity(arm []byte) [3]int {
	var spec [3]int
	compound := false // a tag already counted in this compound
	for i := 0; i < len(arm); {
		switch c := arm[i]; {
		case c == '#':
			spec[0]++
			i = identEnd(arm, i+1)
		case c == '.':
			spec[1]++
			i = identEnd(arm, i+1)
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
			if !compound {
				spec[2]++
			}
			compound = true
			i = identEnd(arm, i)
		default:
			if isCSSSpace(c) || c == '>' || c == '+' || c == '~' {
				compound = false
			}
			i++
		}
	}
	return spec
}
