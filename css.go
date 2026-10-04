package juicer

import (
	"bytes"
	"slices"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// decl is one declaration from a stylesheet.
type decl struct {
	prop      string
	value     []byte // sliced from the source, so original spacing survives
	text      []byte // value and !important as written, for a preserved block
	important bool
	ord       uint32 // position in the stylesheet; breaks specificity ties
}

// A decl with no prop is a comment, kept so a preserved block can re-emit it.
func appendComments(decls []decl, comments [][]byte) []decl {
	for _, c := range comments {
		decls = append(decls, decl{value: c})
	}
	return decls
}

func (d decl) writeTo(b *bytes.Buffer) {
	if d.prop == "" {
		b.Write(d.value)
		return
	}
	b.WriteString(d.prop)
	b.WriteString(": ")
	b.Write(d.text)
	b.WriteByte(';')
}

// preserved is a rule that cannot be inlined and is re-emitted into a
// surviving <style> element.
type preserved struct {
	kind string // "media", "font-face", "keyframes", "pseudo"
	text []byte
}

// cssBlock is a brace-delimited block kept for re-emission. juice reformats
// preserved at-rules through postcss's stringifier rather than echoing the
// source, so the structure has to be rebuilt to match it byte for byte.
type cssBlock struct {
	prelude []byte
	decls   []decl
	blocks  []*cssBlock
}

// text renders a block with juice's formatting: two spaces per level, one
// declaration per line, every declaration semicolon-terminated. Preludes and
// values are kept exactly as written.
func (b *cssBlock) text(indent int) []byte {
	var out bytes.Buffer
	pad := bytes.Repeat([]byte(" "), indent)
	out.Write(pad)
	out.Write(b.prelude)
	out.WriteString(" {")
	for _, d := range b.decls {
		out.WriteByte('\n')
		out.Write(pad)
		out.WriteString("  ")
		d.writeTo(&out)
	}
	for _, c := range b.blocks {
		out.WriteByte('\n')
		out.Write(c.text(indent + 2))
	}
	out.WriteByte('\n')
	out.Write(pad)
	out.WriteByte('}')
	return out.Bytes()
}

// parseStylesheet splits src into inlinable rules and at-rules to preserve.
//
// At-rules never become rules: that is how juice excludes @media, @font-face
// and @keyframes from inlining, rather than by filtering them later.
//
// Values are sliced out of src rather than rebuilt from tokens, because
// tdewolff normalizes whitespace inside a value ("rgba(0 , 0,0,.5)" becomes
// "rgba(0,0,0,.5)") and juice emits it exactly as written.
func parseStylesheet(src []byte, opts *options, ord uint32) ([]rule, []preserved, uint32) {
	var rules []rule
	var keep []preserved

	p := css.NewParser(parse.NewInputBytes(src), false)
	prev := 0
	atKind := ""

	// stack is non-empty while inside an at-rule, so nested rulesets and
	// declarations accumulate into the block being preserved.
	var stack []*cssBlock

	var sel []byte
	var decls []decl

	for {
		gt, _, data := p.Next()
		if gt == css.ErrorGrammar {
			break
		}
		start, end := prev, p.Offset()
		prev = end

		switch gt {
		case css.BeginAtRuleGrammar:
			if len(stack) == 0 {
				atKind = atRuleKind(data)
			}
			stack = append(stack, &cssBlock{prelude: preludeText(src, start, end)})

		case css.AtRuleGrammar:
			// A bodyless at-rule such as `@import url(x);` or `@layer a;`.
			// The terminating semicolon is part of how it is re-emitted.
			if len(stack) == 0 && preserveKind(atRuleKind(data), opts) {
				text := append(append([]byte{}, trimCSS(src[start:end])...), ';')
				keep = append(keep, preserved{atRuleKind(data), text})
			}

		case css.EndAtRuleGrammar:
			done := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) > 0 {
				stack[len(stack)-1].blocks = append(stack[len(stack)-1].blocks, done)
			} else if preserveKind(atKind, opts) {
				keep = append(keep, preserved{atKind, done.text(0)})
			}

		case css.BeginRulesetGrammar:
			sel = selectorText(src, start, end)
			decls = decls[:0]
			if len(stack) > 0 {
				stack = append(stack, &cssBlock{prelude: sel})
			}

		case css.DeclarationGrammar:
			d := declParts(src[start:end], false)
			v := d.value
			if len(stack) > 0 {
				b := stack[len(stack)-1]
				b.decls = appendComments(b.decls, d.before)
				if len(v) > 0 {
					b.decls = append(b.decls, decl{prop: d.name, value: v, text: d.text})
				}
				b.decls = appendComments(b.decls, d.after)
				continue
			}
			decls = appendComments(decls, d.before)
			// juice drops empty values, preserving mensch behaviour.
			if len(v) > 0 {
				// Scored as important either way; only the text may be dropped.
				if d.important && opts.preserveImportant {
					v = append(append([]byte{}, v...), " !important"...)
				}
				decls = append(decls, decl{prop: d.name, value: v, text: d.text, important: d.important, ord: ord})
				ord++
			}
			decls = appendComments(decls, d.after)

		case css.CustomPropertyGrammar:
			d := declParts(src[start:end], true)
			if len(stack) > 0 {
				b := stack[len(stack)-1]
				b.decls = appendComments(b.decls, d.before)
				b.decls = append(b.decls, decl{prop: d.name, value: d.value, text: d.text})
				continue
			}
			decls = appendComments(decls, d.before)
			decls = append(decls, decl{prop: d.name, value: d.value, text: d.text, important: d.important, ord: ord})
			ord++

		case css.EndRulesetGrammar:
			// Comments after the last declaration arrive with the closing brace.
			comments, _ := leadingComments(src[start:end])
			if len(stack) > 0 {
				b := stack[len(stack)-1]
				b.decls = appendComments(b.decls, comments)
				// A ruleset nested in an at-rule is preserved, never inlined.
				if len(stack) > 1 {
					done := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					stack[len(stack)-1].blocks = append(stack[len(stack)-1].blocks, done)
				}
				continue
			}
			decls = appendComments(decls, comments)
			// An empty rule inlines nothing, but juice still preserves it when
			// it has a pseudo-class arm.
			empty := !slices.ContainsFunc(decls, func(d decl) bool { return d.prop != "" })
			shared := append([]decl(nil), decls...)
			arms := splitSelector(sel)
			for _, arm := range arms {
				armRules, ignored := compileRule(arm, shared)
				if !ignored && !empty {
					for k := range armRules {
						armRules[k].group = int32(len(rules))
					}
					rules = append(rules, armRules...)
				}
			}
			// juice preserves the rule as written, selector list and all, so
			// a:hover keeps its :hover arm in <style> while the plain arm is
			// still inlined. It only looks at the first arm, though, so
			// "td, a:hover" is not preserved at all.
			if opts.preservePseudos && len(arms) > 0 && hasIgnoredPseudo(arms[0]) {
				keep = append(keep, preserved{"pseudo", ruleText(sel, shared)})
			}
		}
	}
	return rules, keep, ord
}

// ruleText re-emits a rule that could not be inlined, formatted the way
// juice's stringifier does: two-space indent, one declaration per line.
func ruleText(sel []byte, decls []decl) []byte {
	var b bytes.Buffer
	b.Write(sel)
	b.WriteString(" {")
	for _, d := range decls {
		b.WriteString("\n  ")
		d.writeTo(&b)
	}
	b.WriteString("\n}")
	return b.Bytes()
}

// preludeText recovers an at-rule prelude, which runs up to the opening brace.
func preludeText(src []byte, start, end int) []byte {
	s := src[start:end]
	if i := bytes.LastIndexByte(s, '{'); i >= 0 {
		s = s[:i]
	}
	return trimCSS(s)
}

func atRuleKind(data []byte) string {
	k := string(bytes.TrimLeft(bytes.ToLower(data), "@"))
	if i := bytes.IndexByte([]byte(k), ' '); i >= 0 {
		k = k[:i]
	}
	// Vendor-prefixed keyframes still count as keyframes.
	for _, p := range []string{"-webkit-", "-moz-", "-ms-", "-o-"} {
		if len(k) > len(p) && k[:len(p)] == p {
			k = k[len(p):]
		}
	}
	return k
}

func preserveKind(kind string, o *options) bool {
	switch kind {
	case "media":
		return o.preserveMediaQueries
	case "font-face":
		return o.preserveFontFaces
	case "keyframes":
		return o.preserveKeyFrames
	}
	return false
}

// selectorText recovers the selector as written. The span runs to just past
// the opening brace.
func selectorText(src []byte, start, end int) []byte {
	s := src[start:end]
	if i := bytes.LastIndexByte(s, '{'); i >= 0 {
		s = s[:i]
	}
	return trimCSS(s)
}

// declParts splits a declaration span into its property name and value. The
// name is sliced from the source rather than taken from the parser, which
// lowercases it; juice keys styleProps by the name as written, so a stylesheet
// declaring "A: red" emits "A: red".
//
// Comments ahead of the property, and after a value closed by "}", are nodes
// of their own to postcss; they are returned separately so a preserved block
// can re-emit them.
func declParts(span []byte, custom bool) (d parsedDecl) {
	d.before, span = leadingComments(span)
	i := bytes.IndexByte(span, ':')
	if i < 0 {
		return d
	}
	n := 0
	for n < i && !isCSSSpace(span[n]) && !bytes.HasPrefix(span[n:], []byte("/*")) {
		n++
	}
	v := span[i+1:]
	// The last declaration of a ruleset runs up to and past the closing brace.
	closed := false
	if j := bytes.LastIndexByte(v, '}'); j >= 0 {
		v, closed = v[:j], true
	} else if t := bytes.TrimRight(v, " \t\n\r\f"); len(t) > 0 && t[len(t)-1] == ';' {
		v = t[:len(t)-1]
	} else {
		closed = true
	}
	d.name = string(span[:n])
	if custom {
		// postcss keeps a custom property's trailing whitespace however it ends.
		d.value, d.text = trimCSS(v), bytes.TrimLeft(v, cssSpaces)
		return d
	}
	d.value, d.important, d.after = postcssValue(v, closed)
	d.text = declText(v, closed)
	return d
}

type parsedDecl struct {
	before, after [][]byte
	name          string
	value         []byte
	text          []byte
	important     bool
}

const cssSpaces = " \t\n\r\f"

// declText is a value as postcss re-emits it in a preserved block: its raw
// value and raw !important rejoined, which is the source text less leading
// whitespace and comments, and less trailing ones when "}" closed it.
func declText(v []byte, closed bool) []byte {
	for {
		v = bytes.TrimLeft(v, cssSpaces)
		if !bytes.HasPrefix(v, []byte("/*")) {
			break
		}
		k := bytes.Index(v[2:], []byte("*/"))
		if k < 0 {
			return nil
		}
		v = v[k+4:]
	}
	for closed {
		v = bytes.TrimRight(v, cssSpaces)
		k := bytes.LastIndex(v, []byte("/*"))
		if k < 0 || !bytes.HasSuffix(v, []byte("*/")) {
			break
		}
		v = v[:k]
	}
	return v
}

func leadingComments(span []byte) ([][]byte, []byte) {
	var out [][]byte
	for {
		span = trimCSSLeft(span)
		if !bytes.HasPrefix(span, []byte("/*")) {
			return out, span
		}
		end := len(span)
		if k := bytes.Index(span[2:], []byte("*/")); k >= 0 {
			end = k + 4
		}
		out = append(out, span[:end])
		span = span[end:]
	}
}

func trimCSSLeft(b []byte) []byte {
	for len(b) > 0 && (isCSSSpace(b[0]) || b[0] == ';') {
		b = b[1:]
	}
	return b
}

// cssToken is a postcss token, reduced to the kinds comment handling tells
// apart.
type cssToken struct {
	kind byte // ' ' whitespace, '/' comment, 'w' word, 'o' anything else
	text []byte
}

// tokenizeValue splits a declaration value along postcss's token boundaries.
func tokenizeValue(v []byte) []cssToken {
	var toks []cssToken
	var lastWord []byte // postcss tests the most recent word for url(
	for i := 0; i < len(v); {
		c, j, kind := v[i], i+1, byte('o')
		switch {
		case isCSSSpace(c):
			for j < len(v) && isCSSSpace(v[j]) {
				j++
			}
			kind = ' '
		case c == '/' && j < len(v) && v[j] == '*':
			j = len(v)
			if k := bytes.Index(v[i+2:], []byte("*/")); k >= 0 {
				j = i + 2 + k + 2
			}
			kind = '/'
		case c == '"' || c == '\'':
			for j < len(v) && v[j] != c {
				if v[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(v))
		case c == '(':
			prev := lastWord
			lastWord = nil
			// An unquoted url() is a single token, comments and all.
			if string(prev) == "url" && j < len(v) && v[j] != '"' && v[j] != '\'' && !isCSSSpace(v[j]) {
				for j < len(v) && v[j] != ')' {
					if v[j] == '\\' {
						j++
					}
					j++
				}
				j = min(j+1, len(v))
			}
		case bytes.IndexByte([]byte(")[]{}:;"), c) >= 0:
		default:
			for j < len(v) && !isWordEnd(v, j) {
				j++
			}
			kind = 'w'
			lastWord = v[i:j]
		}
		toks = append(toks, cssToken{kind, v[i:j]})
		i = j
	}
	return toks
}

func isWordEnd(v []byte, i int) bool {
	switch v[i] {
	case '\t', '\n', '\f', '\r', ' ', '!', '"', '#', '\'', '(', ')', ':', ';', '@', '[', '\\', ']', '{', '}':
		return true
	}
	return v[i] == '/' && i+1 < len(v) && v[i+1] == '*'
}

// postcssValue reproduces how postcss reads a declaration value: what it
// strips as !important, and which comments it drops. closed means the
// declaration ended at "}" or end of input rather than at ";", in which case
// postcss hands trailing whitespace and comments back to the rule.
func postcssValue(v []byte, closed bool) (value []byte, important bool, after [][]byte) {
	if !bytes.Contains(v, []byte("/*")) {
		// Without comments postcss's rules reduce to a trim.
		v = trimCSS(v)
		if j := importantSuffix(v); j >= 0 {
			return trimCSS(v[:j]), true, nil
		}
		return v, false, nil
	}
	toks := tokenizeValue(v)
	blank := func(t cssToken) bool { return t.kind == ' ' || t.kind == '/' }
	if closed {
		end := len(toks)
		for end > 0 && blank(toks[end-1]) {
			end--
		}
		for _, t := range toks[end:] {
			if t.kind == '/' {
				after = append(after, t.text)
			}
		}
		toks = toks[:end]
	}
	first := 0
	for first < len(toks) && blank(toks[first]) {
		first++
	}
	lead, toks := toks[:first], toks[first:]

	for i := len(toks) - 1; i >= 0; i-- {
		t := toks[i]
		if bytes.EqualFold(t.text, []byte("!important")) {
			important = true
			toks = toks[:i]
			for len(toks) > 0 && toks[len(toks)-1].kind == ' ' {
				toks = toks[:len(toks)-1]
			}
			break
		}
		if bytes.EqualFold(t.text, []byte("important")) {
			// "! important": postcss pops tokens off the end until the text it
			// has collected starts with a bang.
			cache := slices.Clone(toks)
			var str []byte
			bang := func() bool { return bytes.HasPrefix(bytes.TrimSpace(str), []byte("!")) }
			for j := i; j > 0; j-- {
				if bang() && cache[j].kind != ' ' {
					break
				}
				str = append(slices.Clone(cache[len(cache)-1].text), str...)
				cache = cache[:len(cache)-1]
			}
			if bang() {
				important, toks = true, cache
			}
		}
		if !blank(t) {
			break
		}
	}

	if !slices.ContainsFunc(toks, func(t cssToken) bool { return !blank(t) }) {
		toks = append(lead, toks...)
	}
	var out []byte
	for i, t := range toks {
		switch {
		case t.kind == ' ' && i == len(toks)-1:
		case t.kind == '/':
			// A comment survives only with no whitespace on either side, as in
			// "1px/**/solid".
			if i > 0 && toks[i-1].kind != ' ' && i+1 < len(toks) && toks[i+1].kind != ' ' && !bytes.HasSuffix(out, []byte(",")) {
				out = append(out, t.text...)
			}
		default:
			out = append(out, t.text...)
		}
	}
	return out, important, after
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
