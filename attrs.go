package juicer

import (
	"crypto/md5"
	"encoding/hex"
	"strings"

	"golang.org/x/net/html"
)

// Document cleanup carried over from the Ruby premailer this package used to
// wrap. juice has no equivalent; these run after inlining so that removing a
// class cannot change which rules matched.
func (in *pass) cleanup(root *html.Node) {
	o := in.o
	if !o.removeIDs && !o.removeClasses && !o.removeComments && !o.resetContentEditable {
		return
	}

	// Anchors referencing an id have to be rewritten to the same hash, so
	// collect targets before touching anything.
	targets := map[string]bool{}
	if o.removeIDs {
		walk(root, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" {
				return
			}
			if href, ok := getAttr(n, "href"); ok && strings.HasPrefix(href, "#") && len(href) > 1 {
				targets[href[1:]] = true
			}
		})
	}

	var doomed []*html.Node
	walk(root, func(n *html.Node) {
		switch n.Type {
		case html.CommentNode:
			// Conditional comments carry Outlook-only markup, so they stay.
			if o.removeComments && !isConditionalComment(n.Data) {
				doomed = append(doomed, n)
			}
		case html.ElementNode:
			if o.removeClasses {
				removeAttr(n, "class")
			}
			if o.resetContentEditable {
				removeAttr(n, "contenteditable")
			}
			if o.removeIDs {
				if id, ok := getAttr(n, "id"); ok {
					if targets[id] {
						setAttr(n, "id", hashID(id))
					} else {
						removeAttr(n, "id")
					}
				}
				if href, ok := getAttr(n, "href"); ok && strings.HasPrefix(href, "#") && len(href) > 1 {
					setAttr(n, "href", "#"+hashID(href[1:]))
				}
			}
		}
	})
	// Detach after the walk: RemoveChild clears NextSibling, so removing
	// during the walk would silently skip the rest of each sibling list.
	for _, n := range doomed {
		detach(n)
	}
}

func hashID(id string) string {
	sum := md5.Sum([]byte(id))
	return hex.EncodeToString(sum[:])
}

func isConditionalComment(raw string) bool {
	// <!--[if ...]> ... <![endif]-->
	return strings.HasPrefix(raw, "<!--[")
}

// Outlook ignores CSS width, height and background on table elements, so
// juice mirrors those declarations into presentational attributes.
//
// Two details are load-bearing. These read the resolved property map rather
// than reparsing the style attribute, so they see values after variable
// substitution. And they only look at the head of a duplicate chain.

var widthHeightElements = map[string]bool{
	"table": true, "td": true, "th": true, "img": true,
}

var tableElements = map[string]bool{
	"table": true, "th": true, "tr": true, "td": true, "caption": true,
	"colgroup": true, "col": true, "thead": true, "tbody": true, "tfoot": true,
}

var styleToAttribute = map[string]string{
	"background-color": "bgcolor",
	"background-image": "background",
	"text-align":       "align",
	"vertical-align":   "valign",
}

func (in *pass) promoteAttributes(root *html.Node) {
	o := in.o
	for _, el := range in.resolved {
		n, m := el.node, el.props
		if o.applyWidthAttributes {
			in.setDimension(n, m, "width")
		}
		if o.applyHeightAttributes {
			in.setDimension(n, m, "height")
		}
		if o.applyAttributesTableElement && tableElements[foldedName(n)] {
			// juice walks the element's properties, so attributes land in
			// property order rather than table order.
			for _, i := range m.order() {
				attr, ok := styleToAttribute[m.slots[i].prop]
				if !ok {
					continue
				}
				v := in.attrValue(m.slots[i].value)
				if attr == "background" {
					v = extractURL(v)
				}
				// A gradient cannot go in a presentational attribute.
				if l := strings.ToLower(v); strings.Contains(l, "linear-gradient(") || strings.Contains(l, "radial-gradient(") {
					continue
				}
				setAttr(n, attr, v)
			}
		}
	}
}

// attrValue drops a preserved !important, which has no place in an attribute.
func (in *pass) attrValue(v []byte) string {
	s := string(v)
	if in.o.preserveImportant && strings.HasSuffix(s, "!important") {
		s = strings.TrimRight(strings.TrimSuffix(s, "!important"), " \t\n\r\f")
	}
	return s
}

func (in *pass) setDimension(n *html.Node, m *propMap, dim string) {
	if !widthHeightElements[foldedName(n)] {
		return
	}
	i := m.find(dim)
	if i < 0 {
		return
	}
	v := in.attrValue(m.slots[i].value)
	// juice tests for px or auto anywhere in the value, then strips the first
	// "px". That really does turn `height: auto` into height="auto".
	if strings.Contains(v, "px") || strings.Contains(v, "auto") {
		setAttr(n, dim, strings.Replace(v, "px", "", 1))
		return
	}
	if tableElements[foldedName(n)] && strings.Contains(v, "%") {
		setAttr(n, dim, v)
	}
}

// foldedName is the element name for juice's table and dimension lists, which
// it checks case-insensitively; names only keep their case in XML mode.
func foldedName(n *html.Node) string {
	if strings.IndexFunc(n.Data, func(r rune) bool { return 'A' <= r && r <= 'Z' }) < 0 {
		return n.Data
	}
	return strings.ToLower(n.Data)
}

// extractURL unwraps url(x), url('x') or url("x") to x, exactly as juice's
// /^url\((["'])?([^"']+)\1\)$/ does: whitespace is kept, and anything else,
// including `none` or an empty url(""), passes through unchanged.
func extractURL(v string) string {
	inner, ok := strings.CutPrefix(v, "url(")
	if !ok {
		return v
	}
	if inner, ok = strings.CutSuffix(inner, ")"); !ok {
		return v
	}
	if len(inner) >= 2 && (inner[0] == '"' || inner[0] == '\'') && inner[len(inner)-1] == inner[0] {
		inner = inner[1 : len(inner)-1]
	}
	if inner == "" || strings.ContainsAny(inner, `"'`) {
		return v
	}
	return inner
}
