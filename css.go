package juicer

import (
	"bytes"
	"slices"
	"strings"

	"github.com/mailproto/go-juicer/internal/postcss"
)

// decl is one declaration from a stylesheet.
type decl struct {
	prop      string
	value     []byte
	important bool
	bang      []byte // value + " !important", for elements that keep it
	ord       uint32 // position in the stylesheet; breaks specificity ties
}

// preserved is a rule that cannot be inlined and is re-emitted into a
// surviving <style> element.
type preserved struct {
	kind string // "media", "font-face", "keyframes", "pseudo", ...
	text []byte
}

const cssSpaces = " \t\n\r\f"

// matchesPreserved is juice's matchesPreservedSelector over every arm.
func matchesPreserved(arms [][]byte, patterns []string) bool {
	return slices.ContainsFunc(arms, func(a []byte) bool {
		return slices.ContainsFunc(patterns, func(p string) bool { return bytes.Contains(a, []byte(p)) })
	})
}

// ownSemicolon is a semicolon following a rule, which postcss keeps with the
// rule and re-emits after its closing brace, along with the space before it.
func ownSemicolon(rest []byte) []byte {
	i := 0
	for i < len(rest) && isCSSSpace(rest[i]) {
		i++
	}
	if i < len(rest) && rest[i] == ';' {
		return rest[:i+1]
	}
	return nil
}

// atRuleKind is the at-rule's name as written: juice compares names
// case-sensitively, so "@MEDIA" is not preserved as a media query.
func atRuleKind(prelude []byte) string {
	p := bytes.TrimLeft(prelude, "@")
	k := string(p[:identEnd(p, 0)])
	// juice matches /^(-\w+-)?keyframes$/.
	if strings.HasPrefix(k, "-") {
		if i := strings.IndexByte(k[1:], '-'); i > 0 && k[i+2:] == "keyframes" {
			k = "keyframes"
		}
	}
	return k
}

func preserveKind(kind string, o *options) bool {
	if o.keepAll {
		return true
	}
	switch kind {
	case "media":
		return o.preserveMediaQueries
	case "font-face":
		return o.preserveFontFaces
	case "keyframes":
		return o.preserveKeyFrames
	case "container":
		return o.preserveContainerQueries
	case "layer":
		return o.preserveLayers
	}
	return false
}

// quotes finds where strings end. A quote that never closes means no later
// quote of its kind does either, so it remembers that to stay linear.
type quotes [2]int

// end is the index of the quote closing the string opened at i. When none
// does, postcss ends the string after the next character and reads on.
func (q *quotes) end(src []byte, i int) int {
	k := 0
	if src[i] == '"' {
		k = 1
	}
	if q[k] == 0 || i < q[k]-1 {
		for j := i + 1; j < len(src); j++ {
			switch src[j] {
			case '\\':
				j++
			case src[i]:
				return j
			}
		}
		q[k] = i + 1
	}
	return min(i+1, len(src)-1)
}

// importantSuffix reports where a trailing !important begins, or -1.
func importantSuffix(v []byte) int {
	const kw = "important"
	end := len(v)
	for end > 0 && isCSSSpace(v[end-1]) {
		end--
	}
	if end < len(kw) || !bytes.EqualFold(v[end-len(kw):end], []byte(kw)) {
		return -1
	}
	// CSS allows whitespace between the bang and the keyword.
	end -= len(kw)
	for end > 0 && isCSSSpace(v[end-1]) {
		end--
	}
	if end == 0 || v[end-1] != '!' {
		return -1
	}
	return end - 1
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func trimCSS(b []byte) []byte {
	for len(b) > 0 && (isCSSSpace(b[0]) || b[0] == ';') {
		b = b[1:]
	}
	for len(b) > 0 && (isCSSSpace(b[len(b)-1]) || b[len(b)-1] == ';') {
		b = b[:len(b)-1]
	}
	return b
}

// parseStylesheet reads a stylesheet as juice does, through postcss: juice's
// parseCSS for the rules, and its getPreservedText for what stays behind.
func parseStylesheet(src []byte, opts *options, ord uint32) ([]rule, []preserved, uint32) {
	flat := flattenNesting(src)
	text := string(flat)
	if opts.fixesJuiceBugs() {
		if b := blankCommentMarkers(text); b != text {
			text, flat = b, []byte(b)
		}
	}
	root := postcss.Parse(text)
	var rules []rule
	var keep []preserved
	for _, n := range root.Nodes {
		switch n.Type {
		case "atrule":
			if kind := atRuleKind([]byte("@" + n.Name)); preserveKind(kind, opts) {
				keep = append(keep, preserved{kind, []byte(postcss.Stringify(n))})
			}
		case "rule":
			decls := make([]decl, 0, len(n.Nodes))
			for _, c := range n.Nodes {
				if c.Type != "decl" || strings.TrimSpace(c.Value) == "" {
					continue
				}
				v := c.ValueBytes(flat)
				d := decl{prop: c.Prop, value: v, important: c.Important, ord: ord}
				if c.Important {
					d.bang = append(v[:len(v):len(v)], " !important"...)
				}
				decls = append(decls, d)
				ord++
			}
			sels := n.SelectorBytes(flat)
			if len(decls) > 0 {
				for _, sel := range sels {
					armRules, ignored := compileRule(sel, decls, opts)
					if ignored {
						continue
					}
					for k := range armRules {
						armRules[k].group = int32(len(rules))
					}
					rules = append(rules, armRules...)
				}
			}
			if opts.keepAll || opts.preservePseudos && hasIgnoredPseudo(sels[0]) || matchesPreserved(sels, opts.preservedSelectors) {
				keep = append(keep, preserved{"pseudo", []byte(postcss.Stringify(n))})
			}
		default:
			if opts.keepAll && n.Type == "comment" {
				keep = append(keep, preserved{"comment", []byte(postcss.Stringify(n))})
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
