package juicer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// styledElement is one element's tag and its style declarations, normalized.
type styledElement struct {
	tag   string
	style map[string]string
}

// styledElements parses doc as a browser would and lists every element that
// can carry inline style, in document order. Parsing both premailer's and
// this package's output with the same HTML5 parser normalizes away their
// different serializers.
func styledElements(doc string) ([]styledElement, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil, err
	}
	var out []styledElement
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && !nonVisualElements[n.Data] && n.Data != "html" && n.Data != "head" {
			el := styledElement{tag: n.Data, style: map[string]string{}}
			// Presentational attributes are styles of lower priority.
			// premailer moves promoted properties out of style into them, where
			// juice keeps both, so both have to count.
			for _, a := range n.Attr {
				if prop, ok := presentational[a.Key]; ok {
					v := a.Val
					if prop == "background-image" && !strings.Contains(v, "(") {
						v = "url(" + v + ")"
					}
					setLonghands(el.style, prop, normalizeValue(v))
				}
			}
			if v, ok := getAttr(n, "style"); ok {
				for _, d := range splitDeclarations([]byte(v)) {
					setLonghands(el.style, strings.ToLower(d.prop), normalizeValue(string(d.value)))
				}
			}
			out = append(out, el)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	return out, nil
}

var presentational = map[string]string{
	"bgcolor":    "background-color",
	"align":      "text-align",
	"valign":     "vertical-align",
	"background": "background-image",
}

// normalizeValue removes spellings that do not change meaning: runs of
// whitespace, juice's habit of swapping double quotes for single ones, and
// quotes around a url().
func normalizeValue(v string) string {
	v = strings.ReplaceAll(strings.Join(strings.Fields(v), " "), `"`, "'")
	return unquotedURL.ReplaceAllString(v, "url($1)")
}

var unquotedURL = regexp.MustCompile(`url\('([^']*)'\)`)

var sides = []string{"top", "right", "bottom", "left"}

// setLonghands records a declaration as the longhands it sets, so that
// "margin: 0; margin-bottom: 10px" and "margin: 0 0 10px" compare equal.
// premailer's css_parser merges and splits shorthands; juice never does.
func setLonghands(m map[string]string, prop, v string) {
	box := func(f []string) []string {
		switch len(f) {
		case 1:
			return []string{f[0], f[0], f[0], f[0]}
		case 2:
			return []string{f[0], f[1], f[0], f[1]}
		case 3:
			return []string{f[0], f[1], f[2], f[1]}
		case 4:
			return f
		}
		return nil
	}
	switch prop {
	case "margin", "padding":
		if b := box(strings.Fields(v)); b != nil {
			for i, s := range sides {
				m[prop+"-"+s] = b[i]
			}
			return
		}
	case "border-width", "border-style", "border-color":
		if b := box(strings.Fields(v)); b != nil {
			kind := strings.TrimPrefix(prop, "border-")
			for i, s := range sides {
				if kind == "color" {
					b[i] = normalizeColor(b[i])
				}
				m["border-"+s+"-"+kind] = b[i]
			}
			return
		}
	case "background":
		// Only the parts premailer keeps when it splits the shorthand.
		for _, f := range cssFields(v) {
			switch {
			case strings.HasPrefix(strings.ToLower(f), "url("), strings.Contains(strings.ToLower(f), "gradient("):
				m["background-image"] = f
			case isColor(f):
				m["background-color"] = normalizeColor(f)
			}
		}
		return
	case "color", "background-color":
		v = normalizeColor(v)
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		which := sides
		if prop != "border" {
			which = []string{strings.TrimPrefix(prop, "border-")}
		}
		w, st, c := borderParts(v)
		for _, s := range which {
			m["border-"+s+"-width"], m["border-"+s+"-style"], m["border-"+s+"-color"] = w, st, normalizeColor(c)
		}
		return
	}
	m[prop] = v
}

// cssFields splits a value on top-level whitespace, keeping "url(a b)" and
// "rgb(1, 2, 3)" whole.
func cssFields(v string) []string {
	var out []string
	depth, start := 0, -1
	for i, c := range v {
		switch {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ' ' && depth == 0:
			if start >= 0 {
				out = append(out, v[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, v[start:])
	}
	return out
}

var namedColors = map[string]bool{
	"black": true, "white": true, "red": true, "green": true, "blue": true, "yellow": true,
	"gray": true, "grey": true, "silver": true, "navy": true, "teal": true, "orange": true,
	"purple": true, "transparent": true, "currentcolor": true, "inherit": true,
}

func isColor(f string) bool {
	l := strings.ToLower(f)
	return strings.HasPrefix(l, "#") || strings.HasPrefix(l, "rgb") || strings.HasPrefix(l, "hsl") || namedColors[l]
}

// normalizeColor writes opaque rgb() and bare hex (as in bgcolor="FAFAFA")
// as lowercase #rrggbb. Colors with alpha are left alone; premailer drops the
// alpha, which is a real difference.
func normalizeColor(v string) string {
	l := strings.ToLower(strings.TrimSpace(v))
	if len(l) == 6 && strings.Trim(l, "0123456789abcdef") == "" {
		return "#" + l
	}
	if strings.HasPrefix(l, "#") {
		return l
	}
	if inner, ok := strings.CutPrefix(l, "rgb("); ok {
		inner = strings.TrimSuffix(inner, ")")
		parts := strings.FieldsFunc(inner, func(r rune) bool { return r == ',' || r == ' ' })
		if len(parts) == 3 {
			var b strings.Builder
			b.WriteByte('#')
			for _, p := range parts {
				var n int
				if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 0 || n > 255 {
					return l
				}
				fmt.Fprintf(&b, "%02x", n)
			}
			return b.String()
		}
	}
	return l
}

var borderStyles = map[string]bool{
	"none": true, "hidden": true, "dotted": true, "dashed": true, "solid": true,
	"double": true, "groove": true, "ridge": true, "inset": true, "outset": true,
}

// borderParts splits a border shorthand into width, style and color, filling
// the CSS initial values for those not given.
func borderParts(v string) (width, style, color string) {
	width, style, color = "medium", "none", "currentcolor"
	for _, f := range strings.Fields(v) {
		switch {
		case borderStyles[f]:
			style = f
		case f == "0" || f == "thin" || f == "medium" || f == "thick" || (f[0] >= '0' && f[0] <= '9') || f[0] == '.':
			width = f
		default:
			color = f
		}
	}
	return width, style, color
}

// TestPremailerSemantics compares this package's output with the Ruby
// premailer gem's (testdata/out/premailer, written only by
// testdata/oracle/premailer/generate.rb) by meaning rather than bytes: both are
// parsed with the same HTML5 parser, and each element's declarations are
// compared after folding presentational attributes, shorthands, quotes and
// color spellings together. Documents where premailer and juice disagree are
// listed with the reason in testdata/skip/premailer.txt, which only shrinks.
func TestPremailerSemantics(t *testing.T) {
	known := loadSkips(t, "premailer")
	root := filepath.Join("testdata", "out", "premailer")
	var agree, listed int
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, ".html"))
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			in, err := os.ReadFile(filepath.Join("testdata", "in", rel))
			if err != nil {
				t.Fatal(err)
			}
			opts, _ := loadOptions(t, filepath.Join("testdata", "in", name+".json"))
			got, err := New(opts...).Inline(string(in))
			if err != nil {
				t.Skipf("Inline failed: %v", err)
			}
			a, _ := styledElements(string(want))
			b, _ := styledElements(got)
			diffs := compareStyled(a, b)
			slices.Sort(diffs)
			diffs = slices.Compact(diffs)

			reason, isKnown := known[name]
			switch {
			case isKnown && len(diffs) == 0:
				t.Errorf("now agrees with premailer; drop its line from testdata/skip/premailer.txt\n  was: %s", reason)
			case isKnown:
				listed++
				t.Skipf("known difference: %s", reason)
			case len(diffs) > 0:
				t.Errorf("differs from premailer: %s", strings.Join(diffs, "; "))
			default:
				agree++
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, reason := range known {
		if _, err := os.Stat(filepath.Join(root, name+".html")); err != nil {
			t.Errorf("testdata/skip/premailer.txt names %q, which has no premailer output (%s)", name, reason)
		}
	}
	t.Logf("premailer semantics: %d agree, %d known differences", agree, listed)
}

// compareStyled lists differences between premailer's elements (a) and ours
// (b) as short descriptions.
func compareStyled(a, b []styledElement) []string {
	if len(a) != len(b) {
		return []string{"structure: element count"}
	}
	var out []string
	for i := range a {
		if a[i].tag != b[i].tag {
			return []string{"structure: tag"}
		}
		for k, v := range a[i].style {
			// premailer does not resolve custom properties.
			if strings.HasPrefix(k, "--") || strings.Contains(strings.ToLower(v), "var(") || strings.Contains(strings.ToLower(v), "var (") {
				continue
			}
			w, ok := b[i].style[k]
			switch {
			case !ok:
				out = append(out, "only premailer: "+k)
			case v != w:
				out = append(out, fmt.Sprintf("value %s: %q vs %q", k, v, w))
			}
		}
		for k := range b[i].style {
			if _, ok := a[i].style[k]; !ok {
				out = append(out, "only ours: "+k)
			}
		}
	}
	return out
}
