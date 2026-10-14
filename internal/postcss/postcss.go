// Package postcss is a port of the CSS reading juice 12 does: postcss 8's
// tokenizer and parser with postcss-safe-parser's recovery, which never
// fails, and the stringifier with juice's normalized raws for preserved rules.
package postcss

import (
	"bytes"
	"slices"
	"strings"
)

type token struct {
	kind       string // space word string at-word brackets comment ( ) [ ] { } : ;
	text       string
	start, end int
}

type tokenizer struct {
	css      string
	pos      int
	buffer   []string // words, for url( detection
	returned []token
	badParen int
}

func newTokenizer(css string) *tokenizer { return &tokenizer{css: css, badParen: -1} }

func (t *tokenizer) endOfFile() bool { return len(t.returned) == 0 && t.pos >= len(t.css) }

func (t *tokenizer) back(tok token) { t.returned = append(t.returned, tok) }

func (t *tokenizer) at(i int) byte {
	if i < 0 || i >= len(t.css) {
		return 0
	}
	return t.css[i]
}

func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\f' }

// atEnd and wordEnd are postcss's RE_AT_END and RE_WORD_END.
func atEnd(c byte) bool { return strings.IndexByte("\t\n\f\r \"#'()/;[\\]{}", c) >= 0 }

func (t *tokenizer) wordEnd(i int) bool {
	c := t.css[i]
	return strings.IndexByte("\t\n\f\r !\"#'():;@[\\]{}", c) >= 0 || c == '/' && t.at(i+1) == '*'
}

func hexDigit(c byte) bool { return c >= '0' && c <= '9' || c|0x20 >= 'a' && c|0x20 <= 'f' }

// next returns the next token, or ok false at the end. Unclosed strings,
// brackets and comments are recovered from as postcss does with ignoreErrors.
func (t *tokenizer) next() (tok token, ok bool) {
	if n := len(t.returned); n > 0 {
		tok = t.returned[n-1]
		t.returned = t.returned[:n-1]
		return tok, true
	}
	css, pos := t.css, t.pos
	if pos >= len(css) {
		return tok, false
	}
	c := css[pos]
	var next int
	switch {
	case isSpace(c):
		next = pos
		for next < len(css) && isSpace(css[next]) {
			next++
		}
		tok = token{"space", css[pos:next], pos, next - 1}
		pos = next - 1
	case strings.IndexByte("[]{}:;)", c) >= 0:
		tok = token{css[pos : pos+1], css[pos : pos+1], pos, pos}
	case c == '(':
		prev := ""
		if n := len(t.buffer); n > 0 {
			prev = t.buffer[n-1]
			t.buffer = t.buffer[:n-1]
		}
		n := t.at(pos + 1)
		if prev == "url" && n != '\'' && n != '"' && !isSpace(n) {
			next = pos
			for {
				escaped := false
				k := strings.IndexByte(css[next+1:], ')')
				if k < 0 {
					next = pos
					break
				}
				next += 1 + k
				for e := next; t.at(e-1) == '\\'; e-- {
					escaped = !escaped
				}
				if !escaped {
					break
				}
			}
			tok = token{"brackets", css[pos : next+1], pos, next}
			pos = next
		} else if pos <= t.badParen {
			tok = token{"(", "(", pos, pos}
		} else {
			k := strings.IndexByte(css[pos+1:], ')')
			if k < 0 {
				t.badParen = len(css)
				tok = token{"(", "(", pos, pos}
			} else {
				next = pos + 1 + k
				content := css[pos : next+1]
				// RE_BAD_BRACKET: any of \r \n " ' ( / \ after the first character.
				if strings.ContainsAny(content[1:], "\r\n\"'(/\\") {
					t.badParen = next
					tok = token{"(", "(", pos, pos}
				} else {
					tok = token{"brackets", content, pos, next}
					pos = next
				}
			}
		}
	case c == '\'' || c == '"':
		next = pos
		for {
			escaped := false
			k := strings.IndexByte(css[next+1:], c)
			if k < 0 {
				next = pos + 1
				break
			}
			next += 1 + k
			for e := next; t.at(e-1) == '\\'; e-- {
				escaped = !escaped
			}
			if !escaped {
				break
			}
		}
		end := min(next+1, len(css))
		tok = token{"string", css[pos:end], pos, next}
		pos = next
	case c == '@':
		next = pos + 1
		for next < len(css) && !atEnd(css[next]) {
			next++
		}
		next--
		tok = token{"at-word", css[pos : next+1], pos, next}
		pos = next
	case c == '\\':
		next = pos
		escape := true
		for t.at(next+1) == '\\' {
			next++
			escape = !escape
		}
		if n := t.at(next + 1); escape && n != '/' && !isSpace(n) && next+1 < len(css) {
			next++
			if hexDigit(t.at(next)) {
				for next+1 < len(css) && hexDigit(css[next+1]) {
					next++
				}
				if t.at(next+1) == ' ' {
					next++
				}
			}
		}
		tok = token{"word", css[pos : next+1], pos, next}
		pos = next
	case c == '/' && t.at(pos+1) == '*':
		k := strings.Index(css[pos+2:], "*/")
		if k < 0 {
			next = len(css) - 1
		} else {
			next = pos + 2 + k + 1
		}
		tok = token{"comment", css[pos : next+1], pos, next}
		pos = next
	default:
		next = pos + 1
		for next < len(css) && !t.wordEnd(next) {
			next++
		}
		next--
		tok = token{"word", css[pos : next+1], pos, next}
		t.buffer = append(t.buffer, tok.text)
		pos = next
	}
	t.pos = pos + 1
	return tok, true
}

// Node is a postcss node, with the raws juice's output depends on.
type Node struct {
	Type     string // root, rule, atrule, decl or comment
	parent   *Node
	Nodes    []*Node
	hasNodes bool
	start    int
	vs, ve   int // where a comment-free value lies in the source, when it does

	Selector, Name, Params, Prop, Value, Text string
	Important                                 bool

	before, between, after, afterName, ownSemicolon string
	hasAfterName, semicolon                         bool
	importantRaw, left, right                       string
	raw                                             map[string][2]string // prop -> {raw, value}
}

func (n *Node) push(c *Node) {
	c.parent = n
	if n.Nodes == nil {
		n.Nodes = make([]*Node, 0, 4) // most rules hold a few declarations
	}
	n.Nodes = append(n.Nodes, c)
}

// parser is postcss's Parser with SafeParser's overrides folded in.
type parser struct {
	t         *tokenizer
	root      *Node
	current   *Node
	spaces    string
	semicolon bool
	scratch   []token // reused by other for each statement
	slab      []Node  // nodes are allocated from here, a block at a time
	slabSize  int
}

// Parse reads css as postcss-safe-parser does, which never fails, into a
// "root" node.
func Parse(css string) *Node {
	root := &Node{Type: "root", hasNodes: true}
	p := &parser{t: newTokenizer(css), root: root, current: root}
	for !p.t.endOfFile() {
		tok, _ := p.t.next()
		switch tok.kind {
		case "space":
			p.spaces += tok.text
		case ";":
			p.freeSemicolon(tok)
		case "}":
			p.end(tok)
		case "comment":
			p.comment(tok)
		case "at-word":
			p.atrule(tok)
		case "{":
			p.emptyRule(tok)
		default:
			p.other(tok)
		}
	}
	p.endFile()
	return root
}

// node allocates from a slab, since a stylesheet makes one per declaration.
func (p *parser) node(kind string) *Node {
	if len(p.slab) == 0 {
		// Blocks grow with the stylesheet, so a small one stays small.
		p.slabSize = min(max(p.slabSize*2, 4), 256)
		p.slab = make([]Node, p.slabSize)
	}
	n := &p.slab[0]
	p.slab = p.slab[1:]
	n.Type = kind
	return n
}

func (p *parser) init(n *Node, offset int) {
	p.current.push(n)
	n.start = offset
	n.before = p.spaces
	p.spaces = ""
	if n.Type != "comment" {
		p.semicolon = false
	}
}

func (p *parser) setRaw(n *Node, prop string, toks []token, custom bool) {
	// Without comments the value is the tokens' source, less a trailing
	// space, so it can be sliced rather than built.
	if !slices.ContainsFunc(toks, func(t token) bool { return t.kind == "comment" }) {
		v, clean := p.src(toks), true
		if l := len(toks); l > 0 && toks[l-1].kind == "space" && !custom {
			v, clean = p.src(toks[:l-1]), false
		}
		if !clean {
			if n.raw == nil {
				n.raw = map[string][2]string{}
			}
			n.raw[prop] = [2]string{p.src(toks), v}
		}
		if len(toks) > 0 && v != "" {
			n.vs, n.ve = toks[0].start, toks[0].start+len(v)
		}
		p.setProp(n, prop, v)
		return
	}
	var value strings.Builder
	clean := true
	for i, tok := range toks {
		switch {
		case tok.kind == "space" && i == len(toks)-1 && !custom:
			clean = false
		case tok.kind == "comment":
			prev, next := "empty", "empty"
			if i > 0 {
				prev = toks[i-1].kind
			}
			if i+1 < len(toks) {
				next = toks[i+1].kind
			}
			safe := func(k string) bool { return k == "empty" || k == "space" }
			if !safe(prev) && !safe(next) {
				if strings.HasSuffix(value.String(), ",") {
					clean = false
				} else {
					value.WriteString(tok.text)
				}
			} else {
				clean = false
			}
		default:
			value.WriteString(tok.text)
		}
	}
	v := value.String()
	if !clean {
		if n.raw == nil {
			n.raw = map[string][2]string{}
		}
		n.raw[prop] = [2]string{p.src(toks), v}
	}
	p.setProp(n, prop, v)
}

func (p *parser) setProp(n *Node, prop, v string) {
	switch prop {
	case "selector":
		n.Selector = v
	case "params":
		n.Params = v
	case "value":
		n.Value = v
	}
}

// src is the source text of consecutive tokens, sliced rather than joined.
func (p *parser) src(toks []token) string {
	if len(toks) == 0 {
		return ""
	}
	return p.t.css[toks[0].start:min(toks[len(toks)-1].end+1, len(p.t.css))]
}

func (p *parser) comment(tok token) {
	n := p.node("comment")
	p.init(n, tok.start)
	text := strings.TrimPrefix(tok.text, "/*")
	text = strings.TrimSuffix(text, "*/")
	if strings.TrimLeft(text, " \t\n\r\f\v") == "" {
		n.left = text
		return
	}
	trimmed := strings.TrimLeft(text, " \t\n\r\f\v")
	n.left = text[:len(text)-len(trimmed)]
	n.Text = strings.TrimRight(trimmed, " \t\n\r\f\v")
	n.right = trimmed[len(n.Text):]
}

func (p *parser) emptyRule(tok token) {
	n := p.node("rule")
	n.hasNodes = true
	p.init(n, tok.start)
	p.current = n
}

func (p *parser) other(start token) {
	end, colon := false, false
	var brackets []string
	custom := strings.HasPrefix(start.text, "--")
	toks := p.scratch[:0]
	defer func() { p.scratch = toks[:0] }()
	tok, ok := start, true
	for ok {
		toks = append(toks, tok)
		switch k := tok.kind; {
		case k == "(" || k == "[":
			closer := ")"
			if k == "[" {
				closer = "]"
			}
			brackets = append(brackets, closer)
		case custom && colon && k == "{":
			brackets = append(brackets, "}")
		case len(brackets) == 0:
			switch k {
			case ";":
				if colon {
					p.decl(toks)
					return
				}
				goto done
			case "{":
				p.rule(toks)
				return
			case "}":
				p.t.back(toks[len(toks)-1])
				toks = toks[:len(toks)-1]
				end = true
				goto done
			case ":":
				colon = true
			}
		case k == brackets[len(brackets)-1]:
			brackets = brackets[:len(brackets)-1]
		}
		tok, ok = p.t.next()
	}
done:
	if p.t.endOfFile() {
		end = true
	}
	if end && colon {
		if !custom {
			for len(toks) > 0 {
				k := toks[len(toks)-1].kind
				if k != "space" && k != "comment" {
					break
				}
				p.t.back(toks[len(toks)-1])
				toks = toks[:len(toks)-1]
			}
		}
		p.decl(toks)
	} else {
		p.unknownWord(toks)
	}
}

func (p *parser) unknownWord(toks []token) { p.spaces += p.src(toks) }

func (p *parser) rule(toks []token) {
	toks = toks[:len(toks)-1]
	n := p.node("rule")
	n.hasNodes = true
	p.init(n, toks[0].start)
	n.between, toks = spacesAndCommentsFromEnd(toks)
	p.setRaw(n, "selector", toks, false)
	p.current = n
}

func spacesAndCommentsFromEnd(toks []token) (string, []token) {
	s := ""
	for len(toks) > 0 {
		k := toks[len(toks)-1].kind
		if k != "space" && k != "comment" {
			break
		}
		s = toks[len(toks)-1].text + s
		toks = toks[:len(toks)-1]
	}
	return s, toks
}

func (p *parser) decl(toks []token) {
	// SafeParser: only with more than one token and a word among them.
	hasWord := false
	for _, t := range toks {
		hasWord = hasWord || t.kind == "word"
	}
	if len(toks) <= 1 || !hasWord {
		return
	}
	n := p.node("decl")
	p.init(n, toks[0].start)
	if toks[len(toks)-1].kind == ";" {
		p.semicolon = true
		toks = toks[:len(toks)-1]
	}
	i := 0
	for toks[i].kind != "word" {
		if i == len(toks)-1 {
			p.unknownWord(toks[i : i+1])
		}
		i++
	}
	n.before += p.src(toks[:i])
	n.start = toks[i].start
	propStart := i
	for i < len(toks) {
		if k := toks[i].kind; k == ":" || k == "space" || k == "comment" {
			break
		}
		i++
	}
	n.Prop = p.src(toks[propStart:i])
	betweenStart := i
	for i < len(toks) {
		tok := toks[i]
		i++
		if tok.kind == ":" {
			break
		}
		if tok.kind == "word" && strings.IndexFunc(tok.text, isWordRune) >= 0 {
			p.unknownWord([]token{tok})
		}
	}
	n.between = p.src(toks[betweenStart:i])
	if n.Prop != "" && (n.Prop[0] == '_' || n.Prop[0] == '*') {
		n.before += n.Prop[:1]
		n.Prop = n.Prop[1:]
	}
	firstSpacesStart := i
	for i < len(toks) {
		if k := toks[i].kind; k != "space" && k != "comment" {
			break
		}
		i++
	}
	firstSpaces := toks[firstSpacesStart:i]
	toks = p.precheckMissedSemicolon(toks[i:])

	for j := len(toks) - 1; j >= 0; j-- {
		tok := toks[j]
		if strings.EqualFold(tok.text, "!important") {
			n.Important = true
			str := p.src(toks[j:])
			toks = toks[:j]
			sp := ""
			for len(toks) > 0 && toks[len(toks)-1].kind == "space" {
				sp = toks[len(toks)-1].text + sp
				toks = toks[:len(toks)-1]
			}
			if str = sp + str; str != " !important" {
				n.importantRaw = str
			}
			break
		} else if strings.EqualFold(tok.text, "important") {
			cache := append([]token(nil), toks...)
			str := ""
			for k := j; k > 0; k-- {
				if strings.HasPrefix(strings.TrimSpace(str), "!") && cache[k].kind != "space" {
					break
				}
				str = cache[len(cache)-1].text + str
				cache = cache[:len(cache)-1]
			}
			if strings.HasPrefix(strings.TrimSpace(str), "!") {
				n.Important = true
				n.importantRaw = str
				toks = cache
			}
		}
		if tok.kind != "space" && tok.kind != "comment" {
			break
		}
	}
	word := false
	for _, t := range toks {
		word = word || t.kind != "space" && t.kind != "comment"
	}
	if word {
		n.between += p.src(firstSpaces)
		firstSpaces = nil
	}
	if len(firstSpaces) > 0 {
		toks = append(append([]token(nil), firstSpaces...), toks...)
	}
	p.setRaw(n, "value", toks, false)
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// findColon is the index of the first top-level colon, or -1.
func findColon(toks []token) int {
	brackets := 0
	for i, t := range toks {
		switch t.kind {
		case "(":
			brackets++
		case ")":
			brackets--
		case ":":
			if brackets == 0 && i > 0 && !(toks[i-1].kind == "word" && toks[i-1].text == "progid") {
				return i
			}
		}
	}
	return -1
}

// precheckMissedSemicolon is SafeParser's: a second "word:" in a value
// starts a declaration of its own.
func (p *parser) precheckMissedSemicolon(toks []token) []token {
	colon := findColon(toks)
	if colon < 0 {
		return toks
	}
	nextStart := colon - 1
	for ; nextStart >= 0; nextStart-- {
		if toks[nextStart].kind == "word" {
			break
		}
	}
	if nextStart <= 0 {
		return toks
	}
	prevEnd := nextStart - 1
	for ; prevEnd >= 0; prevEnd-- {
		if toks[prevEnd].kind != "space" {
			prevEnd++
			break
		}
	}
	other := append([]token(nil), toks[nextStart:]...)
	p.spaces = p.src(toks[prevEnd:nextStart])
	toks = toks[:prevEnd]
	p.decl(other)
	return toks
}

func (p *parser) atrule(tok token) {
	n := p.node("atrule")
	n.Name = tok.text[1:]
	p.init(n, tok.start)
	var params []token
	var brackets []string
	last, open := false, false
	for !p.t.endOfFile() {
		tok, _ = p.t.next()
		k := tok.kind
		switch {
		case k == "(" || k == "[":
			closer := ")"
			if k == "[" {
				closer = "]"
			}
			brackets = append(brackets, closer)
		case k == "{" && len(brackets) > 0:
			brackets = append(brackets, "}")
		case len(brackets) > 0 && k == brackets[len(brackets)-1]:
			brackets = brackets[:len(brackets)-1]
		}
		if len(brackets) == 0 {
			if k == ";" {
				p.semicolon = true
				break
			} else if k == "{" {
				open = true
				break
			} else if k == "}" {
				p.end(tok)
				break
			}
			params = append(params, tok)
		} else {
			params = append(params, tok)
		}
		if p.t.endOfFile() {
			last = true
			break
		}
	}
	n.between, params = spacesAndCommentsFromEnd(params)
	n.hasAfterName = true
	if len(params) > 0 {
		s := ""
		for len(params) > 0 && (params[0].kind == "space" || params[0].kind == "comment") {
			s += params[0].text
			params = params[1:]
		}
		n.afterName = s
		p.setRaw(n, "params", params, false)
		if last {
			p.spaces = n.between
			n.between = ""
		}
	}
	if open {
		n.hasNodes = true
		p.current = n
	}
}

func (p *parser) freeSemicolon(tok token) {
	p.spaces += tok.text
	if c := p.current; c.hasNodes && len(c.Nodes) > 0 {
		if prev := c.Nodes[len(c.Nodes)-1]; prev.Type == "rule" && prev.ownSemicolon == "" {
			prev.ownSemicolon = p.spaces
			p.spaces = ""
		}
	}
}

func (p *parser) end(tok token) {
	if len(p.current.Nodes) > 0 {
		p.current.semicolon = p.semicolon
	}
	p.semicolon = false
	p.current.after += p.spaces
	p.spaces = ""
	if p.current.parent != nil {
		p.current = p.current.parent
	} else {
		p.current.after += "}"
	}
}

func (p *parser) endFile() {
	if len(p.current.Nodes) > 0 {
		p.current.semicolon = p.semicolon
	}
	p.current.after += p.spaces
	for p.current.parent != nil {
		p.current = p.current.parent
		p.current.after = ""
	}
}

// SelectorBytes is postcss's rule.selectors (list.comma), sliced from src, the
// bytes the rule was parsed from, when the selector is a slice of it.
func (n *Node) SelectorBytes(src []byte) [][]byte {
	if n.ve > n.vs && n.ve-n.vs == len(n.Selector) {
		return commaList(src[n.vs:n.ve:n.ve])
	}
	return commaList([]byte(n.Selector))
}

// ValueBytes is a declaration's Value, sliced from src as SelectorBytes is.
func (n *Node) ValueBytes(src []byte) []byte {
	if n.ve > n.vs && n.ve-n.vs == len(n.Value) {
		return src[n.vs:n.ve:n.ve]
	}
	return []byte(n.Value)
}

func commaList(s []byte) [][]byte {
	var out [][]byte
	start := 0
	fn, quote, escape := 0, byte(0), false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escape:
			escape = false
		case c == '\\':
			escape = true
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			fn++
		case c == ')':
			if fn > 0 {
				fn--
			}
		case fn == 0 && c == ',':
			out = append(out, bytes.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, bytes.TrimSpace(s[start:]))
}

// Stringify is juice's stringifyNodes for one node: postcss's stringifier over
// a clone whose raws are normalized to one rule per line and two-space
// indentation.
func Stringify(n *Node) string {
	var b strings.Builder
	writeNode(&b, n, 0, true, true)
	return b.String()
}

func rawValue(n *Node, prop, value string) string {
	if r, ok := n.raw[prop]; ok && r[1] == value {
		return r[0]
	}
	return value
}

func writeNode(b *strings.Builder, n *Node, depth int, first, semicolon bool) {
	indent := strings.Repeat("  ", depth)
	if !first {
		b.WriteString("\n" + indent)
	}
	switch n.Type {
	case "comment":
		b.WriteString("/*" + n.left + n.Text + n.right + "*/")
	case "decl":
		b.WriteString(n.Prop + ": " + rawValue(n, "value", n.Value))
		if n.Important {
			if n.importantRaw != "" {
				b.WriteString(n.importantRaw)
			} else {
				b.WriteString(" !important")
			}
		}
		if semicolon {
			b.WriteByte(';')
		}
	case "rule", "atrule":
		head := rawValue(n, "selector", n.Selector)
		if n.Type == "atrule" {
			params := ""
			if n.Params != "" {
				params = rawValue(n, "params", n.Params)
			}
			head = "@" + n.Name
			if n.hasAfterName {
				head += n.afterName
			} else if params != "" {
				head += " "
			}
			head += params
		}
		if !n.hasNodes {
			b.WriteString(head)
			if semicolon {
				b.WriteByte(';')
			}
			return
		}
		b.WriteString(head + " {")
		for _, c := range n.Nodes {
			writeNode(b, c, depth+1, false, true)
		}
		b.WriteString("\n" + indent + "}")
		if n.Type == "rule" {
			b.WriteString(n.ownSemicolon)
		}
	}
}
