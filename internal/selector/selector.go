package selector

import (
	"bytes"
	"strings"
	"sync"
)

// Mode is how a selector is read.
type Mode struct {
	JuiceSpecificity bool // pseudo-classes score as type selectors, as juice 12 scores them
	Juice9           bool // as juice 9 reads selectors, with cheerio and slick
	JuiceScope       bool // :scope matches nothing, as in juice, rather than the root
	Lenient          bool // selectors browsers reject still match, as in juice
}

// Compiled is one selector of a list, as juice reads it.
type Compiled struct {
	Spec    [3]int
	Pseudo  PseudoElement
	Ignored bool // juice preserves the selector rather than inlining it
	Invalid bool // browsers reject the selector, and with it the rule

	// Variants are what cascadia matches, one per :is() alternative.
	// Specificity comes from the selector as written, which is exactly how
	// Selectors L4 scores :is(): by its most specific argument, not the
	// alternative that matched.
	Variants []Variant
}

// Variant is one selector for cascadia, and the index bucket it goes in.
type Variant struct {
	Text    string
	Key     KeyKind
	KeyName string
}

// KeyKind is the index bucket a selector goes in.
type KeyKind uint8

const (
	KeyAny KeyKind = iota
	KeyID
	KeyClass
	KeyTag
)

// texts holds the buffers selector text is written into before it is copied
// out as a string.
var texts = sync.Pool{New: func() any { return new([]byte) }}

// Compile reads one selector of a list. A selector neither engine can match
// compiles to no variants.
func Compile(sel []byte, m Mode) Compiled {
	tr := trees.Get().(*tree)
	defer func() { tr.reset(); trees.Put(tr) }()
	a := parse(sel, tr)
	var c Compiled
	if a.open {
		// css-select rejects a selector left open, and juice skips the rule.
		return c
	}
	if m.Juice9 {
		if !balanced(sel) {
			// Half of a list mensch split inside parentheses: cheerio
			// throws on it and juice 9 skips the rule.
			return c
		}
		var ok bool
		if a, ok = juice9Arguments(a); !ok || !juice9PseudoLast(a) {
			return c
		}
	}
	if !m.Lenient {
		if !validArm(a, false) {
			c.Invalid = true
			return c
		}
		if a.list {
			prune(a)
		}
	}
	in := analyze(a, !m.JuiceSpecificity)
	if in.ignored {
		c.Ignored = true
		return c
	}
	c.Spec, c.Pseudo = in.spec, in.pseudo
	if m.Juice9 {
		c.Spec = juice9Specificity(Parse(sel))
	}
	buf := texts.Get().(*[]byte)
	w := writer{buf: (*buf)[:0], root: !m.JuiceScope}
	defer func() { *buf = w.buf[:0]; texts.Put(buf) }()
	if !a.list {
		c.add(&w, a, m)
		return c
	}
	for _, v := range expand(a.Tokens) {
		va := &Arm{Tokens: v}
		if analyze(va, false).ignored {
			continue
		}
		c.add(&w, va, m)
	}
	return c
}

func (c *Compiled) add(w *writer, va *Arm, m Mode) {
	w.buf = w.buf[:0]
	w.arm(va)
	text, b := string(w.buf), w.buf
	if m.Juice9 {
		if t := juice9Tail(va, text); t != text {
			text, b = t, []byte(t)
		}
	}
	k, s, e := key(b)
	c.Variants = append(c.Variants, Variant{Text: text, Key: k, KeyName: text[s:e]})
	if k == KeyTag {
		c.Variants[len(c.Variants)-1].KeyName = strings.ToLower(text[s:e])
	}
}

// key picks the index bucket for a selector's text, and where the name it
// is keyed by lies: the id, else a class, else the tag of its rightmost
// compound.
//
// Only simple selectors written directly in that compound count. A class
// inside a functional pseudo-class argument does not: p:not(.x) matches any p,
// so bucketing it under class "x" would silently lose every other match. An
// escaped name is held unescaped by the element, so cascadia decides.
func key(b []byte) (k KeyKind, s, e int) {
	p := parser{b: b, skip: true}
	var class [2]int
	first, decided := true, false
	for p.i < len(b) {
		start := p.i
		t := p.token()
		escaped := bytes.IndexByte(t.Text, '\\') >= 0
		switch {
		case combinator(t):
			k, class, first, decided = KeyAny, [2]int{}, true, false
			continue
		case decided:
		case escaped && (t.Kind == ID || t.Kind == Class || first && t.Kind == Type):
			k, decided = KeyAny, true
		case t.Kind == ID && len(t.Text) > 1:
			k, s, e, decided = KeyID, start+1, p.i, true
		case t.Kind == Class && len(t.Text) > 1 && class[1] == 0:
			class = [2]int{start + 1, p.i}
		case first && t.Kind == Type:
			k, s, e = KeyTag, start, p.i
		}
		first = false
	}
	switch {
	case decided && k == KeyAny:
		return KeyAny, 0, 0
	case decided:
		return k, s, e
	case class[1] != 0:
		return KeyClass, class[0], class[1]
	case k == KeyTag:
		return k, s, e
	}
	return KeyAny, 0, 0
}
