// Package mensch is a port of mensch 0.3.4, the CSS parser juice 9 reads
// stylesheets with and re-emits preserved rules through. Its recovery from
// malformed CSS, comment handling and output formatting are all part of juice
// 9's behaviour, so the port follows it state for state rather than
// approximating it.
package mensch

import (
	"bytes"
	"strings"
	"unicode"
)

// Token is a token from mensch's lexer.
type Token struct {
	Type, Name, Value, Text, Prefix string
}

var atRules = []struct{ name, typ, prefix, state string }{
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

// Lex is mensch's lexer. A state of "" is JavaScript's undefined, which
// mensch reaches by popping its last state on a stray "}". Setting a state on
// the emptied stack writes its "-1" property, which neg stands in for.
func Lex(css string) []Token {
	var (
		buf    []byte
		cursor = -1
		depth  int
		state  = "before-selector"
		stack  = []string{state}
		neg    string
		tok    Token
		toks   []Token
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
		tok = Token{}
	}
	initializeToken := func(typ string) { tok = Token{Type: typ} }
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
					tok.Value = trimmed()
					addToken()
					popState()
				}
			}
		case ':':
			switch state {
			case "name":
				tok.Name = trimmed()
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
					tok.Value = v
					addToken()
				}
				replaceState("before-name")
			case "value-paren":
				buf = append(buf, ch)
			case "at-value":
				tok.Value = trimmed()
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
				tok.Text = trimmed()
				addToken()
				replaceState("before-name")
				depth++
			case "at-group":
				tok.Name = trimmed()
				switch tok.Type {
				case "font-face", "viewport", "page":
					pushState("before-name")
				default:
					pushState("before-selector")
				}
				addToken()
				depth++
			case "name", "at-rule":
				tok.Name = trimmed()
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
					tok.Value = trimmed()
				}
				if tok.Name != "" && tok.Value != "" {
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
					tok.Text = string(buf)
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
				for _, r := range atRules {
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
						tok.Prefix = r.prefix
					}
					if r.typ != "" {
						tok.Type = r.typ
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
func jsTrim(s string) string { return strings.TrimFunc(s, JSSpace) }

// JSSpace reports whether r is in JavaScript's \s.
func JSSpace(r rune) bool { return r == 0xfeff || r != 0x85 && unicode.IsSpace(r) }

// Node is a node of mensch's AST. Rules is non-nil for an at-rule that
// holds rules; Decls for a rule or an at-rule that holds declarations.
type Node struct {
	Type, Name, Value, Text, Prefix string
	Selectors                       []string
	Decls, Rules                    []Node
}

// Parse is mensch's parser, with comments kept as juice asks.
func Parse(toks []Token) []Node {
	p := parser{toks: toks}
	var rules []Node
	for len(p.toks) > 0 {
		if n, ok := p.token(p.next()); ok {
			rules = append(rules, n)
		}
	}
	return rules
}

type parser struct {
	toks  []Token
	depth int // shared by every open at-rule, as in mensch
}

func (p *parser) next() Token {
	t := p.toks[0]
	p.toks = p.toks[1:]
	return t
}

func (p *parser) token(t Token) (Node, bool) {
	n := Node{Type: t.Type, Name: t.Name, Value: t.Value}
	switch t.Type {
	case "property", "charset", "import", "namespace":
	case "selector":
		n.Type = "rule"
		for _, s := range strings.Split(t.Text, ",") {
			n.Selectors = append(n.Selectors, jsTrim(s))
		}
		n.Decls = p.declarations()
	case "comment":
		n.Text = t.Text
	case "media", "keyframes", "supports", "document":
		p.depth++
		n.Prefix = t.Prefix
		n.Rules = p.while(func(Token) bool { return p.depth != 0 })
		if n.Rules == nil {
			n.Rules = []Node{}
		}
	case "font-face", "viewport", "page":
		p.depth++
		n.Decls = p.declarations()
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
func (p *parser) while(cond func(Token) bool) []Node {
	var nodes []Node
	for len(p.toks) > 0 {
		t := p.next()
		if !cond(t) {
			if t.Type != "end" {
				p.toks = append([]Token{t}, p.toks...)
			}
			return nodes
		}
		if n, ok := p.token(t); ok {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (p *parser) declarations() []Node {
	return p.while(func(t Token) bool { return t.Type == "property" || t.Type == "comment" })
}

// Stringify is mensch.stringify for one node, as juice 9 calls it:
// comments dropped, two-space indentation, trimmed.
func Stringify(n Node) []byte {
	var b bytes.Buffer
	nodeText(&b, n, 1)
	return []byte(jsTrim(b.String()))
}

func indent(b *bytes.Buffer, level int) {
	for range level - 1 {
		b.WriteString("  ")
	}
}

func nodeText(b *bytes.Buffer, n Node, level int) {
	switch n.Type {
	case "rule":
		writeRule(b, n, level)
	case "import", "charset", "namespace":
		b.WriteString("@" + n.Type + " " + n.Value + ";\n")
	case "media", "keyframes", "font-face", "supports", "viewport", "document", "page":
		b.WriteString("@" + n.Prefix + n.Type)
		if n.Name != "" {
			b.WriteString(" " + n.Name)
		}
		b.WriteByte(' ')
		writeBlock(b, n, level, n.Type != "page")
		b.WriteByte('\n')
	}
}

func writeRule(b *bytes.Buffer, n Node, level int) {
	indent(b, level)
	if n.Selectors != nil {
		b.WriteString(strings.Join(n.Selectors, ",\n"))
	} else {
		b.WriteString("@" + n.Type)
		if n.Name != "" {
			b.WriteString(" " + n.Name)
		}
	}
	b.WriteByte(' ')
	writeBlock(b, n, level, false)
	b.WriteByte('\n')
}

func writeBlock(b *bytes.Buffer, n Node, level int, chomp bool) {
	var parts [][]byte
	if n.Rules != nil {
		for _, c := range n.Rules {
			if c.Type == "comment" {
				continue
			}
			var cb bytes.Buffer
			writeRule(&cb, c, level+1)
			parts = append(parts, cb.Bytes())
		}
	} else {
		for _, c := range n.Decls {
			if c.Type != "property" {
				continue
			}
			var cb bytes.Buffer
			indent(&cb, level+1)
			if c.Name != "" {
				cb.WriteString(c.Name + ": ")
			}
			if c.Value == "" {
				// Only a declaration cut short by "{" has no value.
				cb.WriteString("undefined;")
			} else {
				cb.WriteString(c.Value + ";")
			}
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
	indent(b, level)
	b.WriteByte('}')
}
