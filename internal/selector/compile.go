package selector

import (
	"bytes"
	"slices"
)

type PseudoElement uint8

const (
	None PseudoElement = iota
	Before
	After
)

// juice skips a whole selector if any compound in it carries one of these,
// because they have no inline equivalent.
var ignoredPseudos = []string{"hover", "active", "focus", "visited", "link"}

// IgnoredPseudo is juice's matchesPseudo: a plain substring test for ":hover"
// and friends, so ":hovered" and "::link" count too.
func IgnoredPseudo[T string | []byte](sel T) bool {
	for i := 0; i < len(sel); i++ {
		if sel[i] != ':' {
			continue
		}
		rest := sel[i+1:]
		for _, p := range ignoredPseudos {
			if len(rest) >= len(p) && string(rest[:len(p)]) == p {
				return true
			}
		}
	}
	return false
}

// info is what juice reads from a selector, apart from its text.
type info struct {
	spec    [3]int
	pseudo  PseudoElement
	ignored bool // juice preserves the selector rather than inlining it
	empty   bool // nothing is left of the text once pseudo-elements are stripped
}

func isList(name string) bool {
	return name == "is" || name == "where" || name == "matches" || name == "not"
}

// rewrite reports whether a pseudo-class's text is rebuilt from its
// arguments: those of a selector list, and any holding a pseudo-element,
// which is stripped.
func rewrite(t *Token, inner PseudoElement) bool {
	return t.is(call) && (inner != None || isList(t.Name))
}

// analyze reads specificity, the pseudo-element and whether juice skips the
// selector, caching them on each argument so a nested one is read once.
// juice scores pseudo-classes like type selectors; with browser set they
// score like classes, as CSS scores them.
func analyze(a *Arm, browser bool) *info {
	if a.analyzed {
		return &a.info
	}
	a.analyzed = true
	in := &a.info
	in.empty = true
	pc := 2
	if browser {
		pc = 1
	}
	for k := range a.Tokens {
		t := &a.Tokens[k]
		visible := true
		switch t.Kind {
		case ID:
			in.spec[0]++
		case Class, Attr:
			in.spec[1]++
		case Type:
			in.spec[2]++
			visible = len(bytes.TrimSpace(t.Text)) > 0
		case Universal:
			visible = !nextIsType(a.Tokens, k)
		case Other:
			visible = len(bytes.TrimSpace(t.Text)) > 0
		case Pseudo:
			if slices.Contains(ignoredPseudos, t.Name) {
				in.ignored = true
				return in
			}
			// Selectors L4: :is(), :not() and :has() take the highest
			// specificity among their arguments.
			var argSpec [3]int
			inner := None
			for _, arg := range t.Args {
				ai := analyze(arg, browser)
				if ai.ignored || ai.empty && ai.pseudo != None {
					// An argument left empty by stripping its pseudo-element
					// is one css-select rejects.
					in.ignored = true
					return in
				}
				if s := ai.spec; s[0] > argSpec[0] || s[0] == argSpec[0] && (s[1] > argSpec[1] || s[1] == argSpec[1] && s[2] > argSpec[2]) {
					argSpec = s
				}
				if inner == None {
					inner = ai.pseudo
				}
			}
			if rewrite(t, inner) && in.pseudo == None {
				in.pseudo = inner
			}
			switch {
			case t.Name == "before" || t.Name == "after":
				// Matched against the base selector; juice takes the first
				// in "a:after:before".
				if in.pseudo == None {
					in.pseudo = Before
					if t.Name == "after" {
						in.pseudo = After
					}
				}
				in.spec[2]++
				visible = false
			case t.Name == "where":
				// Selectors L4: :where() contributes nothing.
				visible = false
			case t.Name == "is" || t.Name == "matches" || t.Name == "not" || t.Name == "has":
				in.spec[0] += argSpec[0]
				in.spec[1] += argSpec[1]
				in.spec[2] += argSpec[2]
			case t.Name == "empty" && !t.is(double):
				in.spec[pc]++
			case t.is(double) || t.Name == "first-line" || t.Name == "first-letter":
				// Pseudo-elements score as types either way.
				in.spec[2]++
			default:
				in.spec[pc]++
			}
		}
		if visible {
			in.empty = false
		}
	}
	return in
}

func nextIsType(ts []Token, k int) bool { return k+1 < len(ts) && ts[k+1].Kind == Type }

func isComb(c byte) bool { return c == '+' || c == '>' || c == '~' }

// writer builds the text cascadia matches: pseudo-elements stripped,
// :where() dropped, bare attribute values quoted, and :has(> x) and :empty
// mapped onto what cascadia supports.
type writer struct {
	buf  []byte
	root bool // :scope is written as :root, which it is in a stylesheet
}

func (w *writer) arm(a *Arm) {
	start := len(w.buf)
	for k := range a.Tokens {
		t := &a.Tokens[k]
		switch t.Kind {
		case Attr:
			w.buf = appendQuotedAttr(w.buf, t.Text)
		case Universal:
			// "*a", which flattened nesting can produce, is just "a".
			if !nextIsType(a.Tokens, k) {
				w.buf = append(w.buf, '*')
			}
		case Pseudo:
			w.pseudo(t)
		default:
			if t.Kind == Other && len(w.buf) == start && len(bytes.TrimSpace(t.Text)) == 0 {
				continue // leading whitespace, trimmed below anyway
			}
			w.buf = append(w.buf, t.Text...)
		}
	}
	region := w.buf[start:]
	if trimmed := bytes.TrimSpace(region); len(trimmed) != len(region) {
		n := copy(region, trimmed)
		w.buf = w.buf[:start+n]
	}
	// Stripping a pseudo-element can leave "a + "; css-select reads a
	// trailing combinator as if followed by *.
	if len(w.buf) > start && isComb(w.buf[len(w.buf)-1]) {
		w.buf = append(w.buf, '*')
	}
}

func (w *writer) pseudo(t *Token) {
	switch {
	case t.Name == "before" || t.Name == "after" || t.Name == "where":
	case t.Name == "is" || t.Name == "matches" || t.Name == "not" || t.Name == "has":
		inner := None
		for _, arg := range t.Args {
			if inner = arg.info.pseudo; inner != None {
				break
			}
		}
		if !rewrite(t, inner) {
			w.raw(t, t.Name == "has")
			return
		}
		start := len(w.buf)
		w.buf = append(append(w.buf, t.Head()...), '(')
		for i, arg := range t.Args {
			if i > 0 {
				w.buf = append(w.buf, ',')
			}
			p := len(w.buf)
			w.arm(arg)
			if t.Name != "has" && len(w.buf) > p && isComb(w.buf[p]) {
				// css-select matches nothing for an argument that starts
				// with a combinator.
				w.buf = append(w.buf[:p], ":not(*)"...)
			}
		}
		w.buf = append(w.buf, ')')
		if t.Name == "has" {
			w.buf = append(w.buf[:start], hasChild(w.buf[start:])...)
		}
	case t.Name == "scope" && w.root && !t.is(call):
		w.buf = append(w.buf, ":root"...)
	case t.Name == "empty" && !t.is(double):
		// css-select counts whitespace text as content; cascadia does not.
		w.buf = append(w.buf, `:empty:not(:matchesOwn([\s\S]))`...)
	default:
		w.raw(t, false)
	}
}

// raw writes a pseudo-class as written, or as expansion rebuilt it.
func (w *writer) raw(t *Token, has bool) {
	if !t.is(rebuiltFlag) {
		if has {
			w.buf = append(w.buf, hasChild(t.Text)...)
		} else {
			w.buf = append(w.buf, t.Text...)
		}
		return
	}
	start := len(w.buf)
	w.buf = append(append(w.buf, t.Head()...), '(')
	for i, arg := range t.Args {
		if i > 0 {
			w.buf = append(w.buf, ',')
		}
		w.rawArm(arg)
	}
	w.buf = append(w.buf, ')')
	if has {
		w.buf = append(w.buf[:start], hasChild(w.buf[start:])...)
	}
}

func (w *writer) rawArm(a *Arm) {
	for k := range a.Tokens {
		if t := &a.Tokens[k]; !t.is(rebuiltFlag) {
			w.buf = append(w.buf, t.Text...)
		} else {
			w.raw(t, false)
		}
	}
}

// hasChild maps :has(> x) onto cascadia's :haschild(x). Sibling forms stay
// unsupported and make cascadia.Parse fail, which drops the rule.
func hasChild(call []byte) []byte {
	open := bytes.IndexByte(call, '(')
	if open < 0 || len(call) < 2 || call[len(call)-1] != ')' || open+1 > len(call)-1 {
		return call
	}
	inner := bytes.TrimSpace(call[open+1 : len(call)-1])
	if len(inner) > 0 && inner[0] == '>' {
		return append([]byte(":haschild("), append(bytes.TrimSpace(inner[1:]), ')')...)
	}
	return call
}

// appendQuotedAttr quotes a bare attribute value cascadia would reject.
// Browsers and postcss accept [width=600]; cascadia requires an identifier.
func appendQuotedAttr(dst, seg []byte) []byte {
	if len(seg) < 2 || seg[len(seg)-1] != ']' {
		return append(dst, seg...)
	}
	eq := bytes.IndexByte(seg, '=')
	if eq < 0 || eq+1 > len(seg)-1 {
		return append(dst, seg...)
	}
	trimmed := bytes.TrimSpace(seg[eq+1 : len(seg)-1])
	// A trailing case-insensitivity flag is not part of the value.
	var flag []byte
	if n := len(trimmed); n >= 2 && (trimmed[n-1] == 'i' || trimmed[n-1] == 'I') && isCSSSpace(trimmed[n-2]) {
		flag = trimmed[n-2:]
		trimmed = bytes.TrimSpace(trimmed[:n-2])
	}
	if len(trimmed) == 0 || trimmed[0] == '"' || trimmed[0] == '\'' || isIdent(trimmed) {
		return append(dst, seg...)
	}
	dst = append(dst, seg[:eq+1]...)
	dst = append(append(append(dst, '"'), trimmed...), '"')
	return append(append(dst, flag...), ']')
}

func isIdent(b []byte) bool {
	if len(b) == 0 || b[0] >= '0' && b[0] <= '9' || b[0] == '-' && len(b) > 1 && b[1] >= '0' && b[1] <= '9' {
		return false
	}
	for _, c := range b {
		if !(c == '-' || c == '_' || c >= 0x80 || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
