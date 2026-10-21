package postcss

import (
	"regexp"
	"strings"
)

// parsesSelector reports whether postcss-selector-parser 7.1 reads sel
// without throwing. postcss-nesting leaves a rule as it is when either its
// selector or its parent's does not parse, so this ports the parser's control
// flow and every place it throws, building nothing.
func parsesSelector(sel string) (ok bool) {
	toks, ok := selTokenize(sel)
	if !ok {
		return false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	p := selParser{css: sel, toks: toks}
	p.push()
	for p.pos < len(p.toks) {
		p.parse(true)
	}
	return true
}

type selTokKind uint8

const (
	stSpace selTokKind = iota
	stComment
	stWord
	stStr
	stCombinator // + > ~ |, in a run
	stAsterisk
	stAmpersand
	stBang
	stComma
	stEquals
	stDollar
	stCaret
	stOpenSquare
	stCloseSquare
	stColon
	stSemicolon
	stOpenParen
	stCloseParen
	stSlash
)

type selTok struct {
	kind       selTokKind
	start, end int
}

var selSingle = map[byte]selTokKind{
	'*': stAsterisk, '&': stAmpersand, '!': stBang, ',': stComma, '=': stEquals, '$': stDollar,
	'^': stCaret, '[': stOpenSquare, ']': stCloseSquare, ':': stColon, ';': stSemicolon,
	'(': stOpenParen, ')': stCloseParen,
}

func selDelimiter(c byte) bool {
	return strings.IndexByte(" \t\n\r\f&*!,:;()[]'\"+|~>=$^/", c) >= 0
}

func selHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// selTokenize is postcss-selector-parser's tokenize. It fails where that
// throws: on an unclosed string or comment.
func selTokenize(css string) ([]selTok, bool) {
	var toks []selTok
	at := func(i int) byte {
		if i < len(css) {
			return css[i]
		}
		return 0
	}
	for start := 0; start < len(css); {
		c := css[start]
		var t selTok
		end := start + 1
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			for end < len(css) && strings.IndexByte(" \t\n\r\f", css[end]) >= 0 {
				end++
			}
			t.kind = stSpace
		case c == '+' || c == '>' || c == '~' || c == '|':
			for end < len(css) && strings.IndexByte("+>~|", css[end]) >= 0 {
				end++
			}
			t.kind = stCombinator
		case c == '\'' || c == '"':
			next := start
			for {
				k := strings.IndexByte(css[next+1:], c)
				if k < 0 {
					return nil, false
				}
				next += 1 + k
				escaped := false
				for e := next; e > 0 && css[e-1] == '\\'; e-- {
					escaped = !escaped
				}
				if !escaped {
					break
				}
			}
			t.kind, end = stStr, next+1
		case c == '/' && at(start+1) == '*':
			k := strings.Index(css[start+2:], "*/")
			if k < 0 {
				return nil, false
			}
			t.kind, end = stComment, start+2+k+2
		case c == '/':
			t.kind = stSlash
		default:
			if k, ok := selSingle[c]; ok {
				t.kind = k
				break
			}
			// consumeWord, with its escapes.
			next := start
			for next < len(css) && !selDelimiter(css[next]) {
				if css[next] == '\\' {
					next = selEscapeEnd(css, next) + 1
				} else {
					next++
				}
			}
			t.kind, end = stWord, max(next, start+1)
		}
		end = min(end, len(css))
		t.start, t.end = start, end
		toks = append(toks, t)
		start = end
	}
	return toks, true
}

// selEscapeEnd is consumeEscape: the index of an escape's last character.
func selEscapeEnd(css string, start int) int {
	next := start
	if next+1 >= len(css) {
		return next + 1
	}
	switch c := css[next+1]; {
	case c == '\t' || c == '\n' || c == '\r' || c == '\f':
	case selHex(c):
		digits := 0
		for {
			next++
			digits++
			if next+1 >= len(css) || !selHex(css[next+1]) || digits >= 6 {
				break
			}
		}
		if digits < 6 && next+1 < len(css) && css[next+1] == ' ' {
			next++
		}
	default:
		next++
	}
	return next
}

// selParser walks tokens as postcss-selector-parser's Parser does. Failures
// panic, as the parser throws; parsesSelector recovers.
type selParser struct {
	css   string
	toks  []selTok
	pos   int
	depth int
	stack []*selScope // the selector being read, innermost last
}

// selScope is what the parser consults about the selector it is building.
type selScope struct {
	last       byte // kind of the last node appended: 0 none, 'p' pseudo, 'c' comment, 'n' other
	nonComment bool
}

type selFail struct{}

func (p *selParser) fail()          { panic(selFail{}) }
func (p *selParser) push()          { p.stack = append(p.stack, &selScope{}) }
func (p *selParser) cur() *selScope { return p.stack[len(p.stack)-1] }
func (p *selParser) tok(i int) *selTok {
	if i >= 0 && i < len(p.toks) {
		return &p.toks[i]
	}
	return nil
}
func (p *selParser) curTok() *selTok          { return p.tok(p.pos) }
func (p *selParser) next() *selTok            { return p.tok(p.pos + 1) }
func (p *selParser) content(t *selTok) string { return p.css[t.start:t.end] }

func (p *selParser) node(kind byte) {
	s := p.cur()
	s.last = kind
	if kind != 'c' {
		s.nonComment = true
	}
}

func (p *selParser) parse(throwOnParen bool) {
	switch p.curTok().kind {
	case stSpace:
		p.space()
	case stComment:
		p.node('c')
		p.pos++
	case stOpenParen:
		p.parentheses()
	case stCloseParen:
		if throwOnParen {
			p.fail()
		}
	case stOpenSquare:
		p.attribute()
	case stDollar, stCaret, stEquals, stWord:
		p.word()
	case stColon:
		p.pseudo()
	case stComma:
		p.comma()
	case stAsterisk:
		p.universal()
	case stAmpersand:
		p.nesting()
	case stSlash, stCombinator:
		p.combinator()
	case stStr:
		p.node('n')
		p.pos++
	default: // ] ; ! and anything else
		p.fail()
	}
}

func (p *selParser) space() {
	prev := p.tok(p.pos - 1)
	switch next := p.next(); {
	case p.pos == 0 || prev.kind == stComma || prev.kind == stOpenParen || !p.cur().nonComment:
		p.pos++
	case p.pos == len(p.toks)-1 || next.kind == stComma || next.kind == stCloseParen:
		p.pos++
	default:
		p.combinator()
	}
}

func (p *selParser) nextMeaningful(from int) int {
	for i := from; i < len(p.toks); i++ {
		if k := p.toks[i].kind; k != stSpace && k != stComment {
			return i
		}
	}
	return -1
}

func (p *selParser) namedCombinatorAt(i int) bool {
	a, b, c := p.tok(i), p.tok(i+1), p.tok(i+2)
	return a != nil && a.kind == stSlash && b != nil && b.kind == stWord && c != nil && c.kind == stSlash
}

func (p *selParser) combinator() {
	if p.content(p.curTok()) == "|" {
		p.namespace()
		return
	}
	sig := p.nextMeaningful(p.pos)
	if sig < 0 || p.toks[sig].kind == stComma || p.toks[sig].kind == stCloseParen {
		// Trailing whitespace and comments, folded into the last node.
		if sig < 0 {
			sig = len(p.toks)
		}
		p.pos = max(p.pos+1, sig)
		return
	}
	spaced := sig > p.pos
	p.pos = max(p.pos, sig)
	switch t := p.curTok(); {
	case p.namedCombinatorAt(p.pos):
		p.pos += 3
	case t.kind == stCombinator:
		p.pos++
	case t.kind == stSpace:
	case !spaced:
		p.fail()
	}
	if t := p.curTok(); t != nil && t.kind == stSpace {
		p.pos++
	}
	p.node('n')
}

func (p *selParser) comma() {
	if p.pos == len(p.toks)-1 {
		p.pos++
		return
	}
	*p.cur() = selScope{}
	p.pos++
}

func (p *selParser) namespace() {
	next := p.next()
	switch {
	case next == nil:
		p.fail()
	case next.kind == stWord:
		p.pos++
		p.word()
	case next.kind == stAsterisk:
		p.pos++
		p.universal()
	default:
		p.fail()
	}
}

func (p *selParser) word() {
	if next := p.next(); next != nil && p.content(next) == "|" {
		p.pos++
		p.namespace()
		return
	}
	p.splitWord(false)
}

func (p *selParser) universal() {
	if next := p.next(); next != nil && p.content(next) == "|" {
		p.pos++
		p.namespace()
		return
	}
	p.node('n')
	p.pos++
}

func (p *selParser) nesting() {
	if next := p.next(); next != nil && p.content(next) == "|" {
		p.pos++
		return
	}
	p.node('n')
	p.pos++
}

var keyframesPercent = regexp.MustCompile(`^\d+\.\d+%$`)

// splitWord joins a word with the $ ^ = and word tokens after it and splits it
// at each class and id. For a pseudo-class, a split name followed by "("
// fails as a misplaced parenthesis.
func (p *selParser) splitWord(pseudo bool) {
	word := p.content(p.curTok())
	for next := p.next(); next != nil && (next.kind == stDollar || next.kind == stCaret || next.kind == stEquals || next.kind == stWord); next = p.next() {
		p.pos++
		cur := p.content(p.curTok())
		word += cur
		if strings.HasSuffix(cur, "\\") {
			if n := p.next(); n != nil && n.kind == stSpace {
				word += p.content(n)
				p.pos++
			}
		}
	}
	parts := map[int]bool{0: true}
	for i := 0; i < len(word); i++ {
		switch {
		case word[i] == '.' && (i == 0 || word[i-1] != '\\') && !keyframesPercent.MatchString(word):
			parts[i] = true
		case word[i] == '#' && (i == 0 || word[i-1] != '\\') && !strings.HasPrefix(word[i:], "#{"):
			parts[i] = true
		}
	}
	if pseudo {
		p.node('p')
		if next := p.next(); len(parts) > 1 && next != nil && next.kind == stOpenParen {
			p.fail()
		}
	}
	if !pseudo || len(parts) > 1 {
		p.node('n')
	}
	p.pos++
}

func (p *selParser) pseudo() {
	for t := p.curTok(); t != nil && t.kind == stColon; t = p.curTok() {
		p.pos++
	}
	if t := p.curTok(); t == nil || t.kind != stWord {
		p.fail()
	}
	p.splitWord(true)
}

func (p *selParser) parentheses() {
	unbalanced := 1
	p.pos++
	if p.cur().last == 'p' {
		p.push()
		p.depth++
		if p.depth > 256 {
			p.fail()
		}
		for p.pos < len(p.toks) && unbalanced > 0 {
			switch p.curTok().kind {
			case stOpenParen:
				unbalanced++
			case stCloseParen:
				unbalanced--
			}
			if unbalanced > 0 {
				p.parse(false)
			} else {
				p.pos++
			}
		}
		p.depth--
		p.stack = p.stack[:len(p.stack)-1]
	} else {
		for p.pos < len(p.toks) && unbalanced > 0 {
			switch p.curTok().kind {
			case stOpenParen:
				unbalanced++
			case stCloseParen:
				unbalanced--
			}
			p.pos++
		}
		if p.cur().last == 0 {
			p.node('n')
		}
	}
	if unbalanced > 0 {
		p.fail()
	}
}

// attribute ports Parser#attribute's checks, and the TypeErrors it raises
// reading past the last token, which postcss-nesting catches all the same.
func (p *selParser) attribute() {
	start := p.pos
	p.pos++
	for p.pos < len(p.toks) && p.curTok().kind != stCloseSquare {
		p.pos++
	}
	if p.pos >= len(p.toks) {
		p.fail()
	}
	attr := p.toks[start+1 : p.pos]
	if len(attr) == 1 && attr[0].kind != stWord {
		p.fail()
	}
	var (
		namespace, attribute, operator bool
		value                          *string
		quoted, spaceAfter             bool
		lastAdded                      string
	)
	nextKind := func(i int) selTokKind {
		if i+1 >= len(attr) {
			p.fail() // next[TYPE] on undefined
		}
		return attr[i+1].kind
	}
	for i := 0; i < len(attr); i++ {
		t := attr[i]
		content := p.content(&attr[i])
		switch t.kind {
		case stSpace:
			spaceAfter = true
			continue
		case stAsterisk:
			if nextKind(i) == stEquals {
				operator, lastAdded = true, "operator"
			} else if (!namespace || lastAdded == "namespace" && !spaceAfter) && i+1 < len(attr) {
				namespace, lastAdded = true, "namespace"
			}
		case stDollar, stCaret:
			if t.kind == stDollar && lastAdded == "value" {
				*value += "$"
				continue
			}
			if nextKind(i) == stEquals {
				operator, lastAdded = true, "operator"
			}
		case stCombinator:
			if content == "~" && nextKind(i) == stEquals {
				operator, lastAdded = true, "operator"
			}
			if content == "|" {
				if nextKind(i) == stEquals {
					operator, lastAdded = true, "operator"
				} else if !namespace && !attribute {
					namespace = true
				}
			}
		case stWord:
			switch {
			case i+1 < len(attr) && p.content(&attr[i+1]) == "|" && i+2 < len(attr) && attr[i+2].kind != stEquals && !operator && !namespace:
				namespace, lastAdded = true, "namespace"
			case !attribute || lastAdded == "attribute" && !spaceAfter:
				attribute, lastAdded = true, "attribute"
			case value == nil || lastAdded == "value" && !(spaceAfter || quoted):
				v := content
				if value != nil {
					v = *value + content
				}
				value, quoted, lastAdded = &v, false, "value"
			case value != nil && (quoted || spaceAfter):
				lastAdded = "insensitive"
			case value != nil:
				*value += content
				lastAdded = "value"
			}
		case stStr:
			if !attribute || !operator {
				p.fail()
			}
			v := content
			value, quoted, lastAdded = &v, true, "value"
		case stEquals:
			if !attribute || value != nil && *value != "" {
				p.fail()
			}
			operator, lastAdded = true, "operator"
		case stComment:
			continue
		default:
			p.fail()
		}
		spaceAfter = false
	}
	p.node('n')
	p.pos++
}
