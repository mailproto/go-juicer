package selector

// newArm is an arm of tokens that were rearranged.
func newArm(ts []Token) *Arm {
	a := &Arm{Tokens: ts}
	for _, t := range ts {
		a.comb = a.comb || t.comb
		a.list = a.list || t.list
	}
	return a
}

// combinator reports whether t separates compounds: whitespace or one of > + ~.
func combinator(t Token) bool {
	return t.Kind == Other && hasComb(t.Text)
}

var combBytes = [256]bool{' ': true, '\t': true, '\n': true, '>': true, '+': true, '~': true}

// hasComb reports whether b holds whitespace or a combinator character.
func hasComb(b []byte) bool {
	for _, c := range b {
		if combBytes[c] {
			return true
		}
	}
	return false
}

// foldPrefix reports whether b starts with the lower-case ASCII s, in any case.
func foldPrefix(b []byte, s string) bool {
	if len(b) < len(s) {
		return false
	}
	for i := range len(s) {
		c := b[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != s[i] {
			return false
		}
	}
	return true
}

// rebuilt is a pseudo-class whose arguments were rewritten, written out from
// them rather than as it was.
func rebuilt(t Token) Token {
	t.Text = t.Head()
	t.flags |= closed | rebuiltFlag
	t.comb, t.list = false, t.Name == "is" || t.Name == "where" || t.Name == "matches"
	for _, a := range t.Args {
		t.comb = t.comb || a.comb
		t.list = t.list || a.list
	}
	return t
}

// rejoin reads rearranged tokens again as text where two that now meet would
// read differently, as "a" and "b" meeting read as "ab".
func rejoin(ts []Token) []Token {
	for k := 1; k < len(ts); k++ {
		if mayJoin(ts[k-1], ts[k]) {
			var w writer
			w.rawArm(&Arm{Tokens: ts})
			return Parse(w.buf).Tokens
		}
	}
	return ts
}

// mayJoin reports whether l followed by r reads as other tokens.
func mayJoin(l, r Token) bool {
	if r.is(rebuiltFlag) {
		return false
	}
	c := r.Text[0]
	switch l.Kind {
	case Type, ID, Class:
		return IsNameChar(c) || c == '\\'
	case Pseudo:
		if l.is(call) {
			return !l.is(closed)
		}
		return IsNameChar(c) || c == '\\' || c == '(' || c == ':' && l.head == 1
	case Attr, String:
		return !l.is(closed)
	case Other:
		return l.Text[0] == '\\'
	}
	return false
}
