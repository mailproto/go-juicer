package juicer

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// ::before and ::after rules, with InlinePseudoElements on, are applied to a
// <span> of their own, which juice inserts into the element when it ends up
// with a content property. Everything here follows juice's inline.js.

// pseudoMaps holds one element's ::before and ::after declarations.
type pseudoMaps [2]*propMap

func (in *pass) pseudoMap(n *html.Node, kind uint8) *propMap {
	if in.pseudos == nil {
		in.pseudos = map[*html.Node]*pseudoMaps{}
		in.pseudoOrder = nil
	}
	pm := in.pseudos[n]
	if pm == nil {
		pm = &pseudoMaps{}
		in.pseudos[n] = pm
		in.pseudoOrder = append(in.pseudoOrder, n)
	}
	i := kind - pseudoBefore
	if pm[i] == nil {
		pm[i] = &propMap{}
	}
	return pm[i]
}

// materializePseudos inserts a span for each pseudo-element with content and
// registers it like any other styled element, so var() resolution walks up
// through its element and its style attribute is written with the rest.
// It returns the spans whose content was an image.
func (in *pass) materializePseudos(rules []rule) (imgs []pseudoImg, err error) {
	if len(in.pseudoOrder) == 0 {
		return nil, nil
	}
	counters := in.simulateCounters(rules)
	for _, base := range in.pseudoOrder {
		for i, m := range in.pseudos[base] {
			if m == nil {
				continue
			}
			c := m.find("content")
			if c < 0 {
				continue
			}
			span := &html.Node{Type: html.ElementNode, Data: "span"}
			if i == 0 {
				base.InsertBefore(span, base.FirstChild)
			} else {
				base.AppendChild(span)
			}
			in.resolved = append(in.resolved, resolvedEl{span, m})
			if in.byNode == nil {
				in.byNode = make(map[*html.Node]*propMap)
			}
			in.byNode[span] = m

			text, img, err := in.parseContent(string(m.slots[c].value), span, base, counters[base])
			if err != nil {
				return nil, err
			}
			if img != "" {
				imgs = append(imgs, pseudoImg{span, img})
			} else if text != "" {
				span.AppendChild(&html.Node{Type: html.TextNode, Data: text})
			}
		}
	}
	return imgs, nil
}

// pseudoImg is a pseudo-element whose content was url(...). juice turns it
// into an <img> after writing its style attribute, so src comes second.
type pseudoImg struct {
	node *html.Node
	src  string
}

func (p pseudoImg) apply() {
	p.node.Data = "img"
	p.node.Attr = append(p.node.Attr, html.Attribute{Key: "src", Val: p.src})
}

var (
	contentURL     = regexp.MustCompile(`(?i)^\s*url\s*\(\s*(.*?)\s*\)\s*$`)
	contentQuotes  = regexp.MustCompile(`['"]`)
	contentVar     = regexp.MustCompile(`(?i)var\s*\(\s*(.*?)\s*(,\s*(.*?)\s*)?\s*\)`)
	contentCounter = regexp.MustCompile(`(?i)counter\s*\(\s*(.*?)\s*(,\s*(.*?)\s*)?\s*\)`)
	contentAttr    = regexp.MustCompile(`(?i)attr\s*\(\s*(.*?)\s*\)`)
	edgeQuote      = regexp.MustCompile(`^['"]|['"]$`)
)

// errContent stands in for the TypeError juice throws while reading content.
var errContent = errors.New("juice cannot read this pseudo-element content")

// parseContent is juice's parseContent: it splits on quotes and reads
// var(), counter() and attr() in any piece, quoted or not.
func (in *pass) parseContent(content string, span, base *html.Node, scope *counterScope) (text, img string, err error) {
	if content == "none" || content == "normal" {
		return "", "", nil
	}
	if m := contentURL.FindStringSubmatch(content); m != nil {
		return "", edgeQuote.ReplaceAllString(m[1], ""), nil
	}
	var b strings.Builder
	for _, tok := range contentQuotes.Split(content, -1) {
		if tok == "" {
			continue
		}
		if m := contentVar.FindStringSubmatchIndex(tok); m != nil {
			v, ok := in.lookupVar(span, tok[m[2]:m[3]])
			switch {
			case ok && len(v) > 0:
				b.WriteString(edgeQuote.ReplaceAllString(string(v), ""))
			case m[4] >= 0:
				// juice falls back to the comma and all.
				b.WriteString(edgeQuote.ReplaceAllString(tok[m[4]:m[5]], ""))
			default:
				return "", "", errContent
			}
			continue
		}
		if m := contentCounter.FindStringSubmatchIndex(tok); m != nil {
			if v, ok := scope.lookup(tok[m[2]:m[3]]); ok {
				style := ""
				if m[6] >= 0 {
					style = tok[m[6]:m[7]]
				}
				s, err := counterStyle(v, style)
				if err != nil {
					return "", "", err
				}
				b.WriteString(s)
				continue
			}
		}
		if m := contentAttr.FindStringSubmatch(tok); m != nil {
			v, _ := getAttr(base, m[1])
			b.WriteString(v)
			continue
		}
		b.WriteString(tok)
	}
	// juice's "naive unescape".
	return strings.ReplaceAll(b.String(), `\`, ""), "", nil
}

func counterStyle(n int, style string) (string, error) {
	switch style {
	case "lower-roman":
		return strings.ToLower(romanize(n)), nil
	case "upper-roman":
		return romanize(n), nil
	case "lower-latin", "lower-alpha", "upper-latin", "upper-alpha":
		s := alphanumeric(n)
		if s == "" {
			// juice calls toLowerCase on undefined.
			return "", errContent
		}
		if strings.HasPrefix(style, "lower") {
			s = strings.ToLower(s)
		}
		return s, nil
	}
	return strconv.Itoa(n), nil
}

// romanize ports juice's numbers.romanize, digit by digit from the right.
// ponytail: juice throws for a few negative counts; those come out empty.
func romanize(n int) string {
	key := [...]string{"", "C", "CC", "CCC", "CD", "D", "DC", "DCC", "DCCC", "CM",
		"", "X", "XX", "XXX", "XL", "L", "LX", "LXX", "LXXX", "XC",
		"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX"}
	digits := strconv.Itoa(n)
	roman := ""
	for i := 2; i >= 0; i-- {
		if digits == "" {
			break
		}
		d := digits[len(digits)-1]
		digits = digits[:len(digits)-1]
		if d >= '0' && d <= '9' {
			roman = key[int(d-'0')+i*10] + roman
		}
	}
	thousands, _ := strconv.Atoi(digits)
	return strings.Repeat("M", max(thousands, 0)) + roman
}

func alphanumeric(n int) string {
	s := ""
	for n > 0 {
		t := (n - 1) % 26
		s = string(rune('A'+t)) + s
		n = (n - t) / 26
	}
	return s
}

// counterScope is an element's counterProps: own values, falling back to the
// parent's object as it was linked when the element was first matched.
type counterScope struct {
	own    map[string]int
	parent *counterScope
}

func (s *counterScope) lookup(name string) (int, bool) {
	for ; s != nil; s = s.parent {
		if v, ok := s.own[name]; ok {
			return v, true
		}
	}
	return 0, false
}

// matchEvent is one rule matching one element, kept to replay counters in
// juice's order: rule by rule, each over the document.
type matchEvent struct {
	rule int32
	doc  int32
	node *html.Node
}

// needCounters reports whether any ::before/::after content reads a counter,
// the only case where juice's rule-major counter bookkeeping shows.
func needCounters(rules []rule) bool {
	return slices.ContainsFunc(rules, func(r rule) bool {
		return r.pseudo != pseudoNone && slices.ContainsFunc(r.decls, func(d decl) bool {
			return d.prop == "content" && strings.Contains(strings.ToLower(string(d.value)), "counter")
		})
	})
}

// simulateCounters replays counter-reset and counter-increment as juice
// applies them: one rule at a time over every element it matches, with one
// global value per counter name. An element's scope inherits its parent's
// only if the parent had been matched by then.
func (in *pass) simulateCounters(rules []rule) map[*html.Node]*counterScope {
	if len(in.matches) == 0 {
		return nil
	}
	slices.SortStableFunc(in.matches, func(a, b matchEvent) int {
		if a.rule != b.rule {
			return int(a.rule - b.rule)
		}
		return int(a.doc - b.doc)
	})
	scopes := map[*html.Node]*counterScope{}
	styled := map[*html.Node]bool{}
	global := map[string]int{}
	for _, ev := range in.matches {
		n := ev.node
		s := scopes[n]
		if s == nil {
			s = &counterScope{own: map[string]int{}}
			if n.Parent != nil {
				s.parent = scopes[n.Parent]
			}
			scopes[n] = s
		}
		r := &rules[ev.rule]
		if r.pseudo == pseudoNone && !styled[n] {
			// The style attribute is read the first time a plain rule
			// creates the element's styleProps.
			styled[n] = true
			if v, ok := getAttr(n, in.o.styleAttributeName); ok {
				for _, d := range splitDeclarations([]byte(v)) {
					in.counterOp(s, global, d)
				}
			}
		}
		for _, d := range r.decls {
			in.counterOp(s, global, d)
		}
	}
	return scopes
}

func (in *pass) counterOp(s *counterScope, global map[string]int, d decl) {
	if d.prop != "counter-reset" && d.prop != "counter-increment" {
		return
	}
	// juice reads the counter before dropping !important.
	v := string(d.value)
	if d.important {
		v += " !important"
	}
	tok := strings.Fields(v)
	for j := 0; j < len(tok); j++ {
		name := tok[j]
		next, isNum := 0, false
		if j+1 < len(tok) {
			next, isNum = jsParseInt(tok[j+1])
		}
		if d.prop == "counter-reset" {
			s.own[name], global[name] = next, next
		} else {
			if _, ok := s.lookup(name); !ok {
				continue
			}
			if !isNum {
				next = 1
			}
			global[name] += next
			s.own[name] = global[name]
		}
		if isNum {
			j++
		}
	}
}

// jsParseInt is parseInt(s, 10): an optional sign and the leading digits.
func jsParseInt(s string) (int, bool) {
	s = strings.TrimLeft(s, " \t\n\r\f\v")
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0, false
	}
	v, err := strconv.Atoi(s[:j])
	return v, err == nil
}
