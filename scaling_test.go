package juicer

import (
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
			small, large := fastest(s.doc(s.n)), fastest(s.doc(8*s.n))
			if ratio := float64(large) / float64(small); ratio > 24 {
				t.Errorf("8x the input took %.0fx the time (%v vs %v)", ratio, large, small)
			}
		})
	}
}

// fastest is the quickest of three runs, to keep scheduler noise out.
func fastest(doc string) time.Duration {
	in := New()
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
