package juicer

import (
	"strings"
	"testing"
	"time"
)

// Behaviours that cannot be golden-tested because juice cannot produce an
// expectation for them: it crashes, hangs, or corrupts the input. Each is a
// deliberate divergence and is listed in the README.

func TestVariableCycleTerminates(t *testing.T) {
	// juice recurses until the stack overflows on this input, which makes a
	// cyclic stylesheet a denial of service. Resolution here is depth-capped.
	const doc = `<style>:root{--a:var(--b);--b:var(--a)}p{color:var(--a)}</style><p>x</p>`
	done := make(chan string, 1)
	go func() {
		out, err := Inline(doc)
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- out
	}()
	select {
	case got := <-done:
		if !strings.Contains(got, "<p") {
			t.Fatalf("unexpected output: %q", got)
		}
		t.Logf("terminated with %q", got)
	case <-timeoutAfterSeconds(10):
		t.Fatal("var() resolution did not terminate on a cyclic stylesheet")
	}
}

func TestEncodedQuotesInStyleAttribute(t *testing.T) {
	// juice throws a CssSyntaxError here: decodeStyleAttributes defaults off,
	// so the raw &quot; reaches postcss. Refusing an ordinary email is worse
	// than doing something reasonable with it.
	const doc = `<style>p{color:red}</style><p style="font-family:&quot;A B&quot;">x</p>`
	got, err := Inline(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "color: red") {
		t.Errorf("stylesheet rule was not applied: %q", got)
	}
	if !strings.Contains(got, "font-family") {
		t.Errorf("existing declaration was dropped: %q", got)
	}
}

func TestLiquidSurvivesRoundTrip(t *testing.T) {
	// juice's default codeBlocks cover Handlebars and EJS but not Liquid's
	// {% %}, so juice mangles tag-position Liquid into `class="a" endif %}`.
	cases := []string{
		`{{ user.name }}`,
		`{% if x %}`,
		`{% for a in b %}{{ a }}{% endfor %}`,
	}
	for _, frag := range cases {
		doc := `<style>p{color:red}</style><p title="t">` + frag + `</p>`
		got, err := Inline(doc)
		if err != nil {
			t.Fatalf("%s: %v", frag, err)
		}
		if !strings.Contains(got, frag) {
			t.Errorf("Liquid did not survive: %q became %q", frag, got)
		}
	}

	// Tag position is the case juice gets wrong.
	doc := `<style>td{color:red}</style><td {% if x %}class="a"{% endif %}>y</td>`
	got, err := Inline(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"{% if x %}", "{% endif %}"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	in := mustNew(t)
	doc := benchDoc()
	want, err := in.Inline(doc)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			got, err := in.Inline(doc)
			if err != nil {
				errs <- err
				return
			}
			if got != want {
				errs <- errDiff
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Inline diverged: %v", err)
		}
	}
}

var errDiff = errString("output differed between goroutines")

type errString string

func (e errString) Error() string { return string(e) }

func timeoutAfterSeconds(n int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(n) * time.Second)
		close(ch)
	}()
	return ch
}
