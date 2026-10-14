package juicer

import (
	"github.com/mailproto/go-juicer/internal/htmlparser2"
	"golang.org/x/net/html"
)

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
	htmlparser2.Traverse(n, func(n *html.Node) bool { fn(n); return true }, func(*html.Node) {})
}

func findFirst(root *html.Node, tag string) *html.Node {
	var found *html.Node
	htmlparser2.Traverse(root, func(n *html.Node) bool {
		if found == nil && n.Type == html.ElementNode && n.Data == tag {
			found = n
		}
		return found == nil
	}, func(*html.Node) {})
	return found
}
