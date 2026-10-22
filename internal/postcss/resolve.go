package postcss

import (
	"slices"
	"strings"

	"github.com/mailproto/go-juicer/internal/selector"
)

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
	for _, arm := range selector.Split([]byte(parent)) {
		par = append(par, parseComplex(arm))
	}
	if len(par) == 0 {
		par = [][]selNode{nil}
	}
	var out []string
	for _, arm := range selector.Split([]byte(child)) {
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
	for _, arm := range selector.Split([]byte(sel)) {
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
		if isSpace(c) {
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
			j = selector.IdentEnd(s, j)
		case '[':
			n.kind = '['
			j = selector.AttrEnd(s, i)
		case ':':
			n.kind = ':'
			if j < len(s) && s[j] == ':' {
				j++
			}
			j = selector.IdentEnd(s, j)
			name := strings.ToLower(string(s[i:j]))
			if strings.HasPrefix(name, "::") || name == ":before" || name == ":after" || name == ":first-line" || name == ":first-letter" {
				n.kind = 'e'
			}
			if j < len(s) && s[j] == '(' {
				k := selector.ParenEnd(s, j)
				n.text, n.call = string(s[i:j]), true
				inner := s[j+1 : k]
				if k > j+1 && s[k-1] == ')' {
					inner = s[j+1 : k-1]
				}
				for _, arm := range selector.Split(inner) {
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
			for j < len(s) && !isSpace(s[j]) && !strings.ContainsRune(">+~&*.#[:(),\"'", rune(s[j])) {
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
