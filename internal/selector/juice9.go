package selector

import (
	"slices"
	"strings"
)

// Where juice 9 reads selectors differently from juice 12.

// Juice9IgnoredPseudo is juice 9's matchesPseudo, which looks for "hover" and
// the rest anywhere in the selector, without the colon.
func Juice9IgnoredPseudo(sel string) bool {
	return slices.ContainsFunc(ignoredPseudos, func(p string) bool { return strings.Contains(sel, p) })
}

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

// juice9Arguments rewrites the pseudo-classes inside arguments that juice 9
// leaves to css-select, since it skips only those outside them: :hover,
// :active and :visited match nothing, :link is a link with an href, and
// css-select rejects :focus, so the rule is dropped.
func juice9Arguments(a *Arm) (*Arm, bool) {
	ts, changed, ok := juice9Rewrite(a.Tokens, false)
	if !ok || !changed {
		return a, ok
	}
	return newArm(ts), true
}

func juice9Rewrite(ts []Token, inArg bool) (out []Token, changed, ok bool) {
	for k, t := range ts {
		var with []Token
		switch {
		case t.Kind != Pseudo:
		case inArg && !t.is(double) && (t.Name == "hover" || t.Name == "active" || t.Name == "visited"):
			with = Parse([]byte(":not(*)")).Tokens
		case inArg && !t.is(double) && t.Name == "link":
			with = Parse([]byte(":is(a,area,link)[href]")).Tokens
		case inArg && !t.is(double) && t.Name == "focus":
			return nil, false, false
		case t.is(call):
			var args []*Arm
			for i, a := range t.Args {
				ats, ch, ok := juice9Rewrite(a.Tokens, true)
				if !ok {
					return nil, false, false
				}
				if ch && args == nil {
					args = slices.Clone(t.Args)
				}
				if ch {
					args[i] = newArm(ats)
				}
			}
			if args != nil {
				d := t
				d.Args = args
				with = []Token{rebuilt(d)}
			}
		}
		if with == nil {
			if changed {
				out = append(out, t)
			}
			continue
		}
		if !changed {
			out = append(out, ts[:k]...)
			changed = true
		}
		out = append(out, with...)
	}
	if !changed {
		return ts, false, true
	}
	return rejoin(out), true, true
}

// juice9PseudoLast reports whether any ::before or ::after is where juice 9
// looks for one: on the last compound, outside arguments. Anywhere else it
// reaches css-select, which rejects it, and the rule is dropped.
func juice9PseudoLast(a *Arm) bool {
	cs := lastCompound(a.Tokens)
	if pseudoOf(a.Tokens[:cs]) != None {
		return false
	}
	tail := a.Tokens[cs:]
	if pseudoOf(tail) == None {
		return true
	}
	return slices.ContainsFunc(tail, func(t Token) bool {
		return t.Kind == Pseudo && (t.Name == "before" || t.Name == "after")
	})
}

// juice9Tail is a selector as juice 9 matches it once its trailing
// pseudo-element is stripped: what precedes it is kept as a combinator, so
// ".c ::before" is ".c *" and "::after" alone is "*".
func juice9Tail(va *Arm, text string) string {
	tail := &Arm{Tokens: va.Tokens[lastCompound(va.Tokens):]}
	in := analyze(tail, false)
	if in.ignored || in.pseudo == None || !in.empty || strings.HasSuffix(text, "*") {
		return text
	}
	return strings.TrimSpace(text + " *")
}

// juice9Specificity is juice 9's slick-based score: every pseudo-class and
// pseudo-element counts as a type selector, :is() and :where() included,
// except :not(), which scores as the first selector in its argument.
func juice9Specificity(a *Arm) [3]int {
	var spec [3]int
	// slick keeps one tag and one id per compound, and each class once.
	var tag, id bool
	var classes []string
	for _, t := range a.Tokens {
		switch t.Kind {
		case ID:
			if !id {
				spec[0]++
			}
			id = true
		case Class:
			if name := string(t.Text[1:]); !slices.Contains(classes, name) {
				classes = append(classes, name)
				spec[1]++
			}
		case Attr:
			spec[1]++
		case Pseudo:
			if t.Name != "not" {
				spec[2]++
			} else if len(t.Args) > 0 {
				s := juice9Specificity(t.Args[0])
				spec[0], spec[1], spec[2] = spec[0]+s[0], spec[1]+s[1], spec[2]+s[2]
			}
		case Type:
			if !tag {
				spec[2]++
			}
			tag = true
		case Other:
			if c := t.Text[0]; isCSSSpace(c) || isComb(c) {
				tag, id, classes = false, false, classes[:0]
			}
		}
	}
	return spec
}

// lastCompound is the index of the first token after the last top-level
// combinator.
func lastCompound(ts []Token) int {
	cs := 0
	for k, t := range ts {
		if combinator(t) {
			cs = k + 1
		}
	}
	return cs
}

// pseudoOf is the pseudo-element of a selector juice does not skip.
func pseudoOf(ts []Token) PseudoElement {
	in := analyze(&Arm{Tokens: ts}, false)
	if in.ignored {
		return None
	}
	return in.pseudo
}
