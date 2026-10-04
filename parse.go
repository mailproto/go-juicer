package juicer

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// HTML parsing and serialization matched to htmlparser2 in non-XML mode, which
// is what juice parses with (cheerio is loaded with decodeEntities:false).
//
// This deliberately skips HTML5 tree construction: no implied <tbody>, no
// <html>/<head>/<body> synthesis, no foster parenting, no reconstruction of
// active formatting elements. x/net/html's own Parse would do all of those and
// would decode entities besides, and a mail client can see the difference.
//
// The tree is built out of *html.Node so cascadia can match against it
// directly, but it is populated by hand and differs from what html.Parse
// produces in two ways worth knowing:
//
//   - Attr[i].Val and text hold RAW, UNDECODED source bytes. "&nbsp;" stays
//     "&nbsp;". This is also what cheerio matches against, so selectors behave
//     the same.
//   - Comment and Doctype nodes keep their full source text in Data,
//     delimiters included, because bogus constructs like <!decl> and <?pi?>
//     have to round-trip verbatim.
//
// Namespace is set to "svg" or "math" on foreign content, which the serializer
// uses to decide self-closing.

var voidElements = map[string]bool{
	"area": true, "base": true, "basefont": true, "br": true, "col": true,
	"command": true, "embed": true, "frame": true, "hr": true, "img": true,
	"input": true, "isindex": true, "keygen": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// openImpliesClose mirrors htmlparser2's table: opening the key tag pops the
// stack while the element on top is in the value set. It tests only the top of
// the stack, which is why <ul><li>a<ul><li>b</ul> nests rather than closing
// the outer <li>.
var openImpliesClose = map[string]map[string]bool{
	"tr":       {"tr": true, "th": true, "td": true},
	"th":       {"th": true},
	"td":       {"thead": true, "th": true, "td": true},
	"body":     {"head": true, "link": true, "script": true},
	"li":       {"li": true},
	"option":   {"option": true},
	"optgroup": {"optgroup": true, "option": true},
	"dd":       {"dt": true, "dd": true},
	"dt":       {"dt": true, "dd": true},
}

var formTags = map[string]bool{
	"input": true, "option": true, "optgroup": true, "select": true,
	"button": true, "datalist": true, "textarea": true,
}

func init() {
	for _, t := range []string{"select", "input", "output", "button", "datalist", "textarea"} {
		openImpliesClose[t] = formTags
	}
	pTag := map[string]bool{"p": true}
	for _, t := range []string{
		"p", "h1", "h2", "h3", "h4", "h5", "h6",
		"address", "article", "aside", "blockquote", "details", "div", "dl",
		"fieldset", "figcaption", "figure", "footer", "form", "header",
		"hgroup", "hr", "main", "menu", "nav", "ol", "pre", "section",
		"table", "ul",
	} {
		openImpliesClose[t] = pTag
	}
}

// reparseRaw are tags x/net/html's tokenizer reports as raw text but
// htmlparser2 parses as markup, so their contents get a second pass.
var reparseRaw = map[string]bool{
	"noscript": true, "iframe": true, "noembed": true, "noframes": true,
}

func isForeign(tag string) bool { return tag == "svg" || tag == "math" }

// parseDocument builds an htmlparser2-shaped tree. The returned root is a
// synthetic container whose children are the document's top-level nodes.
func parseDocument(src []byte) *html.Node {
	root := &html.Node{Type: html.DocumentNode}
	cur := root
	foreign := 0
	z := html.NewTokenizer(bytes.NewReader(src))
	pos := 0 // offset in src of the tokenizer's next byte

	closeTag := func(name string) {
		open := false
		for p := cur; p != root; p = p.Parent {
			if p.Data == name {
				open = true
				break
			}
		}
		if open {
			for cur.Data != name {
				if isForeign(cur.Data) {
					foreign--
				}
				cur = cur.Parent
			}
			if isForeign(cur.Data) {
				foreign--
			}
			if reparseRaw[cur.Data] {
				reparseChildren(cur)
			}
			cur = cur.Parent
		} else if name == "br" || name == "p" {
			// htmlparser2 turns an unmatched </br> into <br> and an
			// unmatched </p> into an empty <p></p>.
			cur.AppendChild(&html.Node{Type: html.ElementNode, Data: name})
		}
		// Any other unmatched end tag is dropped.
	}
	// resume restarts the tokenizer where htmlparser2 says a construct ends,
	// when x/net/html ended it elsewhere.
	resume := func(end int) {
		if end != pos {
			pos = end
			z = html.NewTokenizer(bytes.NewReader(src[end:]))
		}
	}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		// Slice the token out of src rather than z.Raw(), which points into a
		// buffer the tokenizer reuses.
		start := pos
		pos += len(z.Raw())
		raw := src[start:pos]

		switch tt {
		case html.TextToken:
			cur.AppendChild(&html.Node{Type: html.TextNode, Data: string(raw)})

		case html.CommentToken, html.DoctypeToken:
			var n *html.Node
			var end int
			if bytes.HasPrefix(raw, []byte("</")) {
				var name string
				n, name, end = closeTagLike(src, start)
				if name != "" {
					closeTag(name)
				}
			} else {
				n, end = markupDecl(src, start)
			}
			if n != nil {
				cur.AppendChild(n)
			}
			resume(end)

		case html.StartTagToken, html.SelfClosingTagToken:
			name, attrs := scanStartTag(raw)
			if name == "" {
				continue
			}
			if set := openImpliesClose[name]; set != nil {
				for cur != root && set[cur.Data] {
					cur = cur.Parent
				}
			}
			n := &html.Node{Type: html.ElementNode, Data: name, Attr: attrs}
			if foreign > 0 || isForeign(name) {
				n.Namespace = "svg"
			}
			cur.AppendChild(n)
			if voidElements[name] {
				continue
			}
			// Outside foreign content htmlparser2 ignores the solidus, so
			// <div/> opens a div rather than closing it.
			if tt == html.SelfClosingTagToken && n.Namespace != "" {
				continue
			}
			cur = n
			if isForeign(name) {
				foreign++
			}

		case html.EndTagToken:
			if name := scanEndTag(raw); name != "" {
				closeTag(name)
			}
		}
	}
	// Anything still open at EOF stays open; the serializer closes it.
	return root
}

// markupDecl reads the "<!" or "<?" construct at src[p:] the way htmlparser2's
// tokenizer does, which is not the HTML5 spec x/net/html follows. It returns
// the node produced, if any, and where the construct ends.
//
//   - <!-- ends at the first "-->", counting the opening dashes, so <!--> is
//     an empty comment and "--!>" ends nothing.
//   - <![CDATA[ (exact case) ends at "]]>" and becomes a comment, since there
//     is no CDATA outside XML mode.
//   - Anything else ends at the next ">", but the character right after "<!"
//     or "<!-" is never that ">".
//   - Unterminated at end of input, a comment or CDATA is closed, and
//     anything else becomes text minus its "<!" or "<?".
func markupDecl(src []byte, p int) (*html.Node, int) {
	n := len(src)
	tail := func() (*html.Node, int) {
		if p+2 >= n {
			return nil, n
		}
		return &html.Node{Type: html.TextNode, Data: string(src[p+2:])}, n
	}
	untilGt := func(from int) (*html.Node, int) {
		k := bytes.IndexByte(src[min(from, n):], '>')
		if k < 0 {
			return tail()
		}
		end := from + k + 1
		typ := html.CommentNode
		if len(src)-p >= 9 && bytes.EqualFold(src[p:p+9], []byte("<!doctype")) {
			typ = html.DoctypeNode
		}
		return &html.Node{Type: typ, Data: string(src[p:end])}, end
	}

	i := p + 2
	switch {
	case src[p+1] == '?':
		return untilGt(i)
	case i >= n:
		return tail()
	case src[i] == '[':
		const seq = "CDATA["
		j := i + 1
		for j < n && j-i-1 < len(seq) && src[j] == seq[j-i-1] {
			j++
		}
		if j-i-1 == len(seq) {
			return commentLike(src, j, "]]>", 0, "<!--[CDATA[", "]]-->")
		}
		if j >= n {
			return tail()
		}
		return untilGt(j) // the mismatched character is reconsumed
	case src[i] == '-':
		if i+1 >= n {
			return tail()
		}
		if src[i+1] == '-' {
			return commentLike(src, i+2, "-->", 2, "<!--", "-->")
		}
		return untilGt(i + 2)
	default:
		return untilGt(i + 1)
	}
}

// closeTagLike reads the "</" construct at src[p:] the way htmlparser2 does,
// where x/net/html makes a bogus comment of anything not starting with a
// letter: whitespace after the slash is skipped, "</>" stays as text, a
// letter starts an end tag running to the next ">", and anything else is a
// comment up to it. It returns a node or an end tag name, and where the construct ends.
func closeTagLike(src []byte, p int) (n *html.Node, name string, end int) {
	i := p + 2
	for i < len(src) && isSpace(src[i]) {
		i++
	}
	if i >= len(src) {
		return &html.Node{Type: html.TextNode, Data: string(src[p:])}, "", len(src)
	}
	k := bytes.IndexByte(src[i:], '>')
	switch c := src[i]; {
	case c == '>':
		// Back to text without moving the text start, so it is kept verbatim.
		return &html.Node{Type: html.TextNode, Data: string(src[p : i+1])}, "", i + 1
	case 'a' <= c|0x20 && c|0x20 <= 'z':
		if k < 0 {
			return nil, "", len(src)
		}
		j := i
		for j < i+k && !isSpace(src[j]) {
			j++
		}
		return nil, lowerASCII(src[i:j]), i + k + 1
	case k < 0:
		return &html.Node{Type: html.TextNode, Data: string(src[i:])}, "", len(src)
	default:
		return &html.Node{Type: html.CommentNode, Data: "<!--" + string(src[i:i+k]) + "-->"}, "", i + k + 1
	}
}

// commentLike scans from the start of a comment's or CDATA section's content
// for its end sequence, with htmlparser2's matching: the sequence may already
// be partly matched (idx), and a run of its first character is allowed, as in
// "--->". The content is re-emitted between open and close.
func commentLike(src []byte, from int, seq string, idx int, open, close string) (*html.Node, int) {
	node := func(data []byte) *html.Node {
		return &html.Node{Type: html.CommentNode, Data: open + string(data) + close}
	}
	for k := from; k < len(src); k++ {
		switch c := src[k]; {
		case c == seq[idx]:
			if idx++; idx == len(seq) {
				return node(src[from:max(from, k-2)]), k + 1
			}
		case idx > 0 && c != seq[idx-1]:
			idx = 0
		}
	}
	if from >= len(src) {
		return nil, len(src)
	}
	return node(src[from:]), len(src)
}

// reparseChildren re-parses an element the tokenizer treated as raw text but
// htmlparser2 would have parsed as markup.
func reparseChildren(n *html.Node) {
	if n.FirstChild == nil || n.FirstChild != n.LastChild || n.FirstChild.Type != html.TextNode {
		return
	}
	text := n.FirstChild.Data
	n.RemoveChild(n.FirstChild)
	inner := parseDocument([]byte(text))
	for c := inner.FirstChild; c != nil; {
		next := c.NextSibling
		inner.RemoveChild(c)
		n.AppendChild(c)
		c = next
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func lowerASCII(b []byte) string {
	for _, c := range b {
		if 'A' <= c && c <= 'Z' {
			out := make([]byte, len(b))
			for i, c := range b {
				if 'A' <= c && c <= 'Z' {
					c += 'a' - 'A'
				}
				out[i] = c
			}
			return string(out)
		}
	}
	return string(b)
}

// scanStartTag reads a tag name and attributes from raw start-tag bytes,
// keeping values exactly as written. x/net/html's TagAttr decodes entities,
// which loses the difference between "&amp;" and a literal "&".
func scanStartTag(b []byte) (string, []html.Attribute) {
	i := 1 // skip '<'
	start := i
	for i < len(b) && !isSpace(b[i]) && b[i] != '>' && b[i] != '/' {
		i++
	}
	name := lowerASCII(b[start:i])
	if name == "" {
		return "", nil
	}

	var attrs []html.Attribute
	for i < len(b) {
		for i < len(b) && (isSpace(b[i]) || b[i] == '/') {
			i++
		}
		if i >= len(b) || b[i] == '>' {
			break
		}
		ns := i
		for i < len(b) && !isSpace(b[i]) && b[i] != '=' && b[i] != '>' && b[i] != '/' {
			i++
		}
		an := lowerASCII(b[ns:i])
		for i < len(b) && isSpace(b[i]) {
			i++
		}
		var val string
		if i < len(b) && b[i] == '=' {
			i++
			for i < len(b) && isSpace(b[i]) {
				i++
			}
			if i < len(b) && (b[i] == '"' || b[i] == '\'') {
				q := b[i]
				i++
				vs := i
				for i < len(b) && b[i] != q {
					i++
				}
				val = string(b[vs:i])
				if i < len(b) {
					i++
				}
			} else {
				vs := i
				for i < len(b) && !isSpace(b[i]) && b[i] != '>' {
					i++
				}
				val = string(b[vs:i])
			}
		}
		if an == "" {
			continue
		}
		// htmlparser2 keeps the first occurrence of a repeated attribute.
		dup := false
		for k := range attrs {
			if attrs[k].Key == an {
				dup = true
				break
			}
		}
		if !dup {
			attrs = append(attrs, html.Attribute{Key: an, Val: val})
		}
	}
	return name, attrs
}

func scanEndTag(b []byte) string {
	if len(b) < 3 {
		return ""
	}
	i := 2 // skip '</'
	start := i
	for i < len(b) && !isSpace(b[i]) && b[i] != '>' {
		i++
	}
	return lowerASCII(b[start:i])
}

// render serializes the tree the way dom-serializer does with
// decodeEntities:false: text is written untouched and only the double quote is
// escaped inside attribute values.
func render(buf *bytes.Buffer, n *html.Node) {
	switch n.Type {
	case html.TextNode, html.DoctypeNode, html.CommentNode:
		// Comments hold their markup as htmlparser2 would serialize it.
		buf.WriteString(n.Data)
		return
	}

	if n.Type == html.ElementNode {
		buf.WriteByte('<')
		buf.WriteString(n.Data)
		for _, a := range n.Attr {
			buf.WriteByte(' ')
			buf.WriteString(a.Key)
			if a.Val != "" {
				buf.WriteString(`="`)
				writeAttrValue(buf, a.Val)
				buf.WriteByte('"')
			}
		}
		// Childless elements in foreign content (svg, math) close themselves.
		if n.Namespace != "" && n.FirstChild == nil {
			buf.WriteString("/>")
			return
		}
		buf.WriteByte('>')
		if voidElements[n.Data] {
			return
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		render(buf, c)
	}

	if n.Type == html.ElementNode {
		buf.WriteString("</")
		buf.WriteString(n.Data)
		buf.WriteByte('>')
	}
}

func writeAttrValue(buf *bytes.Buffer, v string) {
	for {
		i := strings.IndexByte(v, '"')
		if i < 0 {
			buf.WriteString(v)
			return
		}
		buf.WriteString(v[:i])
		buf.WriteString("&quot;")
		v = v[i+1:]
	}
}

func renderDocument(n *html.Node) []byte {
	var buf bytes.Buffer
	render(&buf, n)
	return buf.Bytes()
}

// --- small helpers over html.Node ---

func getAttr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func setAttr(n *html.Node, name, v string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr[i].Val = v
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: v})
}

func removeAttr(n *html.Node, name string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr = append(n.Attr[:i], n.Attr[i+1:]...)
			return
		}
	}
}

func detach(n *html.Node) {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

// walk visits n and its descendants in document order. Callers that remove
// nodes must collect them first: RemoveChild clears NextSibling, which would
// silently truncate the walk.
func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func findFirst(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findFirst(c, tag); f != nil {
			return f
		}
	}
	return nil
}
