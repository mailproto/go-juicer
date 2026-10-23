package juicer

import "golang.org/x/net/html"

// Element is an element left with a style attribute after inlining, as an
// ElementHook sees it.
type Element struct {
	Tag   string
	Attrs []Attribute
	// Declarations holds what the cascade resolved for the element, one per
	// property, with values as the stylesheet wrote them rather than as they
	// are written into the style attribute. It is nil when no rule matched the
	// element, whose style attribute is then as the document wrote it.
	Declarations []Declaration
}

// Attribute is an HTML attribute.
type Attribute struct{ Name, Value string }

// Declaration is a CSS declaration.
type Declaration struct{ Property, Value string }

// ElementHook calls fn, once inlining is done, for each element with a style
// attribute, and sets the attributes fn returns, in order. Mapping CSS onto
// presentational attributes with rules of the caller's own, as premailer does,
// is one use.
func ElementHook(fn func(Element) []Attribute) Option {
	return func(o *options) { o.elementHook = fn }
}

func (in *pass) runElementHook(root *html.Node) {
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if _, ok := getAttr(n, in.o.styleAttributeName); !ok {
			return
		}
		el := Element{Tag: n.Data}
		for _, a := range n.Attr {
			el.Attrs = append(el.Attrs, Attribute{a.Key, a.Val})
		}
		if m := in.byNode[n]; m != nil {
			el.Declarations = []Declaration{}
			for _, i := range m.order() {
				el.Declarations = append(el.Declarations, Declaration{m.slots[i].prop, string(m.slots[i].value)})
			}
		}
		for _, a := range in.o.elementHook(el) {
			setAttr(n, a.Name, a.Value)
		}
	})
}
