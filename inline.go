// Package juicer inlines CSS into HTML email, matching the behaviour of
// the JavaScript library juice (https://github.com/Automattic/juice) v12.
//
// Output is intended to be byte-identical to juice's, which is why the HTML
// parser and serializer in parse.go deliberately do not follow the HTML5 tree
// construction spec: juice parses with htmlparser2, and matching a mail
// client's view of the message means matching that.
package juicer

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// Inliner holds resolved options and is safe for concurrent use. Reuse one
// across documents rather than constructing per call.
type Inliner struct {
	opts options
}

type options struct {
	extraCSS                    string
	insertExtraCSS              bool
	insertExtraCSSInto          string
	applyStyleTags              bool
	removeStyleTags             bool
	preserveMediaQueries        bool
	preserveFontFaces           bool
	preserveKeyFrames           bool
	preserveContainerQueries    bool
	preserveLayers              bool
	preservePseudos             bool
	preservedSelectors          []string
	preserveImportant           bool
	applyWidthAttributes        bool
	applyHeightAttributes       bool
	applyAttributesTableElement bool
	resolveCSSVariables         bool
	inlinePseudoElements        bool
	styleAttributeName          string
	xmlMode                     bool
	codeBlocks                  []CodeBlock

	// Document cleanup, carried over from the Ruby premailer this package
	// used to wrap. juice has no equivalent.
	removeIDs            bool
	removeClasses        bool
	removeComments       bool
	resetContentEditable bool
}

// defaults mirror juice's getDefaultOptions. Note that preserveImportant and
// inlinePseudoElements are absent from that table and so default to false:
// !important is stripped from the emitted value even though it still wins the
// cascade.
func defaults() options {
	return options{
		insertExtraCSS:              true,
		applyStyleTags:              true,
		removeStyleTags:             true,
		preserveMediaQueries:        true,
		preserveFontFaces:           true,
		preserveKeyFrames:           true,
		preserveContainerQueries:    true,
		preserveLayers:              true,
		preservePseudos:             true,
		applyWidthAttributes:        true,
		applyHeightAttributes:       true,
		applyAttributesTableElement: true,
		resolveCSSVariables:         true,
		styleAttributeName:          "style",
		codeBlocks:                  DefaultCodeBlocks(),
	}
}

// Option configures an Inliner.
type Option func(*options)

// ExtraCSS is appended to the CSS collected from the document.
func ExtraCSS(css string) Option { return func(o *options) { o.extraCSS = css } }

// InsertPreservedExtraCSS controls whether rules from ExtraCSS that cannot be
// inlined, such as @media blocks, are added to the document in a new <style>
// element, appended to <head>, else <body>, else the document. On by default.
func InsertPreservedExtraCSS(v bool) Option {
	return func(o *options) { o.insertExtraCSS, o.insertExtraCSSInto = v, "" }
}

// InsertPreservedExtraCSSInto appends that <style> element to the first
// element matching selector instead. If nothing matches, it is dropped.
func InsertPreservedExtraCSSInto(selector string) Option {
	return func(o *options) { o.insertExtraCSS, o.insertExtraCSSInto = true, selector }
}

// ApplyStyleTags controls whether CSS is collected from <style> elements.
func ApplyStyleTags(v bool) Option { return func(o *options) { o.applyStyleTags = v } }

// RemoveStyleTags controls whether <style> elements are removed after
// inlining. Rules that cannot be inlined are kept regardless.
func RemoveStyleTags(v bool) Option { return func(o *options) { o.removeStyleTags = v } }

// PreserveMediaQueries keeps @media blocks in a surviving <style> element.
func PreserveMediaQueries(v bool) Option { return func(o *options) { o.preserveMediaQueries = v } }

// PreserveFontFaces keeps @font-face blocks in a surviving <style> element.
func PreserveFontFaces(v bool) Option { return func(o *options) { o.preserveFontFaces = v } }

// PreserveKeyFrames keeps @keyframes blocks in a surviving <style> element.
func PreserveKeyFrames(v bool) Option { return func(o *options) { o.preserveKeyFrames = v } }

// PreserveLayers keeps @layer blocks and statements in a surviving <style> element.
func PreserveLayers(v bool) Option { return func(o *options) { o.preserveLayers = v } }

// PreserveContainerQueries keeps @container blocks in a surviving <style> element.
func PreserveContainerQueries(v bool) Option {
	return func(o *options) { o.preserveContainerQueries = v }
}

// PreservePseudos keeps rules using :hover and friends in a surviving <style>.
func PreservePseudos(v bool) Option { return func(o *options) { o.preservePseudos = v } }

// PreservedSelectors also keeps, in a surviving <style>, every rule with a
// selector containing one of patterns. Matching is by substring, as in juice,
// and the rule is still inlined.
func PreservedSelectors(patterns ...string) Option {
	return func(o *options) { o.preservedSelectors = slices.Clone(patterns) }
}

// PreserveImportant keeps the literal !important suffix on inlined values.
func PreserveImportant(v bool) Option { return func(o *options) { o.preserveImportant = v } }

// ApplyWidthAttributes emits width="" from a CSS width on table cells and images.
func ApplyWidthAttributes(v bool) Option { return func(o *options) { o.applyWidthAttributes = v } }

// ApplyHeightAttributes emits height="" from a CSS height on table cells and images.
func ApplyHeightAttributes(v bool) Option { return func(o *options) { o.applyHeightAttributes = v } }

// ApplyAttributesTableElements emits bgcolor, background, align and valign on
// table elements, which Outlook honours where it ignores the CSS.
func ApplyAttributesTableElements(v bool) Option {
	return func(o *options) { o.applyAttributesTableElement = v }
}

// ResolveCSSVariables substitutes var() references and drops custom properties.
func ResolveCSSVariables(v bool) Option { return func(o *options) { o.resolveCSSVariables = v } }

// InlinePseudoElements materializes ::before and ::after as real elements.
func InlinePseudoElements(v bool) Option { return func(o *options) { o.inlinePseudoElements = v } }

// XMLMode parses and serializes the document as XML, as juice's xmlMode does:
// tag and attribute names keep their case, any element can close itself with
// "/>", none is void or raw text, and elements without children are written
// self-closing.
func XMLMode(v bool) Option { return func(o *options) { o.xmlMode = v } }

// StyleAttributeName writes inlined declarations to an attribute other than style.
func StyleAttributeName(name string) Option {
	return func(o *options) { o.styleAttributeName = name }
}

// CodeBlocks replaces the template delimiters protected from the HTML parser.
// The default set covers Liquid, Handlebars and EJS. juice adds to its default
// set by mutating the global juice.codeBlocks; the Go equivalent is
// appending to DefaultCodeBlocks().
func CodeBlocks(b []CodeBlock) Option { return func(o *options) { o.codeBlocks = b } }

// RemoveIDs replaces id attributes with a hash, rewriting internal anchors.
func RemoveIDs(v bool) Option { return func(o *options) { o.removeIDs = v } }

// RemoveClasses strips class attributes after inlining.
func RemoveClasses(v bool) Option { return func(o *options) { o.removeClasses = v } }

// RemoveComments strips HTML comments. Conditional comments are kept.
func RemoveComments(v bool) Option { return func(o *options) { o.removeComments = v } }

// ResetContentEditable strips contenteditable attributes.
func ResetContentEditable(v bool) Option { return func(o *options) { o.resetContentEditable = v } }

// New returns an Inliner configured by opts.
func New(opts ...Option) *Inliner {
	o := defaults()
	for _, fn := range opts {
		fn(&o)
	}
	return &Inliner{opts: o}
}

// Inline inlines the document's CSS into style attributes.
func (in *Inliner) Inline(doc string) (string, error) {
	out, err := in.InlineBytes([]byte(doc))
	return string(out), err
}

// InlineBytes is Inline over a byte slice.
func (in *Inliner) InlineBytes(doc []byte) ([]byte, error) {
	src, blocks := encodeCodeBlocks(doc, in.opts.codeBlocks)
	root := parseMarkup(src, in.opts.xmlMode)
	if err := in.process(root); err != nil {
		return nil, err
	}
	if in.opts.xmlMode {
		var buf bytes.Buffer
		renderXML(&buf, root)
		return decodeCodeBlocks(buf.Bytes(), blocks), nil
	}
	return decodeCodeBlocks(renderDocument(root), blocks), nil
}

// Inline inlines doc's CSS using a default Inliner.
func Inline(doc string, opts ...Option) (string, error) {
	return New(opts...).Inline(doc)
}

// resolvedEl pairs an element with the declarations resolved for it. The
// property map has to outlive writing the style attribute, because attribute
// promotion reads resolved values rather than reparsing the attribute.
type resolvedEl struct {
	node  *html.Node
	props *propMap
}

// pass holds the state for inlining one document. Inliner itself stays
// immutable so it can be shared across goroutines.
type pass struct {
	o        *options
	resolved []resolvedEl
	byNode   map[*html.Node]*propMap // ancestor lookup for var() resolution

	// InlinePseudoElements only.
	pseudos     map[*html.Node]*pseudoMaps
	pseudoOrder []*html.Node
	matches     []matchEvent // recorded only when content reads a counter
}

// process runs the inlining pipeline over an already-parsed tree.
func (in *Inliner) process(root *html.Node) error {
	p := &pass{o: &in.opts}
	return p.run(root)
}

// appendRules adds one stylesheet's rules, keeping their groups distinct from
// those of earlier stylesheets.
func appendRules(dst, src []rule) []rule {
	off := int32(len(dst))
	for i := range src {
		src[i].group += off
	}
	return append(dst, src...)
}

func (in *pass) run(root *html.Node) error {
	o := in.o

	rules := in.collectCSS(root)
	var keep []preserved
	if extra := strings.TrimSpace(o.extraCSS); extra != "" {
		// juice never preserves pseudo-class rules from extraCss.
		eo := *o
		eo.preservePseudos = false
		r, k, _ := parseStylesheet([]byte(extra), &eo, uint32(len(rules))<<8)
		rules = appendRules(rules, r)
		keep = append(keep, k...)
	}

	in.applyRules(root, rules)
	imgs, err := in.materializePseudos(rules)
	if err != nil {
		return err
	}
	if o.resolveCSSVariables {
		in.resolveVariables()
	}
	in.writeStyles()
	for _, img := range imgs {
		img.apply()
	}
	in.promoteAttributes(root)
	if err := in.emitPreserved(root, keep); err != nil {
		return err
	}
	in.cleanup(root)
	return nil
}

// collectCSS gathers CSS from <style> elements and disposes of them. Each tag
// is handled on its own, because juice computes the text to preserve per tag
// and replaces that tag's contents with it.
func (in *pass) collectCSS(root *html.Node) []rule {
	o := in.o
	var rules []rule
	var ord uint32

	var styles []*html.Node
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "style" {
			styles = append(styles, n)
		}
	})

	for _, s := range styles {
		// juice ignores a <style> that is not exactly one text node.
		if s.FirstChild == nil || s.FirstChild != s.LastChild || s.FirstChild.Type != html.TextNode {
			if o.removeStyleTags {
				detach(s)
			}
			continue
		}
		if _, embedded := getAttr(s, "data-embed"); embedded {
			removeAttr(s, "data-embed")
			continue
		}

		var r []rule
		var k []preserved
		if o.applyStyleTags {
			r, k, ord = parseStylesheet([]byte(s.FirstChild.Data), o, ord)
			rules = appendRules(rules, r)
		}

		if !o.removeStyleTags {
			continue
		}
		if len(k) == 0 {
			detach(s)
			continue
		}
		// Rules that could not be inlined stay behind in this tag.
		s.FirstChild.Data = string(preservedText(k))
	}
	return rules
}

// applyRules walks the document once in order, applying every matching rule to
// each element. Rules are tested in stylesheet order so that the
// different-selector override moves a property to the end exactly as juice's
// delete-and-reinsert does.
func (in *pass) applyRules(root *html.Node, rules []rule) {
	o := in.o
	rs := newRuleSet(rules)
	var sc scratch
	track := o.inlinePseudoElements && needCounters(rules)
	var doc int32
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode || nonVisualElements[n.Data] {
			return
		}
		var m *propMap
		applied := int32(-1)
		doc++
		rs.forEach(n, &sc, func(i int, r *rule) {
			// One selector applies once, however many of its :is()
			// expansions match.
			if r.group == applied {
				return
			}
			applied = r.group
			if track {
				in.matches = append(in.matches, matchEvent{r.group, doc, n})
			}
			if r.pseudo != pseudoNone {
				// Declarations for ::before/::after belong to their own
				// element; without materialization they are dropped. The
				// base element is not touched either way.
				if o.inlinePseudoElements {
					addDecls(in.pseudoMap(n, r.pseudo), r, i)
				}
				return
			}
			if m == nil {
				m = &propMap{}
				// The existing style attribute seeds the map on first match.
				// An element no rule touches is never visited, so its style
				// attribute is left exactly as written.
				if v, ok := getAttr(n, o.styleAttributeName); ok {
					m.seedInline([]byte(v), o)
				}
			}
			addDecls(m, r, i)
		})
		if m == nil {
			return
		}
		in.resolved = append(in.resolved, resolvedEl{n, m})
		if in.byNode == nil {
			in.byNode = make(map[*html.Node]*propMap)
		}
		in.byNode[n] = m
	})
}

func addDecls(m *propMap, r *rule, i int) {
	for _, d := range r.decls {
		if d.prop == "" {
			continue
		}
		prio := 0
		if d.important {
			prio = 2
		}
		m.add(property{
			prop:  d.prop,
			value: d.value,
			key:   packKey(prio, r.spec[0], r.spec[1], r.spec[2]),
			ord:   d.ord,
			rule:  int32(i),
		})
	}
}

// writeStyles serializes each element's resolved declarations. It runs after
// accumulation because attribute promotion reads the same property map, and
// after variable resolution so the promoted values are the substituted ones.
func (in *pass) writeStyles() {
	for _, el := range in.resolved {
		if v := el.props.styleAttr(in.o); v != nil {
			setAttr(el.node, in.o.styleAttributeName, string(v))
		}
	}
}

func preservedText(keep []preserved) []byte {
	var b bytes.Buffer
	b.WriteByte('\n')
	for _, k := range keep {
		b.Write(k.text)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// emitPreserved appends a <style> holding rules that could not be inlined,
// when no surviving <style> already carries them.
func (in *pass) emitPreserved(root *html.Node, keep []preserved) error {
	if len(keep) == 0 || !in.o.insertExtraCSS {
		return nil
	}
	if sel := in.o.insertExtraCSSInto; sel != "" {
		m, err := cascadia.Compile(sel)
		if err != nil {
			return fmt.Errorf("InsertPreservedExtraCSSInto: %w", err)
		}
		if host := cascadia.Query(root, m); host != nil {
			in.appendPreserved(host, keep)
		}
		return nil
	}
	host := findFirst(root, "head")
	if host == nil {
		host = findFirst(root, "body")
	}
	if host == nil {
		host = root
	}
	in.appendPreserved(host, keep)
	return nil
}

// appendPreserved adds the <style> as juice does, by appending markup: in XML
// mode a "<" in the rules is parsed as a tag.
func (in *pass) appendPreserved(host *html.Node, keep []preserved) {
	if in.o.xmlMode {
		frag := parseMarkup(slices.Concat([]byte("<style>"), preservedText(keep), []byte("</style>")), true)
		for c := frag.FirstChild; c != nil; c = frag.FirstChild {
			frag.RemoveChild(c)
			host.AppendChild(c)
		}
		return
	}
	s := &html.Node{Type: html.ElementNode, Data: "style"}
	s.AppendChild(&html.Node{Type: html.TextNode, Data: string(preservedText(keep))})
	host.AppendChild(s)
}
