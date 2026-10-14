package juicer

import (
	"errors"
	"testing"
)

func TestErrJuiceFails(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		opts      []Option
		juice     bool
	}{
		{"juice 9 nested rule", `<style>.a{color:red;.b{margin:0}}</style><p class="a">x</p>`, []Option{CompatibilityMode(Juice9)}, true},
		{"juice 9 style attribute", `<style>p{color:red}</style><p style="a{">x</p>`, []Option{CompatibilityMode(Juice9)}, true},
		{"unreadable content", `<style>p::before{content:var(--missing)}</style><p>x</p>`, []Option{InlinePseudoElements(true)}, true},
		{"bad option", `<style>@media print{p{color:red}}</style><p>x</p>`, []Option{ExtraCSS("@media print{p{color:red}}"), InsertPreservedExtraCSSInto("p[")}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(c.opts...).Inline(c.doc)
			if err == nil {
				t.Fatal("no error")
			}
			if errors.Is(err, ErrJuiceFails) != c.juice {
				t.Errorf("errors.Is(%v, ErrJuiceFails) = %v, want %v", err, !c.juice, c.juice)
			}
		})
	}
}
