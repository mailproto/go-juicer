package juicer

import "testing"

func TestAtRuleKind(t *testing.T) {
	for in, want := range map[string]string{
		"@media print":          "media",
		"@MEDIA print":          "MEDIA",
		"@container(min-width)": "container",
		"@-webkit-keyframes k":  "keyframes",
		"@-x-keyframes k":       "keyframes",
		"@KEYFRAMES k":          "KEYFRAMES",
		"@layer a, b":           "layer",
	} {
		if got := atRuleKind([]byte(in)); got != want {
			t.Errorf("atRuleKind(%q) = %q, want %q", in, got, want)
		}
	}
}
