package juicer

import (
	"bytes"
	"strings"
	"unicode"
)

// A port of mensch 0.3.4, the CSS parser juice 9 reads stylesheets with and
// re-emits preserved rules through. Its recovery from malformed CSS, comment
// handling and output formatting are all part of juice 9's behaviour, so the
// Juice9 profile follows it state for state rather than approximating it.

type menschToken struct {
	typ, name, value, text, prefix string
}

var menschAtRules = []struct{ name, typ, prefix, state string }{
	{"media", "", "", ""},
	{"keyframes", "", "", ""},
	{"-webkit-keyframes", "keyframes", "-webkit-", ""},
	{"-moz-keyframes", "keyframes", "-moz-", ""},
	{"-ms-keyframes", "keyframes", "-ms-", ""},
	{"-o-keyframes", "keyframes", "-o-", ""},
	{"font-face", "", "", ""},
	{"import", "", "", "before-at-value"},
	{"charset", "", "", "before-at-value"},
	{"supports", "", "", ""},
	{"viewport", "", "", ""},
	{"namespace", "", "", "before-at-value"},
	{"document", "", "", ""},
	{"-moz-document", "document", "-moz-", ""},
	{"page", "", "", ""},
}

// menschLex is mensch's lexer. A state of "" is JavaScript's undefined, which
// mensch reaches by popping its last state on a stray "}". Setting a state on
// the emptied stack writes its "-1" property, which neg stands in for.
func menschLex(css string) []menschToken {
	var (
		buf    []byte
		cursor = -1
		depth  int
		state  = "before-selector"
		stack  = []string{state}
		neg    string
		tok    menschToken
		toks   []menschToken
	)
	at := func(i int) byte {
		if i < 0 || i >= len(css) {
			return 0
		}
		return css[i]
	}
	getState := func(index int) string {
		if index == 0 {
			return state
		}
		switch i := len(stack) - 1 - index; {
		case i >= 0:
			return stack[i]
		case i == -1:
			return neg
		}
		return ""
	}
	popState := func() {
		if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
		state = neg
		if len(stack) > 0 {
			state = stack[len(stack)-1]
		}
	}
	pushState := func(s string) {
		state = s
		stack = append(stack, s)
	}
	replaceState := func(s string) {
		state = s
		if len(stack) > 0 {
			stack[len(stack)-1] = s
		} else {
			neg = s
		}
	}
	addToken := func() {
		toks = append(toks, tok)
		buf = buf[:0]
		tok = menschToken{}
	}
	initializeToken := func(typ string) { tok = menschToken{typ: typ} }
	trimmed := func() string { return jsTrim(string(buf)) }

	for cursor++; cursor < len(css); cursor++ {
		ch := css[cursor]
		switch ch {
		case ' ':
			switch state {
			case "selector", "value", "value-paren", "at-group", "at-value", "comment", "double-string", "single-string":
				buf = append(buf, ch)
			}
		case '\n', '\t', '\r', '\f':
			switch state {
			case "value", "value-paren", "at-group", "comment", "single-string", "double-string", "selector":
				buf = append(buf, ch)
			case "at-value":
				if ch == '\n' {
					tok.value = trimmed()
					addToken()
					popState()
				}
			}
		case ':':
			switch state {
			case "name":
				tok.name = trimmed()
				buf = buf[:0]
				replaceState("before-value")
			case "before-selector":
				buf = append(buf, ch)
				initializeToken("selector")
				pushState("selector")
			case "before-value":
				replaceState("value")
				buf = append(buf, ch)
			default:
				buf = append(buf, ch)
			}
		case ';':
			switch state {
			case "name", "before-value", "value":
				if v := trimmed(); v != "" {
					tok.value = v
					addToken()
				}
				replaceState("before-name")
			case "value-paren":
				buf = append(buf, ch)
			case "at-value":
				tok.value = trimmed()
				addToken()
				popState()
			case "before-name":
			default:
				buf = append(buf, ch)
			}
		case '{':
			switch state {
			case "selector":
				if at(cursor-1) == '\\' {
					buf = append(buf, ch)
					break
				}
				tok.text = trimmed()
				addToken()
				replaceState("before-name")
				depth++
			case "at-group":
				tok.name = trimmed()
				switch tok.typ {
				case "font-face", "viewport", "page":
					pushState("before-name")
				default:
					pushState("before-selector")
				}
				addToken()
				depth++
			case "name", "at-rule":
				tok.name = trimmed()
				addToken()
				pushState("before-name")
				depth++
			case "comment", "double-string", "single-string":
				buf = append(buf, ch)
			case "before-value":
				replaceState("value")
				buf = append(buf, ch)
			}
		case '}':
			switch state {
			case "before-name", "name", "before-value", "value":
				if len(buf) > 0 {
					tok.value = trimmed()
				}
				if tok.name != "" && tok.value != "" {
					addToken()
				}
				initializeToken("end")
				addToken()
				popState()
				if state == "at-group" {
					initializeToken("at-group-end")
					addToken()
					popState()
				}
				if depth > 0 {
					depth--
				}
			case "at-group", "before-selector", "selector":
				if at(cursor-1) == '\\' {
					buf = append(buf, ch)
					break
				}
				if depth > 0 && getState(1) == "at-group" {
					initializeToken("at-group-end")
					addToken()
				}
				if depth > 1 {
					popState()
				}
				if depth > 0 {
					depth--
				}
			case "double-string", "single-string", "comment":
				buf = append(buf, ch)
			}
		case '"', '\'':
			switch state {
			case "double-string":
				if ch == '"' && at(cursor-1) != '\\' {
					popState()
				}
			case "single-string":
				if ch == '\'' && at(cursor-1) != '\\' {
					popState()
				}
			case "before-at-value":
				replaceState("at-value")
				pushState(stringState(ch))
			case "before-value":
				replaceState("value")
				pushState(stringState(ch))
			case "comment":
			default:
				if at(cursor-1) != '\\' {
					pushState(stringState(ch))
				}
			}
			buf = append(buf, ch)
		case '/':
			switch state {
			case "comment", "double-string", "single-string":
				buf = append(buf, ch)
			case "before-value", "selector", "name", "value":
				if at(cursor+1) == '*' {
					// Comments inside declarations and selectors vanish.
					if pos := strings.Index(css[cursor:], "*/"); pos > 0 {
						cursor += pos + 1
					}
				} else {
					if state == "before-value" {
						replaceState("value")
					}
					buf = append(buf, ch)
				}
			default:
				if at(cursor+1) == '*' {
					initializeToken("comment")
					pushState("comment")
					cursor++
				} else {
					buf = append(buf, ch)
				}
			}
		case '*':
			switch state {
			case "comment":
				if at(cursor+1) == '/' {
					tok.text = string(buf)
					cursor++
					addToken()
					popState()
				} else {
					buf = append(buf, ch)
				}
			case "before-selector":
				buf = append(buf, ch)
				initializeToken("selector")
				pushState("selector")
			case "before-value":
				replaceState("value")
				buf = append(buf, ch)
			default:
				buf = append(buf, ch)
			}
		case '@':
			switch state {
			case "comment", "double-string", "single-string":
				buf = append(buf, ch)
			case "before-value":
				replaceState("value")
				buf = append(buf, ch)
			default:
				tokenized := false
				for _, r := range menschAtRules {
					if !strings.HasPrefix(css[cursor+1:], r.name) {
						continue
					}
					tokenized = true
					initializeToken(r.name)
					st := r.state
					if st == "" {
						st = "at-group"
					}
					pushState(st)
					cursor += len(r.name)
					if r.prefix != "" {
						tok.prefix = r.prefix
					}
					if r.typ != "" {
						tok.typ = r.typ
					}
					break
				}
				if !tokenized {
					buf = append(buf, ch)
				}
			}
		case '(':
			switch state {
			case "value":
				pushState("value-paren")
			case "before-value":
				replaceState("value")
			}
			buf = append(buf, ch)
		case ')':
			switch state {
			case "value-paren":
				popState()
			case "before-value":
				replaceState("value")
			}
			buf = append(buf, ch)
		default:
			switch state {
			case "before-selector":
				initializeToken("selector")
				pushState("selector")
			case "before-name":
				initializeToken("property")
				replaceState("name")
			case "before-value":
				replaceState("value")
			case "before-at-value":
				replaceState("at-value")
			}
			buf = append(buf, ch)
		}
	}
	return toks
}

func stringState(quote byte) string {
	if quote == '"' {
		return "double-string"
	}
	return "single-string"
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r == 0xfeff || r != 0x85 && unicode.IsSpace(r)
	})
}

// menschNode is a node of mensch's AST. rules is non-nil for an at-rule that
// holds rules; decls for a rule or an at-rule that holds declarations.
type menschNode struct {
	typ, name, value, text, prefix string
	selectors                      []string
	decls, rules                   []menschNode
}

// menschParse is mensch's parser, with comments kept as juice asks.
func menschParse(toks []menschToken) []menschNode {
	p := menschParser{toks: toks}
	var rules []menschNode
	for len(p.toks) > 0 {
		if n, ok := p.token(p.next()); ok {
			rules = append(rules, n)
		}
	}
	return rules
}

type menschParser struct {
	toks  []menschToken
	depth int // shared by every open at-rule, as in mensch
}

func (p *menschParser) next() menschToken {
	t := p.toks[0]
	p.toks = p.toks[1:]
	return t
}

func (p *menschParser) token(t menschToken) (menschNode, bool) {
	n := menschNode{typ: t.typ, name: t.name, value: t.value}
	switch t.typ {
	case "property", "charset", "import", "namespace":
	case "selector":
		n.typ = "rule"
		for _, s := range strings.Split(t.text, ",") {
			n.selectors = append(n.selectors, jsTrim(s))
		}
		n.decls = p.declarations()
	case "comment":
		n.text = t.text
	case "media", "keyframes", "supports", "document":
		p.depth++
		n.prefix = t.prefix
		n.rules = p.while(func(menschToken) bool { return p.depth != 0 })
		if n.rules == nil {
			n.rules = []menschNode{}
		}
	case "font-face", "viewport", "page":
		p.depth++
		n.decls = p.declarations()
	case "at-group-end":
		p.depth--
		return n, false
	default:
		return n, false
	}
	return n, true
}

// while is mensch's parseTokensWhile, which drops the token that ends it if
// that token is an "end".
func (p *menschParser) while(cond func(menschToken) bool) []menschNode {
	var nodes []menschNode
	for len(p.toks) > 0 {
		t := p.next()
		if !cond(t) {
			if t.typ != "end" {
				p.toks = append([]menschToken{t}, p.toks...)
			}
			return nodes
		}
		if n, ok := p.token(t); ok {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (p *menschParser) declarations() []menschNode {
	return p.while(func(t menschToken) bool { return t.typ == "property" || t.typ == "comment" })
}

// menschStringify is mensch.stringify for one node, as juice 9 calls it:
// comments dropped, two-space indentation, trimmed.
func menschStringify(n menschNode) []byte {
	var b bytes.Buffer
	menschNodeText(&b, n, 1)
	return []byte(jsTrim(b.String()))
}

func menschIndent(b *bytes.Buffer, level int) {
	for range level - 1 {
		b.WriteString("  ")
	}
}

func menschNodeText(b *bytes.Buffer, n menschNode, level int) {
	switch n.typ {
	case "rule":
		menschRule(b, n, level)
	case "import", "charset", "namespace":
		b.WriteString("@" + n.typ + " " + n.value + ";\n")
	case "media", "keyframes", "font-face", "supports", "viewport", "document", "page":
		b.WriteString("@" + n.prefix + n.typ)
		if n.name != "" {
			b.WriteString(" " + n.name)
		}
		b.WriteByte(' ')
		menschBlock(b, n, level, n.typ != "page")
		b.WriteByte('\n')
	}
}

func menschRule(b *bytes.Buffer, n menschNode, level int) {
	menschIndent(b, level)
	if n.selectors != nil {
		b.WriteString(strings.Join(n.selectors, ",\n"))
	} else {
		b.WriteString("@" + n.typ)
		if n.name != "" {
			b.WriteString(" " + n.name)
		}
	}
	b.WriteByte(' ')
	menschBlock(b, n, level, false)
	b.WriteByte('\n')
}

func menschBlock(b *bytes.Buffer, n menschNode, level int, chomp bool) {
	var parts [][]byte
	if n.rules != nil {
		for _, c := range n.rules {
			if c.typ == "comment" {
				continue
			}
			var cb bytes.Buffer
			menschRule(&cb, c, level+1)
			parts = append(parts, cb.Bytes())
		}
	} else {
		for _, c := range n.decls {
			if c.typ != "property" {
				continue
			}
			var cb bytes.Buffer
			menschIndent(&cb, level+1)
			if c.name != "" {
				cb.WriteString(c.name + ": ")
			}
			cb.WriteString(c.value + ";")
			parts = append(parts, cb.Bytes())
		}
	}
	b.WriteByte('{')
	if len(parts) > 0 {
		b.WriteByte('\n')
		b.Write(bytes.Join(parts, []byte("\n")))
		if !chomp {
			b.WriteByte('\n')
		}
	}
	menschIndent(b, level)
	b.WriteByte('}')
}
