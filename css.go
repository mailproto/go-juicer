package juicer

import (
	"bytes"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// decl is one declaration from a stylesheet.
type decl struct {
	prop      string
	value     []byte // sliced from the source, so original spacing survives
	important bool
	ord       uint32 // position in the stylesheet; breaks specificity ties
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
		out.WriteString(d.prop)
		out.WriteString(": ")
		out.Write(d.value)
		out.WriteByte(';')
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
			if len(stack) > 0 {
				name, v, imp := declParts(src[start:end])
				if len(v) > 0 {
					if imp {
						v = append(append([]byte{}, v...), importantText(src[start:end])...)
					}
					b := stack[len(stack)-1]
					b.decls = append(b.decls, decl{prop: name, value: v})
				}
				continue
			}
			name, v, imp := declParts(src[start:end])
			if len(v) == 0 {
				// juice drops empty values, preserving mensch behaviour.
				continue
			}
			if imp && !opts.preserveImportant {
				// Scored as important either way; only the text is dropped.
			} else if imp {
				v = append(append([]byte{}, v...), " !important"...)
			}
			decls = append(decls, decl{prop: name, value: v, important: imp, ord: ord})
			ord++

		case css.CustomPropertyGrammar:
			if len(stack) > 0 {
				continue
			}
			name, v, imp := declParts(src[start:end])
			decls = append(decls, decl{prop: name, value: v, important: imp, ord: ord})
			ord++

		case css.EndRulesetGrammar:
			if len(stack) > 0 {
				// A ruleset nested in an at-rule is preserved, never inlined.
				if len(stack) > 1 {
					done := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					stack[len(stack)-1].blocks = append(stack[len(stack)-1].blocks, done)
				}
				continue
			}
			if len(decls) == 0 {
				continue
			}
			shared := append([]decl(nil), decls...)
			anyIgnored := false
			for _, arm := range splitSelector(sel) {
				armRules, ignored := compileRule(arm, shared)
				if ignored {
					anyIgnored = true
					continue
				}
				rules = append(rules, armRules...)
			}
			// juice preserves the rule as written, selector list and all, so
			// a:hover keeps its :hover arm in <style> while the plain arm is
			// still inlined.
			if anyIgnored && opts.preservePseudos {
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
		b.WriteString(d.prop)
		b.WriteString(": ")
		b.Write(d.value)
		b.WriteByte(';')
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

// importantText recovers the !important suffix as written, since juice keeps
// the author's spacing ("blue!important" stays tight).
func importantText(span []byte) []byte {
	i := bytes.IndexByte(span, ':')
	if i < 0 {
		return nil
	}
	v := span[i+1:]
	if j := bytes.LastIndexByte(v, '}'); j >= 0 {
		v = v[:j]
	}
	v = trimCSS(v)
	if j := importantSuffix(v); j >= 0 {
		return v[j-countTrailingSpace(v[:j]):]
	}
	return nil
}

func countTrailingSpace(b []byte) int {
	n := 0
	for n < len(b) && isCSSSpace(b[len(b)-1-n]) {
		n++
	}
	return n
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
func declParts(span []byte) (name string, value []byte, important bool) {
	i := bytes.IndexByte(span, ':')
	if i < 0 {
		return "", nil, false
	}
	v, imp := declValue(span)
	return string(trimCSS(span[:i])), v, imp
}

// declValue pulls the value out of a declaration span, which may carry a
// leading separator from the previous declaration.
func declValue(span []byte) ([]byte, bool) {
	i := bytes.IndexByte(span, ':')
	if i < 0 {
		return nil, false
	}
	v := span[i+1:]
	// The last declaration of a ruleset runs up to and past the closing
	// brace. Trim it before looking for !important, which would otherwise
	// never be recognised there.
	if j := bytes.LastIndexByte(v, '}'); j >= 0 {
		v = v[:j]
	}
	v = trimCSS(v)
	if j := importantSuffix(v); j >= 0 {
		return trimCSS(v[:j]), true
	}
	return v, false
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
