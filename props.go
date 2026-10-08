package juicer

import (
	"bytes"
	"cmp"
	"slices"
	"sort"
	"strings"
)

// The resolved declarations for one element.
//
// This is an insertion-ordered arena rather than a map because juice's
// accumulation is order-sensitive: when a property is won by a *different*
// selector it deletes the key and reinserts it, moving it to the end of the
// output, and when it is won by the *same* selector both declarations survive
// as a chain. A Go map would also randomize iteration and make output
// nondeterministic.
type property struct {
	prop  string
	value []byte
	key   uint64 // packed specificity, including the !important bonus
	ord   uint32 // declaration order; breaks specificity ties
	rule  int32  // source rule index; -1 is the synthetic style="" rule
	next  int32  // same-selector duplicate, kept for progressive enhancement
	dead  bool   // superseded, or reachable only through a chain
}

type propMap struct {
	slots []property
}

// packKey lays the specificity vector out in one word so comparison is a
// single compare. Fields saturate rather than overflow into each other.
func packKey(prio, ids, classes, types int) uint64 {
	return uint64(sat(prio))<<48 | uint64(sat(ids))<<32 |
		uint64(sat(classes))<<16 | uint64(sat(types))
}

func sat(v int) uint16 {
	if v < 0 {
		return 0
	}
	if v > 0xFFFF {
		return 0xFFFF
	}
	return uint16(v)
}

// find returns the live slot for name, scanning backward because recent
// declarations dominate.
func (m *propMap) find(name string) int32 {
	for i := len(m.slots) - 1; i >= 0; i-- {
		if !m.slots[i].dead && m.slots[i].prop == name {
			return int32(i)
		}
	}
	return -1
}

// add applies one declaration, following juice's addProps.
func (m *propMap) add(p property) {
	p.next = -1
	if cur := m.find(p.prop); cur >= 0 {
		e := &m.slots[cur]
		// The incumbent keeps the property only if it strictly outranks the
		// newcomer. Two arms of one selector list tie exactly, since they
		// share declarations, and juice lets the later arm win.
		if p.key < e.key || (p.key == e.key && p.ord < e.ord) {
			return
		}
		e.dead = true
		if e.rule == p.rule {
			// The same rule declared it twice: emit both, in source order, so
			// `background:#fff; background:linear-gradient(...)` survives.
			p.next = cur
		}
	}
	m.slots = append(m.slots, p)
}

// order returns the live properties in juice's styleProps key order: where a
// property was first set, except that an override from a different rule moves
// it to the end. A same-rule chain therefore sits where its oldest slot does.
func (m *propMap) order() []int32 {
	pos := func(i int32) int32 {
		for m.slots[i].next >= 0 {
			i = m.slots[i].next
		}
		return i
	}
	var heads []int32
	for i := range m.slots {
		if !m.slots[i].dead {
			heads = append(heads, int32(i))
		}
	}
	slices.SortFunc(heads, func(a, b int32) int { return cmp.Compare(pos(a), pos(b)) })
	return heads
}

// live returns the emittable slots, newest-wins order resolved, chains
// flattened.
func (m *propMap) live() []int32 {
	var out []int32
	for i := range m.slots {
		if m.slots[i].dead {
			continue
		}
		for j := int32(i); j >= 0; j = m.slots[j].next {
			out = append(out, j)
		}
	}
	return out
}

// styleAttr renders the style attribute value, or nil when nothing survives
// filtering. juice writes no attribute at all in that case.
func (m *propMap) styleAttr(o *options) []byte {
	idx := m.live()
	if len(idx) == 0 {
		return nil
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := &m.slots[idx[a]], &m.slots[idx[b]]
		if x.key != y.key {
			return x.key < y.key
		}
		return x.ord < y.ord
	})

	var buf bytes.Buffer
	for _, i := range idx {
		p := &m.slots[i]
		// `content` belongs to a pseudo-element, never to a style attribute.
		if p.prop == "content" {
			continue
		}
		if o.resolveCSSVariables && strings.HasPrefix(p.prop, "--") {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(p.prop)
		buf.WriteString(": ")
		// The value lands inside a double-quoted attribute, so juice swaps
		// double quotes for single ones rather than escaping them.
		writeSingleQuoted(&buf, p.value)
		buf.WriteByte(';')
	}
	if buf.Len() == 0 {
		return nil
	}
	return buf.Bytes()
}

func writeSingleQuoted(buf *bytes.Buffer, v []byte) {
	for {
		i := bytes.IndexByte(v, '"')
		if i < 0 {
			buf.Write(v)
			return
		}
		buf.Write(v[:i])
		buf.WriteByte('\'')
		v = v[i+1:]
	}
}

// seedInline parses an existing style attribute as a synthetic rule. juice
// gives it specificity [1,0,0,1] -- the sentinel selector "<style>" is read as
// a tag name -- which beats any real selector but loses to an !important one.
func (m *propMap) seedInline(style []byte, keepImportant bool) {
	var ord uint32
	for _, d := range splitDeclarations(style) {
		prio := 1
		if d.important {
			prio += 2
			if keepImportant {
				d.value = append(d.value[:len(d.value):len(d.value)], " !important"...)
			}
		}
		m.add(property{
			prop:  d.prop,
			value: d.value,
			key:   packKey(prio, 0, 0, 1),
			ord:   ord,
			rule:  -1,
			next:  -1,
		})
		ord++
	}
}

// splitDeclarations parses a style attribute body. It is deliberately lenient:
// this text came from a document, not a stylesheet.
// endsEntity reports whether b ends in the body of an HTML character
// reference ("&quot", "&#34"), whose ";" is not a declaration separator.
// The attribute is kept undecoded, so a browser decodes it only later.
func endsEntity(b []byte) bool {
	i := len(b)
	for i > 0 && (b[i-1] == '#' || 'a' <= b[i-1]|0x20 && b[i-1]|0x20 <= 'z' || '0' <= b[i-1] && b[i-1] <= '9') {
		i--
	}
	return i > 0 && i < len(b) && b[i-1] == '&'
}

func splitDeclarations(s []byte) []decl {
	var out []decl
	depth := 0
	var quote byte
	start := 0
	flush := func(seg []byte) {
		i := bytes.IndexByte(seg, ':')
		if i < 0 {
			return
		}
		// juice keys styleProps by the name as written, so MARGIN and margin
		// are distinct properties and the original case is emitted.
		name := string(trimCSS(seg[:i]))
		if name == "" {
			return
		}
		v := trimCSS(seg[i+1:])
		imp := false
		if j := importantSuffix(v); j >= 0 {
			v, imp = trimCSS(v[:j]), true
		}
		if len(v) == 0 {
			return
		}
		out = append(out, decl{prop: name, value: v, important: imp})
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ';' && depth == 0 && !endsEntity(s[:i]):
			flush(s[start:i])
			start = i + 1
		}
	}
	flush(s[start:])
	return out
}
