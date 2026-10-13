package juicer

import (
	"bytes"
	"slices"
	"strings"
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

// selectorText recovers the selector as written. The span runs to just past

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

// trimValue trims a value as postcss does: whitespace that a string takes in,

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
