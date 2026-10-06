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

Typically several to tens of times faster than juice, depending on the
document. Current numbers come from CI rather than this file: every push to
main posts a table of this package against juice, on the same documents and
the same runner, to its job summary. `make bench-report` produces the same
table locally.

The documents are `BenchmarkInline`'s 99 KB marketing email and
`BenchmarkShapes`, which includes shapes chosen to be unfavourable here;
`TestShapesAgree` asserts the Go and Node generators emit identical bytes.
The narrowest margins are on shapes dominated by per-element work rather than
matching, where the rule index has little to offer. The widest is the large
newsletter, juice's O(rules × nodes) showing up. The attribute-only shape is
deliberately hostile: nothing can be bucketed by id, class or tag, so the
index degenerates to brute force plus merge overhead.

Most of the gap is selector matching. juice runs one full document traversal
per rule; here rules are bucketed by the id, class or tag of their rightmost
compound, so a node only tests rules it can actually reach.

Regressions: every pull request gets an advisory `benchstat` of the base
branch against the change, run alternately on one runner (`make bench-compare
BASE=<ref>` locally). Shared runners are too noisy for timing to fail a build,
so the gate is allocations instead: `TestAllocBudget` fails when any
benchmark document allocates past its budget.

## Correctness

juice itself is the oracle. `testdata/in` holds inputs, and
`testdata/out/<target>/<variant>` holds juice's output for each of them under
every pinned juice version (`juice-12`, `juice-9`) and every option set in
`testdata/variants.json`. The suite asserts **raw byte equality with no
normalization**, prints a parity percentage per target on every run, and
`testdata/skip/<target>.txt` lists anything that does not match.

| target | cases | match | skipped |
|---|---|---|---|
| juice 12.1.3 | 3451 | 3302 | 149, each with its reason in `testdata/skip/juice-12.txt` |
| juice 9.1.0 | 3451 | 3039 | 412: the same, plus behaviour juice 12 changed |

`testdata/in/juice-suite`, `testdata/in/premailer-suite` and
`testdata/in/css-inline-suite` hold the inputs from the test suites of juice,
premailer and the Rust css-inline crate, so passing those is part of the same
check. All are checked against juice's output: premailer re-serializes through
libxml2 and css-inline parses to the HTML5 spec, so neither can be compared
byte for byte.

premailer is checked by meaning instead. `testdata/out/premailer` holds the
Ruby gem's output for every fixture (pinned in
`testdata/oracle/premailer/Gemfile.lock`; `make premailer-outputs`), and
`TestPremailerSemantics` parses it and this package's output with the same
HTML5 parser, then compares every element's declarations after folding
together what does not change rendering: presentational attributes, shorthand
splits, quotes and color spellings. 419 of 491 documents agree; the rest are
listed with their reason in `testdata/skip/premailer.txt`, mostly premailer's
own behaviour (its libxml2 tree, no `:is()`, lossy shorthand handling).

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

`FuzzCSS` (stylesheets) and `FuzzDocument` (whole documents under every
combination of boolean options) check that inlining never panics and is
deterministic. Determinism is the one that earns its keep — the cascade
depends on insertion order, so any accidental reliance on Go map iteration
shows up there and nowhere else. CI runs each for 30 seconds on every change
and 5 minutes nightly.

`make differential SEED=n COUNT=n` generates random email-like documents
(`testdata/oracle/differential.mjs`), inlines each with juice under random
options, and requires byte-identical output from this package. A mismatch is
minimized against a live juice process before it is reported, so it arrives
as a few bytes of HTML ready to become a fixture. CI runs 2,000 documents on
every change and 50,000 with a fresh seed nightly; the generator avoids the
deliberate divergences below, so any mismatch is a bug.

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

Five cases where matching juice would mean matching a bug. The first three
are covered by tests in `divergence_test.go`, the rest by `ALLOW` fixtures.

1. **Liquid templates.** juice's default `codeBlocks` covers Handlebars and
   EJS but not Liquid's `{% %}`, so it corrupts
   `<td {% if x %}class="a"{% endif %}>` into `class="a" endif %}`. Liquid is
   in the default set here.
2. **Cyclic custom properties.** `--a:var(--b);--b:var(--a)` recurses until
   juice's stack overflows, which makes a cyclic stylesheet a denial of
   service. Resolution here is depth-capped.
3. **Malformed style attributes.** juice throws, failing the whole document,
   on a `style` attribute postcss cannot parse: `ttt { 123 }`, `----`, or
   `font-family:&quot;A B&quot;` (`decodeStyleAttributes` defaults off, so the
   raw entity reaches postcss). This package inlines the rest of the document.
4. **HTML comment markers around a stylesheet.** Old email templates wrap
   CSS as `<style><!-- p { color: red } --></style>`. CSS ignores `<!--` and
   `-->` there, so browsers and premailer apply the rule; juice reads
   `<!-- p` as a selector and silently drops it. This package applies it.
   premailer's own `test_commented_out_styles_in_the_body` asserts the same.
5. **Each `<style>` block is its own stylesheet.** juice concatenates every
   block before parsing, so an unclosed `@media` in one swallows the rules of
   the next and they are lost. Browsers parse each block separately, and so
   does this package.

## Status

The cascade, at-rule preservation, attribute promotion, CSS variables,
Selectors L4 specificity and `:is()`/`:where()` are implemented. Not yet:
CSS nesting flattening, `::before`/`::after` materialization (off by default
in juice too), and `/* juice ignore */` directives.

External resources are out of scope. This package does no network I/O; resolve
`<link rel=stylesheet>` yourself and pass the CSS via `ExtraCSS`.
