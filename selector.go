package juicer

import (
	"bytes"
	"strings"

	"github.com/andybalholm/cascadia"
)

// Selector handling. cascadia is used only as a match predicate: its
// Specificity() is a 3-vector that scores :hover and friends as zero and
// cannot represent :is()/:where(), so specificity is computed here instead.
// The scan is needed anyway for comma splitting, pseudo filtering and the
// index key, so closing cascadia's gaps costs little on top.

const (
	pseudoNone uint8 = iota
	pseudoBefore
	pseudoAfter
)

type rule struct {
	sel     string
	match   cascadia.Matcher
	spec    [3]int // ids, classes+attrs, types+pseudos
	pseudo  uint8
	order   uint32
	key     keyKind
	keyName string
	decls   []decl
}

type keyKind uint8

const (
	keyAny keyKind = iota
	keyID
	keyClass
	keyTag
)

// juice skips a whole comma-arm if any compound in it carries one of these,
// because they have no inline equivalent.
var ignoredPseudos = map[string]bool{
	"hover": true, "active": true, "focus": true, "visited": true, "link": true,
}

// nonVisualElements never receive a style attribute. This is an element-level
// test, not subtree pruning: juice styles a <b> inside <noscript>.
var nonVisualElements = map[string]bool{
	"head": true, "title": true, "base": true, "link": true,
	"style": true, "meta": true, "script": true, "noscript": true,
}

// splitSelector splits on commas that are not inside brackets, parens or
// quotes. juice splits arms before filtering pseudos, so "a:hover, a" still
// inlines its second arm.
func splitSelector(sel []byte) [][]byte {
	var out [][]byte
	depth := 0
	var quote byte
	start := 0
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
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			if a := trimCSS(sel[start:i]); len(a) > 0 {
				out = append(out, a)
			}
			start = i + 1
		}
	}
	if a := trimCSS(sel[start:]); len(a) > 0 {
		out = append(out, a)
	}
	return out
}

// maxExpansion caps the cartesian blow-up from nested :is() lists.
const maxExpansion = 32

// compileRule turns one comma-arm into rules. :is() and :where() are
// desugared into one rule per alternative, all carrying the specificity of
// the original arm, which is exactly how Selectors L4 scores them: the list
// contributes the maximum of its arguments, not the alternative that matched.
//
// It returns ignored=true for an arm juice preserves rather than inlines, and
// no rules at all for one neither engine can match.
func compileRule(arm []byte, decls []decl) (rules []rule, ignored bool) {
	_, spec, pseudo, ign := scanSelector(arm)
	if ign {
		return nil, true
	}

	for _, variant := range expandFunctional(arm) {
		text, _, _, vIgn := scanSelector(variant)
		if vIgn {
			continue
		}
		m, err := cascadia.Parse(text)
		if err != nil {
			// juice swallows selectors its engine cannot parse.
			continue
		}
		r := rule{sel: string(arm), decls: decls, spec: spec, pseudo: pseudo, match: m}
		r.key, r.keyName = rightmostKey(text)
		rules = append(rules, r)
	}
	return rules, false
}

// expandFunctional rewrites :is()/:where() selector lists into the equivalent
// set of plain selectors. cascadia cannot parse either, and expanding is
// exact here because the specificity comes from the original arm.
func expandFunctional(arm []byte) [][]byte {
	out := [][]byte{arm}
	for {
		grew := false
		var next [][]byte
		for _, cand := range out {
			at, end, ok := findSelectorList(cand)
			if !ok {
				next = append(next, cand)
				continue
			}
			open := bytes.IndexByte(cand[at:end], '(') + at
			if open <= at || end < open+2 || cand[end-1] != ')' {
				next = append(next, cand)
				continue
			}
			grew = true
			for _, alt := range splitSelector(cand[open+1 : end-1]) {
				if len(next) >= maxExpansion {
					break
				}
				v := make([]byte, 0, len(cand)+len(alt))
				v = append(v, cand[:at]...)
				v = append(v, alt...)
				v = append(v, cand[end:]...)
				next = append(next, v)
			}
		}
		out = next
		if !grew || len(out) >= maxExpansion {
			return out
		}
	}
}

// findSelectorList locates the first top-level :is()/:where()/:matches().
func findSelectorList(sel []byte) (start, end int, ok bool) {
	for i := 0; i < len(sel); i++ {
		switch sel[i] {
		case '[':
			i = attrEnd(sel, i) - 1
		case ':':
			j := i + 1
			if j < len(sel) && sel[j] == ':' {
				j++
			}
			ne := identEnd(sel, j)
			name := strings.ToLower(string(sel[j:ne]))
			if ne < len(sel) && sel[ne] == '(' {
				e := parenEnd(sel, ne)
				if name == "is" || name == "where" || name == "matches" {
					return i, e, true
				}
				i = e - 1
				continue
			}
			i = ne - 1
		}
	}
	return 0, 0, false
}

// scanSelector walks a single comma-arm, returning the text to hand cascadia
// (pseudo-elements stripped, :is()/:where() desugared away, unquoted
// attribute values quoted), its specificity, any pseudo-element, and whether
// it must be skipped.
func scanSelector(arm []byte) (text string, spec [3]int, pseudo uint8, ignored bool) {
	var out []byte
	i := 0
	for i < len(arm) {
		c := arm[i]
		switch {
		case c == '#':
			j := identEnd(arm, i+1)
			spec[0]++
			out = append(out, arm[i:j]...)
			i = j

		case c == '.':
			j := identEnd(arm, i+1)
			spec[1]++
			out = append(out, arm[i:j]...)
			i = j

		case c == '[':
			j := attrEnd(arm, i)
			spec[1]++
			out = append(out, quoteAttrValue(arm[i:j])...)
			i = j

		case c == ':':
			dbl := i+1 < len(arm) && arm[i+1] == ':'
			ns := i + 1
			if dbl {
				ns++
			}
			ne := identEnd(arm, ns)
			name := strings.ToLower(string(arm[ns:ne]))
			// Functional pseudo-classes carry an argument list.
			ae := ne
			var arg []byte
			if ne < len(arm) && arm[ne] == '(' {
				ae = parenEnd(arm, ne)
				// parenEnd runs to the end of input when the parenthesis is
				// never closed.
				if ae >= ne+2 && arm[ae-1] == ')' {
					arg = arm[ne+1 : ae-1]
				} else {
					arg = arm[ne+1 : ae]
				}
			}
			switch {
			case ignoredPseudos[name]:
				return "", spec, pseudoNone, true
			case name == "before" || name == "after":
				// Matched against the base selector; declarations are routed
				// to a side map so they cannot leak onto the element.
				if name == "before" {
					pseudo = pseudoBefore
				} else {
					pseudo = pseudoAfter
				}
				spec[2]++
			case name == "where":
				// Selectors L4: :where() contributes zero specificity. The
				// text is irrelevant because expandFunctional rewrites it.
			case name == "is" || name == "matches" || name == "not" || name == "has":
				// Selectors L4: these take the maximum specificity of
				// their arguments.
				s := argSpecificity(arg)
				spec[0] += s[0]
				spec[1] += s[1]
				spec[2] += s[2]
				out = append(out, rewriteHas(arm[i:ae], name)...)
			default:
				spec[2]++
				out = append(out, arm[i:ae]...)
			}
			i = ae

		case c == '*':
			out = append(out, c)
			i++

		case isSelectorNameStart(c):
			j := identEnd(arm, i)
			spec[2]++
			out = append(out, arm[i:j]...)
			i = j

		default:
			out = append(out, c)
			i++
		}
	}
	return string(bytes.TrimSpace(out)), spec, pseudo, false
}

// argSpecificity returns the highest specificity among a selector list, which
// is how Selectors L4 scores :is(), :not() and :has().
func argSpecificity(arg []byte) [3]int {
	var best [3]int
	for _, a := range splitSelector(arg) {
		_, s, _, ign := scanSelector(a)
		if ign {
			continue
		}
		if s[0] > best[0] ||
			(s[0] == best[0] && s[1] > best[1]) ||
			(s[0] == best[0] && s[1] == best[1] && s[2] > best[2]) {
			best = s
		}
	}
	return best
}

// rewriteHas maps :has(> x) onto cascadia's :haschild(x). Sibling forms stay
// unsupported and make cascadia.Parse fail, which drops the rule.
func rewriteHas(seg []byte, name string) []byte {
	if name != "has" {
		return seg
	}
	open := bytes.IndexByte(seg, '(')
	// As above: an unclosed parenthesis leaves no usable span.
	if open < 0 || len(seg) < 2 || seg[len(seg)-1] != ')' || open+1 > len(seg)-1 {
		return seg
	}
	inner := bytes.TrimSpace(seg[open+1 : len(seg)-1])
	if len(inner) > 0 && inner[0] == '>' {
		return append([]byte(":haschild("), append(bytes.TrimSpace(inner[1:]), ')')...)
	}
	return seg
}

// quoteAttrValue quotes a bare attribute value cascadia would reject.
// Browsers and postcss accept [width=600]; cascadia requires an identifier.
func quoteAttrValue(seg []byte) []byte {
	// attrEnd yields the rest of the input when the bracket is never closed;
	// there is nothing to rewrite in that case.
	if len(seg) < 2 || seg[len(seg)-1] != ']' {
		return seg
	}
	eq := bytes.IndexByte(seg, '=')
	if eq < 0 || eq+1 > len(seg)-1 {
		return seg
	}
	val := seg[eq+1 : len(seg)-1]
	trimmed := bytes.TrimSpace(val)
	// Strip a trailing case-insensitivity flag before inspecting the value.
	flag := []byte(nil)
	if n := len(trimmed); n >= 2 && (trimmed[n-1] == 'i' || trimmed[n-1] == 'I') && isCSSSpace(trimmed[n-2]) {
		flag = trimmed[n-2:]
		trimmed = bytes.TrimSpace(trimmed[:n-2])
	}
	if len(trimmed) == 0 || trimmed[0] == '"' || trimmed[0] == '\'' {
		return seg
	}
	if isIdent(trimmed) {
		return seg
	}
	out := append([]byte{}, seg[:eq+1]...)
	out = append(out, '"')
	out = append(out, trimmed...)
	out = append(out, '"')
	out = append(out, flag...)
	return append(out, ']')
}

func isIdent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if b[0] >= '0' && b[0] <= '9' {
		return false
	}
	if b[0] == '-' && len(b) > 1 && b[1] >= '0' && b[1] <= '9' {
		return false
	}
	for _, c := range b {
		if !(c == '-' || c == '_' || c >= 0x80 ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// rightmostKey picks the index bucket for a selector: the id, else a class,
// else the tag of its rightmost compound.
//
// Only simple selectors written directly in that compound count. A class
// inside a functional pseudo-class argument does not: p:not(.x) matches any p,
// so bucketing it under class "x" would silently lose every other match.
func rightmostKey(sel string) (keyKind, string) {
	last := rightmostCompound(sel)
	var class string
	for i := 0; i < len(last); {
		switch c := last[i]; {
		case c == '#':
			if n := identEndStr(last, i+1); n > i+1 {
				return keyID, last[i+1 : n]
			}
			i++
		case c == '.':
			n := identEndStr(last, i+1)
			if n > i+1 && class == "" {
				class = last[i+1 : n]
			}
			i = max(n, i+1)
		case c == '[':
			i = attrEnd([]byte(last), i)
		case c == ':':
			// Skip the pseudo name and any argument list wholesale.
			j := i + 1
			if j < len(last) && last[j] == ':' {
				j++
			}
			j = identEndStr(last, j)
			if j < len(last) && last[j] == '(' {
				j = parenEnd([]byte(last), j)
			}
			i = max(j, i+1)
		default:
			i++
		}
	}
	if class != "" {
		return keyClass, class
	}
	if n := identEndStr(last, 0); n > 0 && isSelectorNameStart(last[0]) {
		return keyTag, strings.ToLower(last[:n])
	}
	return keyAny, ""
}

// rightmostCompound returns the text after the last top-level combinator.
func rightmostCompound(sel string) string {
	depth := 0
	var quote byte
	cut := 0
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
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && (c == ' ' || c == '>' || c == '+' || c == '~' || c == '\t' || c == '\n'):
			cut = i + 1
		}
	}
	return sel[cut:]
}

func isSelectorNameStart(c byte) bool {
	return c == '-' || c == '_' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isSelectorNameChar(c byte) bool {
	return isSelectorNameStart(c) || (c >= '0' && c <= '9')
}

func identEnd(b []byte, i int) int {
	for i < len(b) {
		if b[i] == '\\' && i+1 < len(b) {
			i += 2
			continue
		}
		if !isSelectorNameChar(b[i]) {
			break
		}
		i++
	}
	return i
}

func identEndStr(s string, i int) int {
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if !isSelectorNameChar(s[i]) {
			break
		}
		i++
	}
	return i
}

func attrEnd(b []byte, i int) int {
	var quote byte
	for i < len(b) {
		c := b[i]
		if quote != 0 {
			if c == '\\' {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
		} else if c == '"' || c == '\'' {
			quote = c
		} else if c == ']' {
			return i + 1
		}
		i++
	}
	return len(b)
}

func parenEnd(b []byte, i int) int {
	depth := 0
	var quote byte
	for i < len(b) {
		c := b[i]
		if quote != 0 {
			if c == '\\' {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
		} else {
			switch c {
			case '"', '\'':
				quote = c
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
		i++
	}
	return len(b)
}
