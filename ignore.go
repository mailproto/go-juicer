package juicer

import (
	"bytes"
	"slices"
	"strings"

	"github.com/mailproto/go-juicer/internal/postcss"
)

// juice's ignore comments. Inlining reads them over one stream, every <style>
// block and then extraCss, as juice concatenates them: a "juice ignore" first
// in the stream turns inlining off entirely, and "start ignore" or "ignore
// next" carry over into the next block. What a block keeps in <style> is
// decided per block, walking it backwards as juice's getPreservedText does.

type ignoreState struct {
	started  bool // the stream has had its first node
	off      bool // "juice ignore" opened the stream
	ignoring bool // inside "juice start ignore"
	skipNext bool // "juice ignore next" is waiting for its node
}

func (st *ignoreState) idle() bool { return !st.off && !st.ignoring && !st.skipNext }

// parseWithDirectives is parseStylesheet honouring juice's ignore comments.
func parseWithDirectives(src []byte, opts *options, ord uint32, st *ignoreState) ([]rule, []preserved, uint32) {
	if opts.mensch {
		return parseMensch(src, opts, ord)
	}
	if st.idle() && !bytes.Contains(src, []byte("juice")) {
		if !st.started && len(trimCSS(src)) > 0 {
			st.started = true
		}
		return parseStylesheet(src, opts, ord)
	}
	s := readStylesheet(src, opts)
	nodes := s.root.Nodes

	// Inlining, forward over the shared stream.
	inline := make([]bool, len(nodes))
	if !st.started && len(nodes) > 0 {
		st.started = true
		st.off = directive(nodes[0]) == "juice ignore"
	}
	for i, n := range nodes {
		switch {
		case st.off:
		case st.skipNext:
			st.skipNext = false
		case n.Type == "comment":
			switch directive(n) {
			case "juice start ignore":
				st.ignoring = true
			case "juice end ignore":
				st.ignoring = false
			case "juice ignore next":
				st.skipNext = true
			}
		case !st.ignoring:
			inline[i] = true
		}
	}

	// Preservation, backward within this block.
	if len(nodes) > 0 && directive(nodes[0]) == "juice ignore" {
		var rules []rule
		for i, n := range nodes {
			if inline[i] {
				var r []rule
				r, _, ord = s.read(one(withoutIgnoredDecls(n)), inlineOnly(opts), ord)
				rules = appendRules(rules, r)
			}
		}
		return rules, []preserved{{"ignore", src}}, ord
	}
	forced := make([]bool, len(nodes))
	for i, n := range nodes {
		if n.Type == "comment" && directive(n) == "juice ignore next" && i+1 < len(nodes) {
			forced[i+1] = true
		}
		if n.Type == "rule" && hasInnerIgnoreNext(n) {
			forced[i] = true
		}
	}
	// group[i] is the index where node i's "start ignore" block begins, or -1.
	group := make([]int, len(nodes))
	inBlock := make([]bool, len(nodes))
	ignoring, end := false, -1
	for i := len(nodes) - 1; i >= 0; i-- {
		group[i] = -1
		n := nodes[i]
		if n.Type == "comment" {
			switch directive(n) {
			case "juice end ignore":
				ignoring, end = true, i
				inBlock[i] = true
			case "juice start ignore":
				ignoring = false
				inBlock[i] = true
				for j := i; j <= max(end, i); j++ {
					if inBlock[j] {
						group[j] = i
					}
				}
				end = -1
			}
			continue
		}
		inBlock[i] = ignoring
	}

	var rules []rule
	var keep []preserved
	keepAll := *opts
	keepAll.keepAll = true
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		if g := group[i]; g == i {
			// The whole block, as one preserved unit.
			var parts []string
			for j := i; j < len(nodes); j++ {
				if group[j] == i {
					parts = append(parts, s.format(nodes[j], &keepAll))
				}
			}
			keep = append(keep, preserved{"ignore", []byte(strings.Join(parts, "\n"))})
		}
		normal := !inBlock[i] && !forced[i] && n.Type != "comment"
		switch {
		case normal && inline[i] && !hasInnerIgnoreNext(n):
			r, k, o := s.read(one(n), opts, ord)
			rules, keep, ord = appendRules(rules, r), append(keep, k...), o
			continue
		case normal:
			_, k, _ := s.read(one(n), opts, ord)
			keep = append(keep, k...)
		case forced[i] && !inBlock[i] && n.Type != "comment":
			keep = append(keep, preserved{"ignore", []byte(s.format(n, &keepAll))})
		}
		if inline[i] && n.Type != "comment" {
			var r []rule
			r, _, ord = s.read(one(withoutIgnoredDecls(n)), inlineOnly(opts), ord)
			rules = appendRules(rules, r)
		}
	}
	return rules, keep, ord
}

// directive is a comment node's text, trimmed, or "".
func directive(n *postcss.Node) string {
	if n.Type != "comment" {
		return ""
	}
	return strings.TrimSpace(n.Text)
}

func hasInnerIgnoreNext(n *postcss.Node) bool {
	return slices.ContainsFunc(n.Nodes, func(k *postcss.Node) bool { return directive(k) == "juice ignore next" })
}

func one(n *postcss.Node) []*postcss.Node { return []*postcss.Node{n} }

// withoutIgnoredDecls drops each declaration that follows a "juice ignore
// next" comment inside a rule.
func withoutIgnoredDecls(n *postcss.Node) *postcss.Node {
	if n.Type != "rule" || !hasInnerIgnoreNext(n) {
		return n
	}
	c := n.Copy()
	c.Nodes = nil
	skip := false
	for _, k := range n.Nodes {
		switch {
		case directive(k) == "juice ignore next":
			skip = true
			continue
		case skip && k.Type == "decl":
			skip = false
			continue
		}
		c.Nodes = append(c.Nodes, k)
	}
	return c
}

// format renders a node the way juice's stringifyNodes does.
func (s stylesheet) format(n *postcss.Node, keepAll *options) string {
	_, k, _ := s.read(one(n), keepAll, 0)
	var parts []string
	for _, p := range k {
		parts = append(parts, string(p.text))
	}
	return strings.Join(parts, "\n")
}

func inlineOnly(o *options) *options {
	c := *o
	c.preserveMediaQueries, c.preserveFontFaces, c.preserveKeyFrames = false, false, false
	c.preserveContainerQueries, c.preserveLayers, c.preservePseudos = false, false, false
	c.preservedSelectors = nil
	return &c
}
