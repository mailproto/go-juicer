// Package others benchmarks other Go CSS inliners on the documents
// testdata/oracle/benchreport.mjs writes to JUICER_BENCH_DOCS. It is a
// separate module so their dependencies stay out of go-juicer's.
package others

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aymerick/douceur/inliner"
	"github.com/vanng822/go-premailer/premailer"
)

var inliners = []struct {
	name   string
	inline func(string) (string, error)
}{
	{"go-premailer", func(doc string) (string, error) {
		p, err := premailer.NewPremailerFromString(doc, premailer.NewOptions())
		if err != nil {
			return "", err
		}
		return p.Transform()
	}},
	{"douceur", inliner.Inline},
}

func BenchmarkOthers(b *testing.B) {
	dir := os.Getenv("JUICER_BENCH_DOCS")
	if dir == "" {
		b.Skip("set JUICER_BENCH_DOCS to the documents `make bench-report` writes")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil || len(files) == 0 {
		b.Fatalf("no documents in %s", dir)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}
		doc := string(src)
		name := strings.TrimSuffix(filepath.Base(f), ".html")
		for _, in := range inliners {
			b.Run(in.name+"/"+name, func(b *testing.B) {
				if _, err := in.inline(doc); err != nil {
					b.Skipf("fails on this document: %v", err)
				}
				b.SetBytes(int64(len(doc)))
				b.ReportAllocs()
				for b.Loop() {
					in.inline(doc)
				}
			})
		}
	}
}
