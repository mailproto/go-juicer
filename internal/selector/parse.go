// Package selector reads selectors as juice does. A selector is parsed once,
// into tokens whose functional pseudo-classes hold their parsed arguments, and
// specificity, pseudo-elements, :is() expansion and the text cascadia matches
// all come from that tree.
package selector

import (
	"bytes"
	"slices"
	"strings"
	"sync"
)

type Kind uint8

const (
	Other     Kind = iota // one byte no other kind starts: whitespace, a combinator, stray punctuation
	Type                  // a type selector
	Universal             // *
	ID                    // #name
	Class                 // .name
	Attr                  // [...]
	Pseudo                // :name or ::name, with an argument list when Call
	String                // a quoted string outside brackets
)

type Token struct {
	Text []byte // as written; only the head of a pseudo-class rebuilt from its arguments
	Name string // a pseudo's name, lower-cased
	Args []*Arm // a pseudo's argument list, split at top-level commas
	head int32  // how much of Text is a pseudo's colons and name
	Kind Kind

	flags flag
	comb  bool // the text holds whitespace or a combinator character
	list  bool // the text holds ":is(", ":where(" or ":matches(", in any case
	open  bool // the token, or one in its arguments, never closes
}

type flag uint8

const (
	double      flag = 1 << iota // a pseudo written with "::"
	call                         // a pseudo with an argument list
	closed                       // an argument list, attribute or string that closes
	rebuiltFlag                  // a pseudo-class written out from its arguments
)

func (t *Token) is(f flag) bool { return t.flags&f != 0 }

func (t *Token) set(f flag, on bool) {
	if on {
		t.flags |= f
	}
}

// Head is a pseudo's colons and name, as written.
func (t *Token) Head() []byte { return t.Text[:t.head] }

// Arm is one selector of a comma-separated list.
type Arm struct {
	Tokens []Token

	comb, list bool
	open       bool // an argument list, attribute or string never closes
	analyzed   bool
	info       info // analysis, cached by analyze
}

// skipped stands in for an argument read in skip mode.
var skipped Arm

// Parse reads one selector of a list.
func Parse(sel []byte) *Arm {
	p := parser{b: sel}
	a, _ := p.arm(false)
	return a
}

// parse is Parse into a tree.
func parse(sel []byte, tr *tree) *Arm {
	p := parser{b: sel, tree: tr, buf: tr.buf[:0]}
	a, _ := p.arm(false)
	tr.buf = p.buf[:0]
	return a
}

// tree holds a selector being compiled: the tokens of the arguments being
// read, and the arms and tokens read. Nothing in it outlives Compile, whose
// results are strings, so it is reused rather than allocated per selector.
type tree struct {
	buf  []Token
	toks []Token
	arms []Arm
	high int // how much of buf has been used
}

var trees = sync.Pool{New: func() any { return new(tree) }}

func (t *tree) reset() {
	clear(t.buf[:t.high])
	clear(t.toks)
	clear(t.arms)
	t.buf, t.toks, t.arms, t.high = t.buf[:0], t.toks[:0], t.arms[:0], 0
}

// newArm is an arm holding ts, from the tree when there is one.
func (t *tree) newArm(ts []Token) *Arm {
	if t == nil {
		return &Arm{Tokens: slices.Clone(ts)}
	}
	if cap(t.toks)-len(t.toks) < len(ts) {
		// Arms already read keep the full block.
		t.toks = make([]Token, 0, max(64, len(ts)))
	}
	if len(t.arms) == cap(t.arms) {
		t.arms = make([]Arm, 0, 16)
	}
	n := len(t.toks)
	t.toks = append(t.toks, ts...)
	t.arms = t.arms[:len(t.arms)+1]
	a := &t.arms[len(t.arms)-1]
	a.Tokens = t.toks[n:len(t.toks):len(t.toks)]
	return a
}

type parser struct {
	b    []byte
	i    int
	skip bool    // read past tokens without keeping arguments
	buf  []Token // tokens of the arguments being read
	tree *tree
}

// arm reads tokens to the end of input or, in an argument list, to the comma
// or parenthesis that ends the argument. Stray parentheses and brackets nest:
// a comma inside them does not end the argument, and a ")" closing one does
// not end the list. It reports whether whitespace or combinator characters
// were trimmed from either end.
func (p *parser) arm(inArg bool) (*Arm, bool) {
	start := len(p.buf)
	parens, depth := 0, 0
	for p.i < len(p.b) {
		c := p.b[p.i]
		if inArg && (c == ')' && parens == 0 || c == ',' && depth == 0) {
			break
		}
		switch c {
		case '(':
			parens++
			depth++
		case ')':
			parens--
			depth--
		case ']':
			depth--
		}
		if t := p.token(); !p.skip {
			p.buf = append(p.buf, t)
			if p.tree != nil {
				p.tree.high = max(p.tree.high, len(p.buf))
			}
		}
	}
	if p.skip {
		return nil, false
	}
	ts := p.buf[start:]
	p.buf = p.buf[:start]
	// An argument is trimmed as the comma split trims: whitespace and
	// semicolons.
	trimmed := false
	for inArg && len(ts) > 0 && trimmable(ts[0]) {
		trimmed = trimmed || ts[0].comb
		ts = ts[1:]
	}
	for inArg && len(ts) > 0 && trimmable(ts[len(ts)-1]) {
		trimmed = trimmed || ts[len(ts)-1].comb
		ts = ts[:len(ts)-1]
	}
	a := p.tree.newArm(ts)
	for _, t := range a.Tokens {
		a.comb = a.comb || t.comb
		a.list = a.list || t.list
		a.open = a.open || t.open
	}
	return a, trimmed
}

// token reads the token at p.i. A byte it does not otherwise recognise is a
// token of its own, Other.
func (p *parser) token() Token {
	start := p.i
	c := p.b[p.i]
	var t Token
	switch {
	case c == '#':
		t.Kind, p.i = ID, IdentEnd(p.b, p.i+1)
	case c == '.':
		t.Kind, p.i = Class, IdentEnd(p.b, p.i+1)
	case c == '[':
		t.Kind, p.i = Attr, AttrEnd(p.b, p.i)
		t.set(closed, p.b[p.i-1] == ']' && p.i-start > 1)
	case c == ':':
		return p.pseudo()
	case c == '*':
		t.Kind, p.i = Universal, p.i+1
	case IsNameStart(c):
		t.Kind, p.i = Type, IdentEnd(p.b, p.i)
	case c == '"' || c == '\'':
		t.Kind = String
		p.i++
		for p.i < len(p.b) && p.b[p.i] != c {
			if p.b[p.i] == '\\' {
				p.i++
			}
			p.i++
		}
		t.set(closed, p.i < len(p.b))
		p.i = min(p.i+1, len(p.b))
	case isCSSSpace(c):
		// A run of whitespace is one token.
		t.Kind = Other
		for p.i < len(p.b) && isCSSSpace(p.b[p.i]) {
			p.i++
		}
	default:
		t.Kind, p.i = Other, p.i+1
	}
	t.Text = p.b[start:p.i]
	t.open = (t.Kind == Attr || t.Kind == String) && !t.is(closed)
	t.comb = hasComb(t.Text)
	t.list = (t.Kind == Attr || t.Kind == String) && hasList(t.Text)
	return t
}

func trimmable(t Token) bool {
	return t.Kind == Other && (isCSSSpace(t.Text[0]) || t.Text[0] == ';')
}

func (p *parser) pseudo() Token {
	start := p.i
	p.i++
	t := Token{Kind: Pseudo}
	if p.i < len(p.b) && p.b[p.i] == ':' {
		t.flags |= double
		p.i++
	}
	name := p.i
	p.i = IdentEnd(p.b, p.i)
	t.head = int32(p.i - start)
	t.Name = lowerName(p.b[name:p.i])
	if p.i < len(p.b) && p.b[p.i] == '(' {
		t.flags |= call
		t.list = t.Name == "is" || t.Name == "where" || t.Name == "matches"
		p.i++
		for {
			a, trimmed := p.arm(true)
			if a == nil {
				a = &skipped
			}
			t.comb = t.comb || trimmed || a.comb
			t.list = t.list || a.list
			t.open = t.open || a.open
			if len(a.Tokens) > 0 {
				t.Args = append(t.Args, a)
			}
			if p.i >= len(p.b) {
				break
			}
			p.i++
			if p.b[p.i-1] == ')' {
				t.flags |= closed
				break
			}
		}
	}
	t.open = t.open || t.is(call) && !t.is(closed)
	t.Text = p.b[start:p.i]
	return t
}

// names holds the pseudo names a selector usually has, so reading one does
// not allocate.
var names = map[string]string{}

func init() {
	for _, n := range []string{
		"hover", "active", "focus", "visited", "link", "before", "after", "where", "is", "matches", "not", "has",
		"empty", "first-line", "first-letter", "first-child", "last-child", "only-child", "nth-child", "nth-last-child",
		"first-of-type", "last-of-type", "only-of-type", "nth-of-type", "nth-last-of-type", "root", "checked",
		"disabled", "enabled", "lang", "target", "focus-within", "focus-visible", "marker", "placeholder", "selection",
	} {
		names[n] = n
	}
}

func lowerName(b []byte) string {
	if n, ok := names[string(b)]; ok {
		return n
	}
	return strings.ToLower(string(b))
}

// hasList reports whether b holds ":is(", ":where(" or ":matches(", in any case.
func hasList(b []byte) bool {
	for i := bytes.IndexByte(b, ':'); i >= 0; {
		if foldPrefix(b[i+1:], "is(") || foldPrefix(b[i+1:], "where(") || foldPrefix(b[i+1:], "matches(") {
			return true
		}
		k := bytes.IndexByte(b[i+1:], ':')
		if k < 0 {
			break
		}
		i += 1 + k
	}
	return false
}

// Split splits a selector list on the commas outside brackets, parentheses
// and strings, trimming each selector and dropping empty ones.
func Split(sel []byte) [][]byte {
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
			if a := trim(sel[start:i]); len(a) > 0 {
				out = append(out, a)
			}
			start = i + 1
		}
	}
	if a := trim(sel[start:]); len(a) > 0 {
		out = append(out, a)
	}
	return out
}

func trim(b []byte) []byte {
	return bytes.TrimFunc(b, func(r rune) bool { return r < 0x80 && (isCSSSpace(byte(r)) || r == ';') })
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func IsNameStart(c byte) bool {
	return c == '-' || c == '_' || c >= 0x80 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func IsNameChar(c byte) bool { return IsNameStart(c) || (c >= '0' && c <= '9') }

// IdentEnd is where the name starting at b[i] ends, past any escapes.
func IdentEnd(b []byte, i int) int {
	for i < len(b) {
		if b[i] == '\\' && i+1 < len(b) {
			i += 2
			continue
		}
		if !IsNameChar(b[i]) {
			break
		}
		i++
	}
	return i
}

// AttrEnd is where the attribute selector opening at b[i] ends, past its "]",
// or the end of b when it never closes.
func AttrEnd(b []byte, i int) int {
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
