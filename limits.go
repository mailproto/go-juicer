package juicer

import (
	"fmt"
	"strings"

	"github.com/mailproto/go-juicer/internal/htmlparser2"
	"golang.org/x/net/html"
)

// Limits on the input, so a hostile or broken document costs bounded time
// and memory. Unclosed tags nest without end, and matching a descendant
// selector walks every ancestor of every element, so depth multiplies the
// work.

const (
	defaultMaxInputSize = 16 << 20
	defaultMaxDepth     = 1024
)

// MaxInputSize limits the document Inline accepts, in bytes. Larger input
// fails with an error wrapping ErrLimitExceeded. The default is 16 MiB; 0
// removes the limit.
func MaxInputSize(n int) Option { return func(o *options) { o.maxInputSize = n } }

// MaxDepth limits how deeply elements, and the blocks and parentheses of a
// stylesheet, may nest. A deeper document fails with an error wrapping
// ErrLimitExceeded. The default is 1024; 0 removes the limit.
func MaxDepth(n int) Option { return func(o *options) { o.maxDepth = n } }

func checkSize(doc []byte, o *options) error {
	if o.maxInputSize > 0 && len(doc) > o.maxInputSize {
		return fmt.Errorf("%w: the document is %d bytes, over MaxInputSize %d", ErrLimitExceeded, len(doc), o.maxInputSize)
	}
	return nil
}

// checkDepth measures element nesting, and the nesting of every stylesheet
// and style attribute the parsers would read. Stylesheets are measured
// joined, as compatibility modes read them.
func checkDepth(root *html.Node, o *options) error {
	if o.maxDepth <= 0 {
		return nil
	}
	depth, deepest := 0, 0
	var css strings.Builder
	css.WriteString(o.extraCSS)
	htmlparser2.Traverse(root, func(n *html.Node) bool {
		if n.Type != html.ElementNode {
			return true
		}
		depth++
		deepest = max(deepest, depth)
		if n.Data == "style" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			css.WriteString(n.FirstChild.Data)
		}
		if v, ok := getAttr(n, o.styleAttributeName); ok {
			deepest = max(deepest, blockDepth(v))
		}
		return true
	}, func(n *html.Node) {
		if n.Type == html.ElementNode {
			depth--
		}
	})
	deepest = max(deepest, blockDepth(css.String()))
	if deepest > o.maxDepth {
		return fmt.Errorf("%w: the document nests %d deep, over MaxDepth %d", ErrLimitExceeded, deepest, o.maxDepth)
	}
	return nil
}

// blockDepth is how deeply braces and parentheses nest in css, together.
// Selectors nest in parentheses, as in :is(:not(a)). Those in strings and
// comments count too, which can only overstate it.
func blockDepth(css string) int {
	depth, deepest := 0, 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{', '(':
			depth++
			deepest = max(deepest, depth)
		case '}', ')':
			depth = max(depth-1, 0)
		}
	}
	return deepest
}
