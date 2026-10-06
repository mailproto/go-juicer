package juicer

import (
	"bytes"
	"slices"
	"strings"
)

// flattenNesting rewrites CSS nesting into flat rules the way juice does,
// through postcss-nesting (2024-02 edition), before anything else reads the
// stylesheet. Rules with nothing nested are re-emitted as written.
func flattenNesting(src []byte) []byte {
	if !hasNesting(src) {
		return src
	}
	nodes, _ := parseCSSNodes(src, 0, false)
	var b bytes.Buffer
	b.Grow(len(src))
	writeCSSNodes(&b, flattenContainer(nodes, false), true)
	return b.Bytes()
}

// hasNesting reports a block opened inside a style rule, or a top-level
// selector using "&", without allocating.
func hasNesting(src []byte) bool {
	var buf [16]bool
	stack := buf[:0] // true for a style rule's block
	stmt, amp := 0, false
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\\':
			i++
		case c == '"' || c == '\'':
			quote = c
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			if k := bytes.Index(src[i+2:], []byte("*/")); k >= 0 {
				i += k + 3
			} else {
				i = len(src)
			}
		case c == '&':
			amp = true
		case c == '@':
			if len(stack) > 0 && stack[len(stack)-1] && len(trimCSS(src[stmt:i])) == 0 {
				return true
			}
		case c == '{':
			if len(stack) > 0 && stack[len(stack)-1] {
				return true
			}
			isRule := !bytes.HasPrefix(trimCSS(src[stmt:i]), []byte("@"))
			if isRule && amp {
				return true
			}
			stack = append(stack, isRule)
			stmt, amp = i+1, false
		case c == ';':
			stmt, amp = i+1, false
		case c == '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			stmt, amp = i+1, false
		}
	}
	return false
}

// cssNode is a postcss node, as much of one as flattening needs.
type cssNode struct {
	kind  byte   // 'r' rule, 'a' at-rule, 'd' declaration, 'c' comment
	head  string // selector or at-rule prelude
	name  string // at-rule name
	raw   []byte // source text, re-emitted while nothing inside changes
	kids  []*cssNode
	block bool
	semi  []byte // see ownSemicolon; copies of a rule inherit it
}

func parseCSSNodes(src []byte, i int, inBlock bool) ([]*cssNode, int) {
	var nodes []*cssNode
	for {
		for i < len(src) && (isCSSSpace(src[i]) || src[i] == ';') {
			i++
		}
		if i >= len(src) {
			return nodes, i
		}
		if src[i] == '}' {
			i++
			if inBlock {
				return nodes, i
			}
			continue
		}
		if bytes.HasPrefix(src[i:], []byte("/*")) {
			end := len(src)
			if k := bytes.Index(src[i+2:], []byte("*/")); k >= 0 {
				end = i + k + 4
			}
			nodes = append(nodes, &cssNode{kind: 'c', raw: src[i:end]})
			i = end
			continue
		}
		j := statementEnd(src, i)
		head := trimCSS(src[i:j])
		n := &cssNode{kind: 'd', head: string(head)}
		if len(head) > 0 && head[0] == '@' {
			n.kind = 'a'
			k := 1
			for k < len(head) && !isCSSSpace(head[k]) && head[k] != '(' {
				k++
			}
			n.name = string(head[1:k])
		}
		if j < len(src) && src[j] == '{' {
			if n.kind == 'd' {
				n.kind = 'r'
			}
			n.block = true
			var end int
			n.kids, end = parseCSSNodes(src, j+1, true)
			n.raw = src[i:end]
			if n.kind == 'r' {
				n.semi = ownSemicolon(src[end:])
			}
			i = end + len(n.semi)
		} else {
			closed := j < len(src) && src[j] == ';'
			if closed {
				j++
			}
			n.raw = src[i:j]
			i = j
			if n.kind == 'd' && !closed {
				// Comments after the last value in a block are nodes of their
				// own to postcss.
				var after []*cssNode
				for {
					t := bytes.TrimRight(n.raw, cssSpaces)
					k := bytes.LastIndex(t, []byte("/*"))
					if !bytes.HasSuffix(t, []byte("*/")) || k < 0 || len(trimCSS(t[:k])) == 0 {
						break
					}
					after = append([]*cssNode{{kind: 'c', raw: t[k:]}}, after...)
					n.raw = t[:k]
				}
				nodes = append(append(nodes, n), after...)
				continue
			}
		}
		nodes = append(nodes, n)
	}
}

// statementEnd finds the "{", ";" or "}" that ends the statement at i.
func statementEnd(src []byte, i int) int {
	depth := 0
	var quote byte
	for ; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\\':
			i++
		case c == '"' || c == '\'':
			quote = c
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			if k := bytes.Index(src[i+2:], []byte("*/")); k >= 0 {
				i += k + 3
			} else {
				return len(src)
			}
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth <= 0 && (c == '{' || c == ';' || c == '}'):
			return i
		}
	}
	return len(src)
}

func writeCSSNodes(b *bytes.Buffer, nodes []*cssNode, top bool) {
	for i, n := range nodes {
		switch {
		case n.kind == 'd' && i < len(nodes)-1 && !bytes.HasSuffix(n.raw, []byte(";")):
			// It ended its block; now something follows. postcss keeps a
			// custom property's trailing space as part of its value.
			if bytes.HasPrefix(n.raw, []byte("--")) {
				b.Write(n.raw)
			} else {
				b.Write(bytes.TrimRight(n.raw, cssSpaces))
			}
			b.WriteByte(';')
		case n.raw != nil:
			b.Write(n.raw)
			if n.kind == 'a' && !n.block && !bytes.HasSuffix(n.raw, []byte(";")) {
				b.WriteByte(';')
			}
		default:
			b.WriteString(n.head)
			b.WriteByte('{')
			writeCSSNodes(b, n.kids, false)
			b.WriteByte('}')
		}
		b.Write(n.semi)
		if top {
			b.WriteByte('\n')
		}
	}
}

// hoisted lists the at-rules postcss-nesting lifts out of a style rule.
var hoisted = []string{"container", "document", "media", "supports", "layer", "starting-style"}

// flattenContainer flattens the rules in a stylesheet or at-rule block.
func flattenContainer(nodes []*cssNode, inRule bool) []*cssNode {
	var out []*cssNode
	for _, n := range nodes {
		switch {
		case n.kind == 'r' && slices.ContainsFunc(n.kids, func(k *cssNode) bool { return k.kind == 'r' || k.kind == 'a' }):
			out = append(out, flattenRule(n)...)
		case n.kind == 'a' && n.block:
			kids := flattenContainer(n.kids, inRule || n.name == "scope")
			if len(kids) != len(n.kids) || slices.ContainsFunc(kids, func(k *cssNode) bool { return k.raw == nil }) {
				n.kids, n.raw = kids, nil
			}
			out = append(out, n)
		default:
			out = append(out, n)
		}
	}
	if inRule {
		return out
	}
	// A rule outside any other with "&" left in its selector is scoped to the
	// document root.
	for _, n := range out {
		if n.kind == 'r' && selectorHasNesting(n.head) {
			n.head, n.raw = resolveNested(n.head, ":scope"), nil
		}
	}
	return out
}

func flattenRule(t *cssNode) []*cssNode {
	var out, pending []*cssNode
	// postcss-nesting moves each nested node out before its parent, and the
	// parent's earlier children into a copy ahead of that.
	flush := func(kids []*cssNode) {
		if len(kids) == 0 {
			return
		}
		if !slices.ContainsFunc(kids, func(k *cssNode) bool { return k.kind != 'c' }) {
			out = append(out, kids...)
			return
		}
		out = append(out, &cssNode{kind: 'r', head: t.head, kids: kids, block: true, semi: t.semi})
	}
	kids := t.kids
	moved := false
	for i := 0; i < len(kids); i++ {
		c := kids[i]
		switch {
		case c.kind == 'r' && !strings.Contains(c.head, "|"):
			flush(pending)
			pending, moved = nil, true
			c.head, c.raw = resolveNested(c.head, t.head), nil
			if c.head == t.head {
				// Same selector: the parent's remaining children join it.
				c.kids = append(slices.Clip(c.kids), kids[i+1:]...)
				return append(out, flattenRule(c)...)
			}
			out = append(out, flattenRule(c)...)
		case c.kind == 'a' && slices.Contains(hoisted, c.name):
			flush(pending)
			pending, moved = nil, true
			if c.block {
				c.raw = nil
				c.kids = flattenRule(&cssNode{kind: 'r', head: t.head, kids: c.kids, block: true, semi: t.semi})
			}
			out = append(out, c)
		default:
			if c.kind == 'a' && c.block {
				c.kids, c.raw = flattenContainer(c.kids, true), nil
			}
			pending = append(pending, c)
		}
	}
	if !moved {
		// Nothing moved out, so postcss-nesting leaves the rule in place,
		// empty or not.
		if slices.ContainsFunc(pending, func(k *cssNode) bool { return k.raw == nil }) {
			t.raw = nil
		}
		t.kids = pending
		return append(out, t)
	}
	flush(pending)
	return out
}

// selNode is a postcss-selector-parser node.
type selNode struct {
	kind byte // ' ' combinator, '*', 't' tag, '&', '#', '.', '[', ':' pseudo, 'e' pseudo-element
	text string
	args [][]selNode // a pseudo's selector arguments, when it has parens
	call bool
}

var selOrder = map[byte]int{'*': 0, 't': 1, 'e': 2, '&': 3, '#': 4, '.': 5, '[': 6, ':': 7}

// resolveNested resolves a nested rule's selector against its parent's, as
// @csstools/selector-resolve-nested does.
func resolveNested(child, parent string) string {
	var par [][]selNode
	for _, arm := range splitSelector([]byte(parent)) {
		par = append(par, parseComplex(arm))
	}
	if len(par) == 0 {
		par = [][]selNode{nil}
	}
	var out []string
	for _, arm := range splitSelector([]byte(child)) {
		c := parseComplex(arm)
		if !nodesHaveNesting(c) {
			c = append([]selNode{{kind: '&'}, {kind: ' ', text: " "}}, c...)
		} else if len(c) > 0 && c[0].kind == ' ' {
			c = append([]selNode{{kind: '&'}}, c...)
		}
		out = append(out, writeSelector(replaceNesting(c, par, false)))
	}
	return strings.Join(out, ",")
}

func selectorHasNesting(sel string) bool {
	for _, arm := range splitSelector([]byte(sel)) {
		if nodesHaveNesting(parseComplex(arm)) {
			return true
		}
	}
	return false
}

func nodesHaveNesting(nodes []selNode) bool {
	return slices.ContainsFunc(nodes, func(n selNode) bool {
		return n.kind == '&' || slices.ContainsFunc(n.args, nodesHaveNesting)
	})
}

func replaceNesting(nodes []selNode, parent [][]selNode, inHas bool) []selNode {
	var out []selNode
	had := false
	for _, n := range nodes {
		switch {
		case n.kind == '&':
			had = true
			if !inHas && len(parent) == 1 && !slices.ContainsFunc(parent[0], func(p selNode) bool { return p.kind == ' ' || p.kind == 'e' }) {
				out = append(out, parent[0]...)
			} else {
				out = append(out, selNode{kind: ':', text: ":is", args: parent, call: true})
			}
		case n.call:
			args := make([][]selNode, len(n.args))
			for i, a := range n.args {
				args[i] = replaceNesting(a, parent, strings.EqualFold(n.text, ":has"))
			}
			n.args = args
			out = append(out, n)
		default:
			out = append(out, n)
		}
	}
	if had {
		out = sortCompounds(out)
	}
	return out
}

// sortCompounds orders each compound the way postcss-nesting does: type
// selectors first, a repeated type wrapped in :is(), a repeated * dropped.
func sortCompounds(nodes []selNode) []selNode {
	out := make([]selNode, 0, len(nodes))
	seg := 0
	flush := func() {
		slices.SortStableFunc(out[seg:], func(a, b selNode) int { return selOrder[a.kind] - selOrder[b.kind] })
		seg = len(out)
	}
	for _, n := range nodes {
		switch {
		case n.kind == ' ':
			flush()
			out = append(out, n)
			seg = len(out)
			continue
		case n.kind == 'e':
			flush()
		case n.kind == '*' && slices.ContainsFunc(out[seg:], func(p selNode) bool { return p.kind == '*' }):
			continue
		case n.kind == 't' && slices.ContainsFunc(out[seg:], func(p selNode) bool { return p.kind == 't' }):
			n = selNode{kind: ':', text: ":is", args: [][]selNode{{n}}, call: true}
		}
		out = append(out, n)
	}
	flush()
	return out
}

// parseComplex splits one complex selector into postcss-selector-parser's
// nodes. Whitespace is not kept: a resolved selector is re-spaced anyway.
func parseComplex(s []byte) []selNode {
	var out []selNode
	space := false
	for i := 0; i < len(s); {
		c := s[i]
		if isCSSSpace(c) {
			space = true
			i++
			continue
		}
		if c == '>' || c == '+' || c == '~' {
			out = append(out, selNode{kind: ' ', text: string(c)})
			space = false
			i++
			continue
		}
		if space && len(out) > 0 && out[len(out)-1].kind != ' ' {
			out = append(out, selNode{kind: ' ', text: " "})
		}
		space = false
		n := selNode{kind: 't'}
		j := i + 1
		switch c {
		case '&', '*':
			n.kind = c
		case '.', '#':
			n.kind = c
			j = identEnd(s, j)
		case '[':
			n.kind = '['
			j = attrEnd(s, i)
		case ':':
			n.kind = ':'
			if j < len(s) && s[j] == ':' {
				j++
			}
			j = identEnd(s, j)
			name := strings.ToLower(string(s[i:j]))
			if strings.HasPrefix(name, "::") || name == ":before" || name == ":after" || name == ":first-line" || name == ":first-letter" {
				n.kind = 'e'
			}
			if j < len(s) && s[j] == '(' {
				k := parenEnd(s, j)
				n.text, n.call = string(s[i:j]), true
				inner := s[j+1 : k]
				if k > j+1 && s[k-1] == ')' {
					inner = s[j+1 : k-1]
				}
				for _, arm := range splitSelector(inner) {
					n.args = append(n.args, parseComplex(arm))
				}
				out = append(out, n)
				i = k
				continue
			}
		case '"', '\'':
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(s))
		default:
			for j < len(s) && !isCSSSpace(s[j]) && !strings.ContainsRune(">+~&*.#[:(),\"'", rune(s[j])) {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(s))
		}
		n.text = string(s[i:j])
		out = append(out, n)
		i = j
	}
	if len(out) > 0 && out[len(out)-1].kind == ' ' && out[len(out)-1].text == " " {
		out = out[:len(out)-1]
	}
	return out
}

func writeSelector(nodes []selNode) string {
	var b strings.Builder
	for _, n := range nodes {
		switch {
		case n.kind == ' ' && n.text != " ":
			b.WriteString(" " + n.text + " ")
		case n.call:
			b.WriteString(n.text + "(")
			for i, a := range n.args {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(writeSelector(a))
			}
			b.WriteByte(')')
		case n.kind == '&':
			b.WriteByte('&')
		default:
			b.WriteString(n.text)
		}
	}
	return b.String()
}
