package juicer

import "testing"

// TestRemoveIDs checks the hash against premailer's, Digest::SHA256.hexdigest("top").
func TestRemoveIDs(t *testing.T) {
	const top = "28720365c5e7476a011e4f43ac003ee5f16247a263b9d623aa85ed311d73bf39"
	got, err := mustNew(t, RemoveIDs(true)).Inline(`<a href="#top">x</a><p id="top">y</p><p id="other">z</p><area href="#top">`)
	if err != nil {
		t.Fatal(err)
	}
	want := `<a href="#` + top + `">x</a><p id="` + top + `">y</p><p>z</p><area href="#top">`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
