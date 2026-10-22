package selector

import (
	"bytes"
	"slices"
)

// Valid reports whether browsers accept sel, one selector of a list, rather
// than dropping its rule. A nested rule's selector is relative: it may start
// with a combinator.
// ponytail: catches the errors juice applies regardless (stray punctuation,
// dangling combinators, a type after "*", unquoted non-identifier attribute
// values); a full Selectors L4 grammar if others turn up.
func Valid(sel []byte, relative bool) bool {
	return validArm(Parse(sel), relative)
}

func validArm(a *Arm, relative bool) bool {
	if a.open {
		return false
	}
	// Escapes are not read, so a selector with one passes.
	if slices.ContainsFunc(a.Tokens, func(t Token) bool { return bytes.IndexByte(t.Text, '\\') >= 0 }) {
		return true
	}
	empty := true    // nothing read yet
	pending := false // a combinator waits for its compound
	typeOK := true   // at the start of a compound
	for k := range a.Tokens {
		t := &a.Tokens[k]
		switch t.Kind {
		case Other:
			c := t.Text[0]
			switch {
			case isCSSSpace(c):
				typeOK = true
				continue
			case isComb(c):
				if pending || empty && !relative {
					return false
				}
				pending, typeOK = true, true
				continue
			case c == '|':
				// A namespace prefix: the type or * follows.
				typeOK = true
				continue
			case c != '&':
				return false
			}
		case String:
			return false
		case Type, Universal:
			if !typeOK {
				return false
			}
		case ID, Class:
			if !ident(t.Text[1:]) {
				return false
			}
		case Attr:
			if !validAttr(t.Text) {
				return false
			}
		case Pseudo:
			if !validPseudo(t) {
				return false
			}
		}
		empty, pending, typeOK = false, false, false
	}
	return !empty && !pending
}

// validPseudo checks a pseudo-class's arguments where they are selectors:
// :not() and :has() reject any invalid one, :is() and :where() skip it.
func validPseudo(t *Token) bool {
	if len(t.Name) == 0 || t.is(call) && !t.is(closed) {
		return false
	}
	switch t.Name {
	case "not", "has":
		for _, arg := range t.Args {
			if !validArm(arg, t.Name == "has") {
				return false
			}
		}
	}
	return true
}

// validAttr checks [name], or [name op value] with an identifier or string
// value and an optional i or s flag.
func validAttr(b []byte) bool {
	if len(b) < 3 || b[len(b)-1] != ']' {
		return false
	}
	in := bytes.TrimSpace(b[1 : len(b)-1])
	eq := bytes.IndexByte(in, '=')
	if eq < 0 {
		return validAttrName(in)
	}
	name := in[:eq]
	if n := len(name); n > 0 && bytes.IndexByte([]byte("~|^$*"), name[n-1]) >= 0 {
		name = name[:n-1]
	}
	if !validAttrName(bytes.TrimSpace(name)) {
		return false
	}
	v := bytes.TrimSpace(in[eq+1:])
	var flag []byte
	if len(v) > 0 && (v[0] == '"' || v[0] == '\'') {
		end := bytes.IndexByte(v[1:], v[0])
		if end < 0 {
			return false
		}
		flag = bytes.TrimSpace(v[end+2:])
	} else {
		f := bytes.Fields(v)
		if len(f) == 0 || len(f) > 2 || !ident(f[0]) {
			return false
		}
		if len(f) == 2 {
			flag = f[1]
		}
	}
	return len(flag) == 0 || len(flag) == 1 && bytes.IndexByte([]byte("iIsS"), flag[0]) >= 0
}

func validAttrName(b []byte) bool {
	if _, after, ok := bytes.Cut(b, []byte("|")); ok {
		b = after
	}
	return ident(b)
}

func ident(b []byte) bool {
	switch {
	case len(b) == 0 || isDigit(b[0]):
		return false
	case b[0] == '-' && (len(b) == 1 || isDigit(b[1])):
		return false
	}
	return IdentEnd(b, 0) == len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// prune drops the arguments of :is(), :where() and :matches() that browsers
// reject, as those lists forgive them; a list left empty matches nothing.
func prune(a *Arm) {
	for k := range a.Tokens {
		t := &a.Tokens[k]
		if t.Kind != Pseudo {
			continue
		}
		if t.Name == "is" || t.Name == "where" || t.Name == "matches" {
			t.Args = slices.DeleteFunc(t.Args, func(arg *Arm) bool { return !validArm(arg, false) })
		}
		for _, arg := range t.Args {
			prune(arg)
		}
	}
}
