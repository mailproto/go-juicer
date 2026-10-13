package juicer

import (
	"os"
	"slices"
	"strings"
)

// A port of the CSS reading juice 12 does: postcss 8's tokenizer and parser
// with postcss-safe-parser's recovery, which never fails, and the stringifier
// with juice's normalized raws for preserved rules. SPIKE: it stands in for
// the tdewolff-based reading behind JUICER_POSTCSS, to measure whether one
// parser modelled on postcss can replace the several now in use.

type pcToken struct {
	kind       string // space word string at-word brackets comment ( ) [ ] { } : ;
	text       string
	start, end int
}

type pcTokenizer struct {
	css      string
	pos      int
	buffer   []string // words, for url( detection
	returned []pcToken
	badParen int
}

func newPCTokenizer(css string) *pcTokenizer { return &pcTokenizer{css: css, badParen: -1} }

func (t *pcTokenizer) endOfFile() bool { return len(t.returned) == 0 && t.pos >= len(t.css) }

func (t *pcTokenizer) back(tok pcToken) { t.returned = append(t.returned, tok) }

func (t *pcTokenizer) at(i int) byte {
	if i < 0 || i >= len(t.css) {
		return 0
	}
	return t.css[i]
}

func pcSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\f' }

// atEnd and wordEnd are postcss's RE_AT_END and RE_WORD_END.
func atEnd(c byte) bool { return strings.IndexByte("\t\n\f\r \"#'()/;[\\]{}", c) >= 0 }

func (t *pcTokenizer) wordEnd(i int) bool {
	c := t.css[i]
	return strings.IndexByte("\t\n\f\r !\"#'():;@[\\]{}", c) >= 0 || c == '/' && t.at(i+1) == '*'
}

func hexDigit(c byte) bool { return c >= '0' && c <= '9' || c|0x20 >= 'a' && c|0x20 <= 'f' }

// next returns the next token, or ok false at the end. Unclosed strings,
// brackets and comments are recovered from as postcss does with ignoreErrors.
func (t *pcTokenizer) next() (tok pcToken, ok bool) {
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
	case pcSpace(c):
		next = pos
		for next < len(css) && pcSpace(css[next]) {
			next++
		}
		tok = pcToken{"space", css[pos:next], pos, next - 1}
		pos = next - 1
	case strings.IndexByte("[]{}:;)", c) >= 0:
		tok = pcToken{string(c), string(c), pos, pos}
	case c == '(':
		prev := ""
		if n := len(t.buffer); n > 0 {
			prev = t.buffer[n-1]
			t.buffer = t.buffer[:n-1]
		}
		n := t.at(pos + 1)
		if prev == "url" && n != '\'' && n != '"' && !pcSpace(n) {
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
			tok = pcToken{"brackets", css[pos : next+1], pos, next}
			pos = next
		} else if pos <= t.badParen {
			tok = pcToken{"(", "(", pos, pos}
		} else {
			k := strings.IndexByte(css[pos+1:], ')')
			if k < 0 {
				t.badParen = len(css)
				tok = pcToken{"(", "(", pos, pos}
			} else {
				next = pos + 1 + k
				content := css[pos : next+1]
				// RE_BAD_BRACKET: any of \r \n " ' ( / \ after the first character.
				if strings.ContainsAny(content[1:], "\r\n\"'(/\\") {
					t.badParen = next
					tok = pcToken{"(", "(", pos, pos}
				} else {
					tok = pcToken{"brackets", content, pos, next}
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
		tok = pcToken{"string", css[pos:end], pos, next}
		pos = next
	case c == '@':
		next = pos + 1
		for next < len(css) && !atEnd(css[next]) {
			next++
		}
		next--
		tok = pcToken{"at-word", css[pos : next+1], pos, next}
		pos = next
	case c == '\\':
		next = pos
		escape := true
		for t.at(next+1) == '\\' {
			next++
			escape = !escape
		}
		if n := t.at(next + 1); escape && n != '/' && !pcSpace(n) && next+1 < len(css) {
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
		tok = pcToken{"word", css[pos : next+1], pos, next}
		pos = next
	case c == '/' && t.at(pos+1) == '*':
		k := strings.Index(css[pos+2:], "*/")
		if k < 0 {
			next = len(css) - 1
		} else {
			next = pos + 2 + k + 1
		}
		tok = pcToken{"comment", css[pos : next+1], pos, next}
		pos = next
	default:
		next = pos + 1
		for next < len(css) && !t.wordEnd(next) {
			next++
		}
		next--
		tok = pcToken{"word", css[pos : next+1], pos, next}
		t.buffer = append(t.buffer, tok.text)
		pos = next
	}
	t.pos = pos + 1
	return tok, true
}

// pcNode is a postcss node, with the raws juice's output depends on.
type pcNode struct {
	kind     string // root rule atrule decl comment
	parent   *pcNode
	nodes    []*pcNode
	hasNodes bool
	start    int
	vs, ve   int // where a comment-free value lies in the source, when it does

	selector, name, params, prop, value, text string
	important                                 bool

	before, between, after, afterName, ownSemicolon string
	hasAfterName, semicolon                         bool
	importantRaw, left, right                       string
	raw                                             map[string][2]string // prop -> {raw, value}
}

func (n *pcNode) push(c *pcNode) {
	c.parent = n
	n.nodes = append(n.nodes, c)
}

// pcParser is postcss's Parser with SafeParser's overrides folded in.
type pcParser struct {
	t         *pcTokenizer
	root      *pcNode
	current   *pcNode
	spaces    string
	semicolon bool
	scratch   []pcToken // reused by other for each statement
}

func parsePostcss(css string) *pcNode {
	root := &pcNode{kind: "root", hasNodes: true}
	p := &pcParser{t: newPCTokenizer(css), root: root, current: root}
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

func (p *pcParser) init(n *pcNode, offset int) {
	p.current.push(n)
	n.start = offset
	n.before = p.spaces
	p.spaces = ""
	if n.kind != "comment" {
		p.semicolon = false
	}
}

func (p *pcParser) setRaw(n *pcNode, prop string, toks []pcToken, custom bool) {
	// Without comments the value is the tokens' source, less a trailing
	// space, so it can be sliced rather than built.
	if !slices.ContainsFunc(toks, func(t pcToken) bool { return t.kind == "comment" }) {
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

func (p *pcParser) setProp(n *pcNode, prop, v string) {
	switch prop {
	case "selector":
		n.selector = v
	case "params":
		n.params = v
	case "value":
		n.value = v
	}
}

// src is the source text of consecutive tokens, sliced rather than joined.
func (p *pcParser) src(toks []pcToken) string {
	if len(toks) == 0 {
		return ""
	}
	return p.t.css[toks[0].start:min(toks[len(toks)-1].end+1, len(p.t.css))]
}

func (p *pcParser) comment(tok pcToken) {
	n := &pcNode{kind: "comment"}
	p.init(n, tok.start)
	text := strings.TrimPrefix(tok.text, "/*")
	text = strings.TrimSuffix(text, "*/")
	if strings.TrimLeft(text, " \t\n\r\f\v") == "" {
		n.left = text
		return
	}
	trimmed := strings.TrimLeft(text, " \t\n\r\f\v")
	n.left = text[:len(text)-len(trimmed)]
	n.text = strings.TrimRight(trimmed, " \t\n\r\f\v")
	n.right = trimmed[len(n.text):]
}

func (p *pcParser) emptyRule(tok pcToken) {
	n := &pcNode{kind: "rule", hasNodes: true}
	p.init(n, tok.start)
	p.current = n
}

func (p *pcParser) other(start pcToken) {
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

func (p *pcParser) unknownWord(toks []pcToken) { p.spaces += p.src(toks) }

func (p *pcParser) rule(toks []pcToken) {
	toks = toks[:len(toks)-1]
	n := &pcNode{kind: "rule", hasNodes: true}
	p.init(n, toks[0].start)
	n.between, toks = spacesAndCommentsFromEnd(toks)
	p.setRaw(n, "selector", toks, false)
	p.current = n
}

func spacesAndCommentsFromEnd(toks []pcToken) (string, []pcToken) {
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

func (p *pcParser) decl(toks []pcToken) {
	// SafeParser: only with more than one token and a word among them.
	hasWord := false
	for _, t := range toks {
		hasWord = hasWord || t.kind == "word"
	}
	if len(toks) <= 1 || !hasWord {
		return
	}
	n := &pcNode{kind: "decl"}
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
	n.prop = p.src(toks[propStart:i])
	betweenStart := i
	for i < len(toks) {
		tok := toks[i]
		i++
		if tok.kind == ":" {
			break
		}
		if tok.kind == "word" && strings.IndexFunc(tok.text, isWordRune) >= 0 {
			p.unknownWord([]pcToken{tok})
		}
	}
	n.between = p.src(toks[betweenStart:i])
	if n.prop != "" && (n.prop[0] == '_' || n.prop[0] == '*') {
		n.before += n.prop[:1]
		n.prop = n.prop[1:]
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
			n.important = true
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
			cache := append([]pcToken(nil), toks...)
			str := ""
			for k := j; k > 0; k-- {
				if strings.HasPrefix(strings.TrimSpace(str), "!") && cache[k].kind != "space" {
					break
				}
				str = cache[len(cache)-1].text + str
				cache = cache[:len(cache)-1]
			}
			if strings.HasPrefix(strings.TrimSpace(str), "!") {
				n.important = true
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
		toks = append(append([]pcToken(nil), firstSpaces...), toks...)
	}
	p.setRaw(n, "value", toks, false)
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// colon is the index of the first top-level colon, or -1.
func pcColon(toks []pcToken) int {
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
func (p *pcParser) precheckMissedSemicolon(toks []pcToken) []pcToken {
	colon := pcColon(toks)
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
	other := append([]pcToken(nil), toks[nextStart:]...)
	p.spaces = p.src(toks[prevEnd:nextStart])
	toks = toks[:prevEnd]
	p.decl(other)
	return toks
}

func (p *pcParser) atrule(tok pcToken) {
	n := &pcNode{kind: "atrule", name: tok.text[1:]}
	p.init(n, tok.start)
	var params []pcToken
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

func (p *pcParser) freeSemicolon(tok pcToken) {
	p.spaces += tok.text
	if c := p.current; c.hasNodes && len(c.nodes) > 0 {
		if prev := c.nodes[len(c.nodes)-1]; prev.kind == "rule" && prev.ownSemicolon == "" {
			prev.ownSemicolon = p.spaces
			p.spaces = ""
		}
	}
}

func (p *pcParser) end(tok pcToken) {
	if len(p.current.nodes) > 0 {
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

func (p *pcParser) endFile() {
	if len(p.current.nodes) > 0 {
		p.current.semicolon = p.semicolon
	}
	p.current.after += p.spaces
	for p.current.parent != nil {
		p.current = p.current.parent
		p.current.after = ""
	}
}

// selectors is postcss's rule.selectors: list.comma.
func (n *pcNode) selectors() []string {
	var out []string
	var cur strings.Builder
	fn, quote, escape := 0, byte(0), false
	for i := 0; i < len(n.selector); i++ {
		c := n.selector[i]
		split := false
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
			split = true
		}
		if split {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		} else {
			cur.WriteByte(c)
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

// stringifyPreserved is juice's stringifyNodes: postcss's stringifier over
// clones whose raws are normalized to one rule per line and two-space
// indentation.
func stringifyPreserved(nodes []*pcNode) string {
	var b strings.Builder
	for i, n := range nodes {
		writeNode(&b, n, 0, i == 0, true)
	}
	return b.String()
}

func rawValue(n *pcNode, prop, value string) string {
	if r, ok := n.raw[prop]; ok && r[1] == value {
		return r[0]
	}
	return value
}

func writeNode(b *strings.Builder, n *pcNode, depth int, first, semicolon bool) {
	indent := strings.Repeat("  ", depth)
	if !first {
		b.WriteString("\n" + indent)
	}
	switch n.kind {
	case "comment":
		b.WriteString("/*" + n.left + n.text + n.right + "*/")
	case "decl":
		b.WriteString(n.prop + ": " + rawValue(n, "value", n.value))
		if n.important {
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
		head := rawValue(n, "selector", n.selector)
		if n.kind == "atrule" {
			params := ""
			if n.params != "" {
				params = rawValue(n, "params", n.params)
			}
			head = "@" + n.name
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
		for _, c := range n.nodes {
			writeNode(b, c, depth+1, false, true)
		}
		b.WriteString("\n" + indent + "}")
		if n.kind == "rule" {
			b.WriteString(n.ownSemicolon)
		}
	}
}

// usePostcss switches parseStylesheet to the postcss port. SPIKE only.
var usePostcss = os.Getenv("JUICER_POSTCSS") != ""

// parseStylesheetPostcss is parseStylesheet over the postcss port: juice's
// parseCSS for the rules, and its getPreservedText for what stays behind.
func parseStylesheetPostcss(src []byte, opts *options, ord uint32) ([]rule, []preserved, uint32) {
	flat := flattenNesting(src)
	text := string(flat)
	if opts.fixesJuiceBugs() {
		if b := blankCommentMarkers(text); b != text {
			text, flat = b, []byte(b)
		}
	}
	root := parsePostcss(text)
	var rules []rule
	var keep []preserved
	for _, n := range root.nodes {
		switch n.kind {
		case "atrule":
			if kind := atRuleKind([]byte("@" + n.name)); preserveKind(kind, opts) {
				keep = append(keep, preserved{kind, []byte(stringifyPreserved([]*pcNode{n}))})
			}
		case "rule":
			var decls []decl
			for _, c := range n.nodes {
				if c.kind != "decl" || strings.TrimSpace(c.value) == "" {
					continue
				}
				// A value sliced from the source needs no copy.
				var v []byte
				if c.ve > c.vs && c.ve-c.vs == len(c.value) {
					v = flat[c.vs:c.ve:c.ve]
				} else {
					v = []byte(c.value)
				}
				d := decl{prop: c.prop, value: v, important: c.important, ord: ord}
				if c.important {
					d.bang = []byte(c.value + " !important")
				}
				decls = append(decls, d)
				ord++
			}
			sels := n.selectors()
			if len(decls) > 0 {
				for _, sel := range sels {
					armRules, ignored := compileRule([]byte(sel), decls, opts)
					if ignored {
						continue
					}
					for k := range armRules {
						armRules[k].group = int32(len(rules))
					}
					rules = append(rules, armRules...)
				}
			}
			arms := make([][]byte, len(sels))
			for i, s := range sels {
				arms[i] = []byte(s)
			}
			if opts.keepAll || opts.preservePseudos && len(arms) > 0 && hasIgnoredPseudo(arms[0]) || matchesPreserved(arms, opts.preservedSelectors) {
				keep = append(keep, preserved{"pseudo", []byte(stringifyPreserved([]*pcNode{n}))})
			}
		default:
			if opts.keepAll && n.kind == "comment" {
				keep = append(keep, preserved{"comment", []byte(stringifyPreserved([]*pcNode{n}))})
			}
		}
	}
	return rules, keep, ord
}

// blankCommentMarkers hides "<!--" and "-->" outside strings and comments,
// which CSS ignores in a stylesheet and postcss reads as words.
func blankCommentMarkers(css string) string {
	if !strings.Contains(css, "<!--") && !strings.Contains(css, "-->") {
		return css
	}
	b := []byte(css)
	var q quotes
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '"' || b[i] == '\'':
			i = q.end(b, i)
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			if k := strings.Index(css[i+2:], "*/"); k >= 0 {
				i += k + 3
			} else {
				i = len(b)
			}
		case strings.HasPrefix(css[i:], "<!--"):
			copy(b[i:], "    ")
			i += 3
		case strings.HasPrefix(css[i:], "-->") && (i == 0 || !isSelectorNameChar(b[i-1])):
			copy(b[i:], "   ")
			i += 2
		}
	}
	return string(b)
}
