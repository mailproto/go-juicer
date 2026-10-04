# go-juicer

A Go CSS inliner for HTML email, matching the behaviour of
[juice](https://github.com/Automattic/juice) v12.

```go
out, err := juicer.Inline(`<style>p{color:red}</style><p>hi</p>`)
// <p style="color: red;">hi</p>
```

Email clients strip `<style>` blocks, so CSS has to be moved into `style`
attributes before sending. juice is the reference implementation the
JavaScript ecosystem uses; this ports its semantics to Go.

## Performance

On a 99 KB table-based marketing email (1,500 elements, 185 selectors),
Apple M4 Max, Go 1.26 / Node 22.22:

| | ms/doc | |
|---|---|---|
| **this package** | **2.0** | |
| Rust `css-inline` 0.21 | 4.8 | measured previously, same document |
| `vanng822/go-premailer` | 7.3 | |
| Node `juice` 12.1.3 | 28.4 | |

One document proves little on its own, so `BenchmarkShapes` covers seven,
including shapes chosen to be unfavourable here. Same machine, and
`TestShapesAgree` asserts the Go and Node generators emit identical bytes:

| shape | bytes | this | juice | |
|---|---|---|---|---|
| 1 rule, 1 element | 41 | 0.002 | 0.077 | 40x |
| transactional, 14 rules | 1.1 K | 0.032 | 0.342 | 11x |
| 800 rules, 2 elements | 21 K | 0.554 | 6.06 | 11x |
| 2 rules, 9k elements | 95 K | 2.85 | 12.5 | 4.4x |
| 120 attribute-only selectors | 12 K | 0.656 | 5.64 | 8.6x |
| 60 deep descendant chains | 12 K | 1.24 | 5.68 | 4.6x |
| 200 rules, 16k elements | 291 K | 6.00 | 194 | 32x |

No shape tested is faster under juice; the narrowest margin is 4.4x. The two
narrow cases are the ones dominated by per-element work rather than matching,
where the rule index has little to offer. The widest is the large newsletter,
which is juice's O(rules x nodes) showing up: 200 rules across 16k elements.

The "attribute-only selectors" row is deliberately hostile to the design here
— nothing can be bucketed by id, class or tag, so the index degenerates to
brute force plus merge overhead — and it still comes out ahead.

Most of the gap is selector matching: juice runs one full document traversal
per rule. Here rules are bucketed by the id, class or tag of their rightmost
compound, so a node only tests rules it can actually reach. That change alone
took the 99 KB document from 3.3 ms to 1.9 ms.

Not measured: peak memory, and process startup (which would favour Go further,
since Node costs ~35 ms before juice runs at all).

`make bench` runs the Go side and `make bench-node` runs juice over the same
input.

## Correctness

juice itself is the oracle. `testdata/in` holds inputs, and
`testdata/out/<target>/<variant>` holds juice's output for each of them under
every pinned juice version (`juice-12`, `juice-9`) and every option set in
`testdata/variants.json`. The suite asserts **raw byte equality with no
normalization**, prints a parity percentage per target on every run, and
`testdata/skip/<target>.txt` lists anything that does not match.

| target | cases | match | skipped |
|---|---|---|---|
| juice 12.1.3 | 1386 | 1287 | 99, each with its reason in `testdata/skip/juice-12.txt` |
| juice 9.1.0 | 1386 | 1102 | 284: the same, plus behaviour juice 12 changed |

`testdata/in/juice-suite` holds the inputs from juice's own test suite, so
passing it there is part of the same check.

juice 12 is what this package implements. juice 9 is tracked because it is
still widely deployed, and its list shows exactly where the two disagree.
juice 9 is pinned with cheerio 1.0.0-rc.12, the release it was built against;
its `^1.0.0-rc.12` range now resolves to cheerio 1.x, whose serializer differs.

```
make test             # parity suite, race detector on
make goldens-verify   # committed goldens still match pinned juice
make goldens          # regenerate after a deliberate juice upgrade
```

Goldens are written only by `testdata/oracle/generate.mjs`. There is no
`-update` flag on the Go side: regenerating expectations from the code under
test would certify the implementation against itself.

`FuzzCSS` checks that inlining never panics and is deterministic. Determinism
is the one that earns its keep — the cascade depends on insertion order, so
any accidental reliance on Go map iteration shows up there and nowhere else.

### Why the HTML parser is not `html.Parse`

Byte-exactness rules out `x/net/html`'s `Parse` and `Render`. juice parses
with htmlparser2 in non-XML mode, which performs no HTML5 tree construction
and leaves entities undecoded, so a spec-compliant round trip diverges on
almost every real document:

| input | juice | `x/net/html` |
|---|---|---|
| `<table><tr><td>x` | unchanged | inserts `<tbody>` |
| `<p>x</p>` | unchanged | adds `<html><head></head><body>` |
| `<br>` | unchanged | `<br/>` |
| `a &nbsp; &copy;` | unchanged | `&nbsp;` becomes U+00A0 |
| `href="?a=1&b=2"` | unchanged | `&` becomes `&amp;` |
| `<p class="">` | `<p class>` | `class=""` |

A mail client can see all of those. `parse.go` builds an htmlparser2-shaped
tree over `html.NewTokenizer` instead, reading raw token bytes rather than the
decoding accessors. It is also the faster path: the tokenizer is about twice
as quick as the full parser, and nothing is escaped on the way out.

## Deliberate divergences from juice

Three cases where matching juice would mean matching a bug. Each is covered by
a test in `divergence_test.go`.

1. **Liquid templates.** juice's default `codeBlocks` covers Handlebars and
   EJS but not Liquid's `{% %}`, so it corrupts
   `<td {% if x %}class="a"{% endif %}>` into `class="a" endif %}`. Liquid is
   in the default set here.
2. **Cyclic custom properties.** `--a:var(--b);--b:var(--a)` recurses until
   juice's stack overflows, which makes a cyclic stylesheet a denial of
   service. Resolution here is depth-capped.
3. **Encoded quotes in a style attribute.** juice throws a `CssSyntaxError` on
   `style="font-family:&quot;A B&quot;"`, because `decodeStyleAttributes`
   defaults off and the raw entity reaches postcss. This package inlines it.

## Status

The cascade, at-rule preservation, attribute promotion, CSS variables,
Selectors L4 specificity and `:is()`/`:where()` are implemented. Not yet:
CSS nesting flattening, `::before`/`::after` materialization (off by default
in juice too), and `/* juice ignore */` directives.

External resources are out of scope. This package does no network I/O; resolve
`<link rel=stylesheet>` yourself and pass the CSS via `ExtraCSS`.
