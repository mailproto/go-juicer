package juicer_test

import (
	"fmt"

	"github.com/mailproto/go-juicer"
)

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
