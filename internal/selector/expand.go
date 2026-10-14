package selector

import (
	"bytes"
	"slices"
	"strings"
)

// maxExpansion caps the cartesian blow-up from nested :is() lists. A list
// that would take it past the cap is kept whole instead, as :not(:not()).
const maxExpansion = 32

// expand rewrites :is(), :where() and :matches() lists into the equivalent
// set of plain selectors, which cascadia can parse. Each pass expands the
// first list left in each candidate.
//
// An alternative with combinators can only be spliced in where the list
// starts the selector: "div > :is(.a .b)" is not "div > .a .b". Elsewhere it
// becomes ":not(:not(.a .b))".
func expand(ts []Token) [][]Token {
	out := [][]Token{ts}
	for {
		grew := false
		var next [][]Token
		for _, cand := range out {
			at := -1
			for k, t := range cand {
				if t.Kind == Pseudo && t.is(call) && (t.Name == "is" || t.Name == "where" || t.Name == "matches") {
					at = k
					break
				}
			}
			if at < 0 || !cand[at].is(closed) {
				next = append(next, cand)
				continue
			}
			grew = true
			alts := cand[at].Args
			if len(next)+len(alts) > maxExpansion {
				// Past the cap the list stays one selector, which matches
				// exactly what the list does.
				next = append(next, splice(cand, at, at+1, notNot(alts)))
				continue
			}
			for _, alt := range alts {
				if at > 0 && alt.comb {
					next = append(next, splice(cand, at, at+1, notNot([]*Arm{alt})))
				} else if v, ok := spliceAlternative(cand, at, alt); ok {
					next = append(next, v)
				}
			}
		}
		out = next
		if !grew {
			for i, v := range out {
				out[i] = expandArguments(v)
			}
			return out
		}
	}
}

// notNot is ":not(:not(alts))", which matches what the list does.
func notNot(alts []*Arm) []Token {
	inner := rebuilt(Token{Kind: Pseudo, Text: []byte(":not"), head: 4, Name: "not", flags: call, Args: alts})
	return []Token{rebuilt(Token{Kind: Pseudo, Text: []byte(":not"), head: 4, Name: "not", flags: call, Args: []*Arm{newArm([]Token{inner})}})}
}

// splice replaces cand[from:to] with mid.
func splice(cand []Token, from, to int, mid []Token) []Token {
	v := make([]Token, 0, len(cand)-(to-from)+len(mid))
	v = append(append(append(v, cand[:from]...), mid...), cand[to:]...)
	return rejoin(v)
}

// spliceAlternative replaces the list at cand[at] with one of its
// alternatives. A type selector in the alternative has to lead its compound,
// so "p.c:where(p)" becomes "p.c" rather than "p.cp", and one that conflicts
// with the compound's own type can never match (ok is false).
func spliceAlternative(cand []Token, at int, alt *Arm) ([]Token, bool) {
	ts := alt.Tokens
	if ts[0].Kind != Type && ts[0].Kind != Universal || alt.comb {
		return splice(cand, at, at+1, ts), true
	}
	cs := compoundStart(cand, at)
	altT := ts[0]
	var pre []Token
	switch pt := cand[cs]; {
	case cs == at || pt.Kind != Type && pt.Kind != Universal:
		pre = append(append(append(pre, cand[:cs]...), altT), cand[cs:at]...)
	case altT.Kind == Universal || bytes.EqualFold(pt.Text, altT.Text):
		pre = cand[:at:at]
	case pt.Kind == Universal:
		pre = append(append(append(pre, cand[:cs]...), altT), cand[cs+1:at]...)
	default:
		return nil, false
	}
	return rejoin(append(append(pre, ts[1:]...), cand[at+1:]...)), true
}

// compoundStart finds the token starting the compound that holds cand[at].
func compoundStart(cand []Token, at int) int {
	cs := 0
	for k := 0; k < at; k++ {
		t := cand[k]
		switch {
		case t.Kind == Other && t.Text[0] == '(':
			// A parenthesis closed before the list is skipped whole.
			depth, end := 0, -1
		scan:
			for j := k; j < at; j++ {
				if cand[j].Kind != Other {
					continue
				}
				switch cand[j].Text[0] {
				case '(':
					depth++
				case ')':
					if depth--; depth == 0 {
						end = j
						break scan
					}
				}
			}
			if end < 0 {
				cs = k + 1
			} else {
				k = end
			}
		case combinator(t):
			cs = k + 1
		}
	}
	return cs
}

// expandArguments expands lists inside the arguments of other functional
// pseudo-classes, so :not(:is(.a, .b)) becomes :not(.a,.b).
func expandArguments(ts []Token) []Token {
	var out []Token
	changed := false
	for k := range ts {
		t := ts[k]
		if t.Kind != Pseudo || !t.is(call) || !slices.ContainsFunc(t.Args, func(a *Arm) bool { return a.list }) {
			out = append(out, t)
			continue
		}
		var alts []*Arm
		for _, a := range t.Args {
			for _, v := range expand(a.Tokens) {
				alts = append(alts, newArm(v))
			}
		}
		changed = true
		switch {
		case len(alts) > 0:
			d := t
			d.Args = alts
			out = append(out, rebuilt(d))
		case t.Name == "not":
			// Nothing to exclude: matches any element.
			if k == 0 || prevEndsCompound(ts[k-1]) {
				out = append(out, Token{Kind: Universal, Text: []byte("*")})
			}
		default:
			out = append(out, t)
		}
	}
	if !changed {
		return ts
	}
	return rejoin(out)
}

func prevEndsCompound(t Token) bool {
	return t.Kind == Other && strings.IndexByte(" \t\n>+~(", t.Text[len(t.Text)-1]) >= 0
}
