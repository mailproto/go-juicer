package juicer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzCSS checks two properties that do not need juice to verify: inlining
// never panics, and it is deterministic.
//
// Determinism is the one that earns its keep. The cascade depends on
// insertion order, so any accidental reliance on Go map iteration shows up
// here as two different outputs for one input, and nowhere else.
func FuzzCSS(f *testing.F) {
	for _, css := range seedCSS(f) {
		f.Add(css)
	}
	f.Add("p{color:red}")
	f.Add("@media screen{.a{color:blue!important}}")
	f.Add(":root{--a:var(--b);--b:red}p{color:var(--a)}")
	f.Add("p:not(.x),a:hover,td[width=600]{color:red}")

	const skeleton = `<table><tr><td class="c" id="i" width="600">` +
		`<p class="c x" style="margin:0"><span>t</span></p></td></tr></table>`

	in := New()
	f.Fuzz(func(t *testing.T, css string) {
		if len(css) > 8192 {
			t.Skip()
		}
		doc := []byte("<style>" + css + "</style>" + skeleton)
		a, errA := in.InlineBytes(doc)
		b, errB := in.InlineBytes(doc)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("nondeterministic error: %v vs %v", errA, errB)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("nondeterministic output for %q:\n  %q\n  %q", css, a, b)
		}
	})
}

// fuzzOptions maps each bit of a fuzzed mask to one boolean option, so the
// fuzzer explores option combinations alongside documents.
var fuzzOptions = []func(bool) Option{
	ApplyStyleTags, RemoveStyleTags, PreserveMediaQueries, PreserveFontFaces,
	PreserveKeyFrames, PreservePseudos, PreserveImportant, ApplyWidthAttributes,
	ApplyHeightAttributes, ApplyAttributesTableElements, ResolveCSSVariables,
	InlinePseudoElements, RemoveIDs, RemoveClasses, RemoveComments,
	ResetContentEditable, PreserveContainerQueries, PreserveLayers,
	InsertPreservedExtraCSS, XMLMode,
}

// FuzzDocument fuzzes whole documents under every option combination, which
// reaches the HTML parser, serializer and code-block masking that FuzzCSS
// never touches.
func FuzzDocument(f *testing.F) {
	for _, doc := range seedDocs(f) {
		f.Add(doc, uint32(0))
		f.Add(doc, uint32(1<<len(fuzzOptions)-1))
	}

	f.Fuzz(func(t *testing.T, doc string, mask uint32) {
		if len(doc) > 16384 {
			t.Skip()
		}
		var opts []Option
		for i, opt := range fuzzOptions {
			opts = append(opts, opt(mask&(1<<i) != 0))
		}
		in := New(opts...)
		a, errA := in.Inline(doc)
		b, errB := in.Inline(doc)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("nondeterministic error: %v vs %v", errA, errB)
		}
		if a != b {
			t.Fatalf("nondeterministic output for %q:\n  %q\n  %q", doc, a, b)
		}
	})
}

func seedDocs(f *testing.F) []string {
	f.Helper()
	var out []string
	root := filepath.Join("testdata", "in")
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".html") {
			if b, err := os.ReadFile(p); err == nil {
				out = append(out, string(b))
			}
		}
		return nil
	})
	return out
}

func seedCSS(f *testing.F) []string {
	f.Helper()
	var out []string
	root := filepath.Join("testdata", "in")
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		s := string(b)
		for {
			i := strings.Index(s, "<style")
			if i < 0 {
				return nil
			}
			j := strings.Index(s[i:], ">")
			k := strings.Index(s, "</style>")
			if j < 0 || k < 0 || i+j+1 > k {
				return nil
			}
			out = append(out, s[i+j+1:k])
			s = s[k+8:]
		}
	})
	return out
}
