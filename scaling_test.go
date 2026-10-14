package juicer

import (
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// TestScaling guards against work that grows faster than the input. Each
// shape is timed at n and 8n; linear work takes about 8 times as long, and
// quadratic work about 64 times, so the bound sits well between the two.
// Fuzzing never finds these: its inputs are a few kilobytes.
func TestScaling(t *testing.T) {
	shapes := []struct {
		name string
		n    int
		doc  func(n int) string
	}{
		{"template tags", 5000, func(n int) string { return strings.Repeat("<p>{{x}}</p>", n) }},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			small, large := fastest(t, s.doc(s.n)), fastest(t, s.doc(8*s.n))
			if ratio := float64(large) / float64(small); ratio > 24 {
				t.Errorf("8x the input took %.0fx the time (%v vs %v)", ratio, large, small)
			}
		})
	}
}

// TestDeepNesting inlines a tree far deeper than any stack-based walk could
// survive under a small stack limit. Unclosed tags nest without bound, and a
// goroutine stack overflow is fatal to the whole process, not a recoverable
// panic.
func TestDeepNesting(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(16 << 20))
	doc := "<style>b{color:red}</style>" + strings.Repeat("<b>", 500_000)
	out, err := mustNew(t).Inline(doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := 500_000; strings.Count(out, `style="color: red;"`) != want {
		t.Errorf("styled %d elements, want %d", strings.Count(out, `style="color: red;"`), want)
	}
}

// fastest is the quickest of three runs, to keep scheduler noise out.
func fastest(t *testing.T, doc string) time.Duration {
	in := mustNew(t)
	best := time.Duration(1<<63 - 1)
	for range 3 {
		start := time.Now()
		if _, err := in.Inline(doc); err != nil {
			panic(err)
		}
		best = min(best, time.Since(start))
	}
	return best
}
