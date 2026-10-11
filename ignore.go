package juicer

import (
	"bytes"
	"strings"
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
	if st.idle() && !bytes.Contains(src, []byte("juice")) {
		if !st.started && len(trimCSS(src)) > 0 {
			st.started = true
		}
		return parseStylesheet(src, opts, ord)
	}
	flat := flattenNesting(src)
	nodes, _ := parseCSSNodes(flat, 0, false)

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
		case n.kind == 'c':
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
				r, _, ord = parseStylesheet(withoutIgnoredDecls(n), inlineOnly(opts), ord)
				rules = appendRules(rules, r)
			}
		}
		return rules, []preserved{{"ignore", src}}, ord
	}
	forced := make([]bool, len(nodes))
	for i, n := range nodes {
		if n.kind == 'c' && directive(n) == "juice ignore next" && i+1 < len(nodes) {
			forced[i+1] = true
		}
		if n.kind == 'r' && hasInnerIgnoreNext(n) {
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
		if n.kind == 'c' {
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
					parts = append(parts, formatNode(nodes[j], &keepAll))
				}
			}
			keep = append(keep, preserved{"ignore", []byte(strings.Join(parts, "\n"))})
		}
		normal := !inBlock[i] && !forced[i] && n.kind != 'c'
		switch {
		case normal && inline[i] && !hasInnerIgnoreNext(n):
			r, k, o := parseStylesheet(nodeText(n), opts, ord)
			rules, keep, ord = appendRules(rules, r), append(keep, k...), o
			continue
		case normal:
			_, k, _ := parseStylesheet(nodeText(n), opts, ord)
			keep = append(keep, k...)
		case forced[i] && !inBlock[i] && n.kind != 'c':
			keep = append(keep, preserved{"ignore", []byte(formatNode(n, &keepAll))})
		}
		if inline[i] && n.kind != 'c' {
			var r []rule
			r, _, ord = parseStylesheet(withoutIgnoredDecls(n), inlineOnly(opts), ord)
			rules = appendRules(rules, r)
		}
	}
	return rules, keep, ord
}

// directive is a comment node's text, trimmed, or "".
func directive(n *cssNode) string {
	if n.kind != 'c' {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(string(n.raw), "/*"), "*/"))
}

func hasInnerIgnoreNext(n *cssNode) bool {
	for _, k := range n.kids {
		if directive(k) == "juice ignore next" {
			return true
		}
	}
	return false
}

func nodeText(n *cssNode) []byte {
	var b bytes.Buffer
	writeCSSNodes(&b, []*cssNode{n}, true)
	return b.Bytes()
}

// withoutIgnoredDecls drops each declaration that follows a "juice ignore
// next" comment inside a rule.
func withoutIgnoredDecls(n *cssNode) []byte {
	if n.kind != 'r' || !hasInnerIgnoreNext(n) {
		return nodeText(n)
	}
	var kids []*cssNode
	skip := false
	for _, k := range n.kids {
		switch {
		case directive(k) == "juice ignore next":
			skip = true
			continue
		case skip && k.kind == 'd':
			skip = false
			continue
		}
		kids = append(kids, k)
	}
	return nodeText(&cssNode{kind: 'r', head: n.head, kids: kids, block: true, semi: n.semi})
}

// formatNode renders a node the way juice's stringifyNodes does.
func formatNode(n *cssNode, keepAll *options) string {
	if n.kind == 'c' {
		return string(n.raw)
	}
	_, k, _ := parseStylesheet(nodeText(n), keepAll, 0)
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
