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

func TestMaybeColor(t *testing.T) {
	for v, want := range map[string]bool{
		"#fff":                          true,
		"#F8F6F3":                       true,
		"#ffff":                         true,
		"#ff":                           false,
		"#ggg":                          false,
		"red":                           true,
		"rgb(250, 250, 250)":            true,
		"rgba(0,0,0,.5)":                true,
		"rgb(250 250 250 / 15%)":        true,
		"rgb(calc(100 + 1) 2 3)":        true,
		"HSL(1 2% 3%)":                  true,
		"rgb(111 222 / 1)":              false,
		"rgb(1, 2)":                     false,
		"rgb(1 2 3 4)":                  false,
		"rgb(1 2 3":                     false,
		"oklch(70% 0.1 200)":            true,
		"color-mix(in srgb, red, blue)": true,
	} {
		if got := maybeColor(v); got != want {
			t.Errorf("maybeColor(%q) = %v, want %v", v, got, want)
		}
	}
}
