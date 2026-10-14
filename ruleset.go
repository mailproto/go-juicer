package juicer

import (
	"github.com/mailproto/go-juicer/internal/selector"
	"golang.org/x/net/html"
)

// A rightmost-key rule index, the optimization browsers use and juice does
// not. juice runs one full document traversal per rule, so matching is
// O(rules x nodes) and dominates its runtime; here rules are bucketed by the
// id, class or tag of their rightmost compound and a node only tests the
// buckets its own id, classes and tag can reach.
//
// Buckets hold rule indices rather than matchers, and candidates are merged in
// ascending index order, because rules must be applied in stylesheet order:
// that order decides which selector wins a property and therefore where the
// property lands in the output.

type ruleSet struct {
	rules   []rule
	byID    map[string][]uint32
	byClass map[string][]uint32
	byTag   map[string][]uint32
	// any holds rules whose rightmost compound is universal or keyed only by
	// an attribute, which no node-derived key can index.
	any []uint32
}

// scratch is per-document reusable state, kept off the Inliner so it stays
// safe to share.
type scratch struct {
	lists [][]uint32
	pos   []int32
}

func newRuleSet(rules []rule) *ruleSet {
	rs := &ruleSet{
		rules:   rules,
		byID:    make(map[string][]uint32),
		byClass: make(map[string][]uint32),
		byTag:   make(map[string][]uint32),
	}
	for i := range rules {
		r := &rules[i]
		if r.match == nil {
			continue
		}
		switch r.key {
		case selector.KeyID:
			rs.byID[r.keyName] = append(rs.byID[r.keyName], uint32(i))
		case selector.KeyClass:
			rs.byClass[r.keyName] = append(rs.byClass[r.keyName], uint32(i))
		case selector.KeyTag:
			rs.byTag[r.keyName] = append(rs.byTag[r.keyName], uint32(i))
		default:
			rs.any = append(rs.any, uint32(i))
		}
	}
	return rs
}

// forEach calls fn for every rule that matches n, in stylesheet order.
func (rs *ruleSet) forEach(n *html.Node, sc *scratch, fn func(i int, r *rule)) {
	sc.lists = sc.lists[:0]
	add := func(l []uint32) {
		if len(l) == 0 {
			return
		}
		// class="a a" would otherwise queue the same bucket twice.
		for _, seen := range sc.lists {
			if &seen[0] == &l[0] {
				return
			}
		}
		sc.lists = append(sc.lists, l)
	}

	for _, a := range n.Attr {
		switch a.Key {
		case "id":
			add(rs.byID[a.Val])
		case "class":
			for _, tok := range classTokens(a.Val) {
				add(rs.byClass[tok])
			}
		}
	}
	add(rs.byTag[n.Data])
	add(rs.any)

	if len(sc.lists) == 0 {
		return
	}
	sc.pos = sc.pos[:0]
	for range sc.lists {
		sc.pos = append(sc.pos, 0)
	}
	// Each bucket is ascending by construction and a rule lives in exactly one
	// bucket, so a k-way merge yields every candidate once, in order.
	for {
		best := -1
		var bestV uint32
		for i, l := range sc.lists {
			p := sc.pos[i]
			if int(p) < len(l) && (best < 0 || l[p] < bestV) {
				best, bestV = i, l[p]
			}
		}
		if best < 0 {
			return
		}
		sc.pos[best]++
		r := &rs.rules[bestV]
		if r.match.Match(n) {
			fn(int(bestV), r)
		}
	}
}

// classTokens splits a class attribute without allocating for the common case.
func classTokens(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		for i < len(s) && isCSSSpace(s[i]) {
			i++
		}
		j := i
		for j < len(s) && !isCSSSpace(s[j]) {
			j++
		}
		if j > i {
			out = append(out, s[i:j])
		}
		i = j
	}
	return out
}
