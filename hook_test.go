package juicer

import (
	"reflect"
	"testing"
)

func TestElementHook(t *testing.T) {
	var got []Element
	hook := ElementHook(func(el Element) []Attribute {
		got = append(got, el)
		return []Attribute{{"data-seen", el.Tag}}
	})
	out, err := mustNew(t, hook).Inline(`<style>td{background:url("a.png")}</style><td>x</td><p style="color:red">y</p><b>z</b>`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Element{
		// Values keep the stylesheet's double quotes, which the style attribute swaps out.
		{Tag: "td", Attrs: []Attribute{{"style", "background: url('a.png');"}}, Declarations: []Declaration{{"background", `url("a.png")`}}},
		// No rule matched: the style attribute is as written.
		{Tag: "p", Attrs: []Attribute{{"style", "color:red"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hook saw %+v\nwant %+v", got, want)
	}
	if wantOut := `<td style="background: url('a.png');" data-seen="td">x</td><p style="color:red" data-seen="p">y</p><b>z</b>`; out != wantOut {
		t.Errorf("got  %s\nwant %s", out, wantOut)
	}
}
