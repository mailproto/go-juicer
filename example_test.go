package juicer_test

import (
	"errors"
	"fmt"

	"github.com/mailproto/go-juicer"
)

func Example() {
	out, err := juicer.Inline(`<style>p{color:red} .lead{font-size:18px}</style><p class="lead">Hi</p>`)
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output:
	// <p class="lead" style="color: red; font-size: 18px;">Hi</p>
}

func ExampleNew() {
	// An Inliner is safe for concurrent use; build one and reuse it.
	in, err := juicer.New(juicer.PreserveImportant(true))
	if err != nil {
		panic(err)
	}
	for _, doc := range []string{
		`<style>a{color:red !important}</style><a>x</a>`,
		`<style>td{padding:0}</style><table><tr><td>y</td></tr></table>`,
	} {
		out, err := in.Inline(doc)
		if err != nil {
			panic(err)
		}
		fmt.Println(out)
	}
	// Output:
	// <a style="color: red !important;">x</a>
	// <table><tr><td style="padding: 0;">y</td></tr></table>
}

func ExampleExtraCSS() {
	// Rules that cannot be inlined, such as @media, go into a new <style>.
	out, err := juicer.Inline(`<html><head></head><body><p>Hi</p></body></html>`,
		juicer.ExtraCSS(`p{margin:0} @media (max-width:600px){p{margin:8px}}`))
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output:
	// <html><head><style>
	// @media (max-width:600px) {
	//   p {
	//     margin: 8px;
	//   }
	// }
	// </style></head><body><p style="margin: 0;">Hi</p></body></html>
}

func ExampleCompatibilityMode() {
	// Browsers score :first-child as a class, so it beats .x; juice scores it
	// as a type, so .x wins.
	doc := `<style>p:first-child{color:red} .x{color:blue}</style><div><p class="x">Hi</p></div>`
	for _, opts := range [][]juicer.Option{nil, {juicer.CompatibilityMode(juicer.Juice12)}} {
		out, err := juicer.Inline(doc, opts...)
		if err != nil {
			panic(err)
		}
		fmt.Println(out)
	}
	// Output:
	// <div><p class="x" style="color: red;">Hi</p></div>
	// <div><p class="x" style="color: blue;">Hi</p></div>
}

func ExampleMaxInputSize() {
	_, err := juicer.Inline(`<style>p{color:red}</style><p>Hi</p>`, juicer.MaxInputSize(16))
	fmt.Println(errors.Is(err, juicer.ErrLimitExceeded))
	// Output:
	// true
}

func ExampleInlinePseudoElements() {
	out, err := juicer.Inline(`<style>a::after{content:" →";color:gray}</style><a href="#">More</a>`,
		juicer.InlinePseudoElements(true))
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output:
	// <a href="#">More<span style="color: gray;"> →</span></a>
}

func ExampleCodeBlocks() {
	blocks := append(juicer.DefaultCodeBlocks(), juicer.CodeBlock{Start: "[[", End: "]]"})
	out, err := juicer.Inline(
		`<style>td{color:red}</style><td [[ if .VIP ]]class="vip"[[ end ]]>{{ name }}</td>`,
		juicer.CodeBlocks(blocks),
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output:
	// <td [[ if .VIP ]]class="vip" [[ end ]] style="color: red;">{{ name }}</td>
}
