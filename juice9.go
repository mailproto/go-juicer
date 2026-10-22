package juicer

import (
	"fmt"
	"strings"

	"github.com/mailproto/go-juicer/internal/mensch"
	"github.com/mailproto/go-juicer/internal/selector"
)

// Where the Juice9 profile reads stylesheets differently from juice 12.

// parseMensch is parseStylesheet for the Juice9 profile: juice 9 reads
// stylesheets with mensch and preserves rules through its stringifier.
func parseMensch(src []byte, opts *options, ord uint32) ([]rule, []preserved, uint32) {
	var rules []rule
	var keep []preserved
	for _, n := range mensch.Parse(mensch.Lex(string(src))) {
		if n.Type != "rule" {
			if preserveKind(n.Type, opts) {
				keep = append(keep, preserved{n.Type, mensch.Stringify(n)})
			}
			continue
		}
		var decls []decl
		var throws bool
		decls, throws, ord = menschDecls(n, ord)
		// An empty rule still counts: juice 9 rewrites the style attribute of
		// every element a rule matches.
		for _, sel := range n.Selectors {
			if sel == "" {
				continue
			}
			armRules, _, _ := compileRule([]byte(sel), decls, opts)
			for k := range armRules {
				armRules[k].group = int32(len(rules))
				armRules[k].throws = throws
			}
			rules = append(rules, armRules...)
		}
		if opts.preservePseudos && selector.Juice9IgnoredPseudo(n.Selectors[0]) {
			keep = append(keep, preserved{"pseudo", mensch.Stringify(n)})
		}
	}
	return rules, keep, ord
}

// menschDecls reads a rule's declarations as juice 9's addProps does. bang
// holds the value as written, which juice 9 keeps when preserving !important.
func menschDecls(n mensch.Node, ord uint32) (decls []decl, throws bool, _ uint32) {
	for _, d := range n.Decls {
		if d.Type != "property" {
			continue
		}
		if d.Value == "" {
			// mensch reads "a{b{" as a declaration b with no value, which
			// juice 9 reads with value.match and throws on.
			throws = true
			continue
		}
		v, important := strings.CutSuffix(d.Value, "!important")
		if important {
			v = strings.TrimRightFunc(v, mensch.JSSpace)
		} else {
			v = d.Value
		}
		name := d.Name
		if name == "" {
			// A word with no colon, which juice 9 writes out as it reads it.
			name = "undefined"
		}
		decls = append(decls, decl{prop: name, value: []byte(v), important: important, bang: []byte(d.Value), ord: ord})
		ord++
	}
	return decls, throws, ord
}

// menschStyle reads a style attribute as juice 9 does, through mensch as the
// body of a "*" rule.
func menschStyle(style []byte) ([]decl, error) {
	for _, n := range mensch.Parse(mensch.Lex("* { " + string(style) + " } ")) {
		if n.Type != "rule" {
			continue
		}
		decls, throws, _ := menschDecls(n, 0)
		if throws {
			break
		}
		return decls, nil
	}
	return nil, fmt.Errorf("%w: juice 9 cannot read the style attribute %q", ErrJuiceFails, style)
}
