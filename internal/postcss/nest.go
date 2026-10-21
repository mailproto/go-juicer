package postcss

import (
	"slices"
	"strings"
)

// Nest flattens CSS nesting in place, as postcss-nesting 14 (the 2024-02
// edition) does when juice runs it as a postcss plugin. The plugin's moves are
// ported one for one, and so is postcss's visitor loop around them: a rule a
// move dirties is visited again, until a pass changes nothing.
func Nest(root *Node) {
	if !nested(root) {
		return
	}
	s := nester{clean: map[*Node]bool{}, iters: map[*Node][]*int{}}
	for !s.clean[root] {
		s.clean[root] = true
		s.visit(root)
	}
}

// nested reports whether the plugin has anything to do: a rule holding a rule
// or at-rule, or a selector with "&".
func nested(n *Node) bool {
	for _, c := range n.Nodes {
		if c.Type == "rule" && (strings.Contains(c.Selector, "&") ||
			slices.ContainsFunc(c.Nodes, func(k *Node) bool { return k.Type == "rule" || k.Type == "atrule" })) {
			return true
		}
		if nested(c) {
			return true
		}
	}
	return false
}

// hoisted lists the at-rules postcss-nesting lifts out of a style rule.
var hoisted = []string{"container", "document", "media", "supports", "layer", "starting-style"}

// nester holds what postcss keeps on nodes while plugins run: which nodes are
// clean, and the live iterators over each container, which its mutations
// keep pointing at the same child.
type nester struct {
	clean map[*Node]bool
	iters map[*Node][]*int
}

// visit is postcss's walkSync: the Rule listener, then each child not yet
// clean. A rule its listener detached is not descended into.
func (s *nester) visit(n *Node) {
	if n.Type == "rule" {
		s.rule(n)
		if n.parent == nil {
			return
		}
	}
	s.each(n, func(c *Node) {
		if !s.clean[c] {
			s.clean[c] = true
			s.visit(c)
		}
	})
}

// rule is the plugin's Rule listener.
func (s *nester) rule(n *Node) {
	s.walk(n)
	if strings.Contains(n.Selector, "&") {
		s.ampersandToScope(n)
	}
}

func (s *nester) walk(n *Node) {
	s.each(n, func(c *Node) {
		parent := c.parent
		switch {
		case c.Type == "rule" && parent.Type == "rule" && !strings.Contains(c.Selector, "|"):
			s.ruleWithinRule(c, parent)
		case c.Type == "atrule" && parent.Type == "rule" && slices.Contains(hoisted, c.Name):
			s.atruleWithinRule(c, parent)
		}
		if c.hasNodes && len(c.Nodes) > 0 {
			s.walk(c)
		}
	})
}

func (s *nester) ruleWithinRule(n, parent *Node) {
	// postcss-nesting skips a rule whose selector, or its parent's, does
	// not parse.
	if !parsesSelector(n.Selector) || !parsesSelector(parent.Selector) {
		return
	}
	sel := resolveNested(n.Selector, parent.Selector)
	s.shiftBeforeParent(n, parent)
	s.setSelector(n, sel)
	if n.Selector == parent.Selector {
		s.append(n, slices.Clone(parent.Nodes)...)
	}
	s.cleanup(parent)
}

func (s *nester) atruleWithinRule(n, parent *Node) {
	s.shiftBeforeParent(n, parent)
	if !n.hasNodes {
		s.cleanup(parent)
		return
	}
	o := s.clone(parent)
	s.removeAll(o)
	s.append(o, slices.Clone(n.Nodes)...)
	s.append(n, o)
	s.cleanup(parent)
	s.walk(o)
}

// shiftBeforeParent moves n out ahead of its parent, and the children before
// it into a copy of the parent ahead of that.
func (s *nester) shiftBeforeParent(n, parent *Node) {
	if i := slices.Index(parent.Nodes, n); i > 0 {
		c := s.clone(parent)
		s.insertBefore(parent.parent, parent, c)
		s.removeAll(c)
		s.append(c, slices.Clone(parent.Nodes[:i])...)
		c.semicolon = true
		s.cleanup(c)
	}
	s.insertBefore(parent.parent, parent, n)
	parent.semicolon = true
}

// cleanup drops a rule left empty, and one left holding only comments in
// favour of the comments.
func (s *nester) cleanup(n *Node) {
	if len(n.Nodes) == 0 {
		s.removeChild(n.parent, n)
		return
	}
	if !slices.ContainsFunc(n.Nodes, func(k *Node) bool { return k.Type != "comment" }) {
		s.replaceWith(n, slices.Clone(n.Nodes))
	}
}

// ampersandToScope resolves "&" against :scope in a rule outside any other.
func (s *nester) ampersandToScope(n *Node) {
	for p := n.parent; p != nil; p = p.parent {
		if p.Type == "rule" || p.Type == "atrule" && p.Name == "scope" {
			return
		}
	}
	if parsesSelector(n.Selector) && selectorHasNesting(n.Selector) {
		s.setSelector(n, resolveNested(n.Selector, ":scope"))
	}
}

// The rest is postcss's Container and Node, as the plugin uses them.

func (s *nester) each(n *Node, fn func(*Node)) {
	if !n.hasNodes {
		return
	}
	it := new(int)
	s.iters[n] = append(s.iters[n], it)
	for *it < len(n.Nodes) {
		fn(n.Nodes[*it])
		*it++
	}
	its := s.iters[n]
	s.iters[n] = slices.Delete(its, slices.Index(its, it), slices.Index(its, it)+1)
}

func (s *nester) markDirty(n *Node) {
	if s.clean[n] {
		for ; n != nil; n = n.parent {
			s.clean[n] = false
		}
	}
}

func (s *nester) markTreeDirty(n *Node) {
	s.clean[n] = false
	for _, c := range n.Nodes {
		s.markTreeDirty(c)
	}
}

// adopt takes nodes from wherever they are for parent, as normalize does.
func (s *nester) adopt(parent *Node, nodes []*Node) {
	for _, c := range nodes {
		if c.parent != nil {
			s.removeChild(c.parent, c)
		}
		if s.clean[c] {
			s.markTreeDirty(c)
		}
		c.parent = parent
	}
}

func (s *nester) insertBefore(parent, exist *Node, nodes ...*Node) {
	s.adopt(parent, nodes)
	i := slices.Index(parent.Nodes, exist)
	parent.Nodes = slices.Insert(parent.Nodes, i, nodes...)
	for _, it := range s.iters[parent] {
		if i <= *it {
			*it += len(nodes)
		}
	}
	s.markDirty(parent)
}

func (s *nester) insertAfter(parent, exist *Node, nodes ...*Node) {
	s.adopt(parent, nodes)
	i := slices.Index(parent.Nodes, exist)
	parent.Nodes = slices.Insert(parent.Nodes, i+1, nodes...)
	for _, it := range s.iters[parent] {
		if i < *it {
			*it += len(nodes)
		}
	}
	s.markDirty(parent)
}

func (s *nester) append(parent *Node, nodes ...*Node) {
	s.adopt(parent, nodes)
	parent.Nodes = append(parent.Nodes, nodes...)
	s.markDirty(parent)
}

func (s *nester) removeChild(parent, n *Node) {
	i := slices.Index(parent.Nodes, n)
	n.parent = nil
	parent.Nodes = slices.Delete(parent.Nodes, i, i+1)
	for _, it := range s.iters[parent] {
		if *it >= i {
			*it--
		}
	}
	s.markDirty(parent)
}

func (s *nester) removeAll(n *Node) {
	for _, c := range n.Nodes {
		c.parent = nil
	}
	n.Nodes = nil
	s.markDirty(n)
}

// replaceWith is Node#replaceWith for nodes that do not include n.
func (s *nester) replaceWith(n *Node, nodes []*Node) {
	parent := n.parent
	for _, c := range nodes {
		s.insertBefore(parent, n, c)
	}
	s.removeChild(parent, n)
}

func (s *nester) setSelector(n *Node, sel string) {
	if n.Selector != sel {
		n.SetSelector(sel)
		s.markDirty(n)
	}
}

// SetSelector replaces a rule's selector. As in postcss, the raw text it was
// read from stays, and is written while it still reads as the selector.
func (n *Node) SetSelector(sel string) {
	n.Selector, n.vs, n.ve = sel, 0, 0
}

// Selectors is postcss's rule.selectors: list.comma, empty entries kept.
func (n *Node) Selectors() []string {
	var out []string
	for _, s := range commaList([]byte(n.Selector)) {
		out = append(out, string(s))
	}
	return out
}

// Copy is a shallow copy of n, detached, sharing its children.
func (n *Node) Copy() *Node {
	c := *n
	c.parent = nil
	return &c
}

// clone is a deep copy, detached and not yet visited.
func (s *nester) clone(n *Node) *Node {
	c := *n
	c.parent = nil
	c.Nodes = nil
	for _, k := range n.Nodes {
		kc := s.clone(k)
		kc.parent = &c
		c.Nodes = append(c.Nodes, kc)
	}
	return &c
}
