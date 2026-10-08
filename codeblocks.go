package juicer

import (
	"bytes"
	"slices"
)

// Template expressions are swapped for inert placeholders before parsing and
// restored after serializing, so the HTML parser never sees them. Without
// this, template syntax in tag position is mangled by attribute scanning and
// quotes inside a value get escaped.
//
// juice ships the same mechanism but its default set is Handlebars and EJS
// only. Liquid's statement tag {% ... %} is absent, so juice corrupts
// `<td {% if x %}class="a"{% endif %}>` into `<td {% if x %}class="a" endif %}>`.
// We add it, which is a deliberate divergence from the reference.

// CodeBlock is a pair of template delimiters whose contents are left untouched.
type CodeBlock struct{ Start, End string }

// DefaultCodeBlocks returns a fresh copy of the delimiters protected by default.
func DefaultCodeBlocks() []CodeBlock {
	return []CodeBlock{
		{"{{", "}}"}, // Handlebars, Liquid output, Go templates
		{"{%", "%}"}, // Liquid statements, Jinja, Nunjucks
		{"<%", "%>"}, // EJS, ERB
	}
}

const placeholderPrefix = "juice_code_block_"

// encodeCodeBlocks replaces every template expression with a placeholder made
// only of identifier characters, so it survives tokenizing in text, attribute
// value and tag position alike. The placeholder is lowercase because attribute
// names are lowercased during parsing.
func encodeCodeBlocks(src []byte, blocks []CodeBlock) ([]byte, [][]byte) {
	if len(blocks) == 0 {
		return src, nil
	}
	var found [][]byte
	var out []byte
	i := 0
	// A delimiter pair that is left open once is open for good: no later
	// opening can find a close either. Other pairs still apply.
	open := slices.Clone(blocks)
	for i < len(src) {
		// Earliest opening delimiter wins, so nested-looking syntax is taken
		// in source order rather than delimiter order.
		best, bestBlock := -1, -1
		for k, b := range open {
			if j := bytes.Index(src[i:], []byte(b.Start)); j >= 0 && (best < 0 || j < best) {
				best, bestBlock = j, k
			}
		}
		if best < 0 {
			break
		}
		b := open[bestBlock]
		start := i + best
		// The close comes after the whole opening, so "<%>" is not a block.
		close := bytes.Index(src[start+len(b.Start):], []byte(b.End))
		if close < 0 {
			open = slices.Delete(open, bestBlock, bestBlock+1)
			continue
		}
		end := start + len(b.Start) + close + len(b.End)
		if out == nil {
			out = make([]byte, 0, len(src))
		}
		out = append(out, src[i:start]...)
		out = append(out, placeholder(len(found))...)
		found = append(found, src[start:end])
		i = end
	}
	if out == nil {
		return src, nil
	}
	return append(out, src[i:]...), found
}

func placeholder(n int) []byte {
	b := make([]byte, 0, len(placeholderPrefix)+4)
	b = append(b, placeholderPrefix...)
	b = appendInt(b, n)
	// The trailing underscore stops placeholder 1 matching inside placeholder 10.
	return append(b, '_')
}

func appendInt(b []byte, n int) []byte {
	if n >= 10 {
		b = appendInt(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

// decodeCodeBlocks restores the original template expressions. An empty
// attribute serializes bare, so a placeholder that landed in tag position has
// no ="" suffix to strip, but we tolerate one in case the value was preserved.
func decodeCodeBlocks(out []byte, found [][]byte) []byte {
	if len(found) == 0 {
		return out
	}
	var buf []byte
	i := 0
	for {
		j := bytes.Index(out[i:], []byte(placeholderPrefix))
		if j < 0 {
			break
		}
		start := i + j
		k := start + len(placeholderPrefix)
		n := 0
		digits := 0
		for k < len(out) && out[k] >= '0' && out[k] <= '9' {
			n = n*10 + int(out[k]-'0')
			k++
			digits++
		}
		if digits == 0 || k >= len(out) || out[k] != '_' || n >= len(found) {
			// Not one of ours; skip past the prefix and keep looking.
			i = start + len(placeholderPrefix)
			continue
		}
		k++
		if bytes.HasPrefix(out[k:], []byte(`=""`)) {
			k += 3
		}
		if buf == nil {
			buf = make([]byte, 0, len(out))
		}
		buf = append(buf, out[i:start]...)
		buf = append(buf, found[n]...)
		i = k
	}
	if buf == nil {
		return out
	}
	return append(buf, out[i:]...)
}
