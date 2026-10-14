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

## Limits

A document over `MaxInputSize` (16 MiB by default), or nesting elements or
stylesheet blocks deeper than `MaxDepth` (1024 by default), fails with an
error wrapping `ErrLimitExceeded` instead of being inlined. Unclosed tags nest
without end, and a descendant selector walks every ancestor of every element,
so depth multiplies the work; at the defaults the worst case found, 16 MiB of
1000-deep chains under a descendant selector, takes about two seconds on a
laptop. `TestScaling` fails when work grows faster than the input. Set either
limit to 0 to remove it.

## Correctness

juice itself is the oracle. `testdata/in` holds inputs, and
`testdata/out/<target>/<variant>` holds juice's output for each of them under
every pinned juice version (`juice-12`, `juice-9`) and every option set in
`testdata/variants.json`. The suite asserts **raw byte equality with no
normalization**, prints a parity percentage per target on every run, and
`testdata/skip/<target>.txt` lists anything that does not match.

| target | cases | match | skipped |
|---|---|---|---|
| juice 12.1.3, with `CompatibilityMode(Juice12)` | 4074 | 4025 | 49: juice throwing on malformed style attributes (divergence 3 below), in `testdata/skip/juice-12.txt` |
| juice 12.1.3, default mode | 4074 | 3943 | 131: the same 49, plus 82 where the default follows the browser (`testdata/skip/default.txt`) |
| juice 9.1.0, with `CompatibilityMode(Juice9)` | 4074 | 4074 | none |

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
splits, quotes and color spellings. 457 of 580 documents agree; the rest are
listed with their reason in `testdata/skip/premailer.txt`, mostly premailer's
own behaviour (its libxml2 tree, no `:is()`, lossy shorthand handling).

Finally, a browser judges whether inlining preserved the cascade at all.
`make cascade` (`TestCascade`, `testdata/oracle/cascade.mjs`) renders every
fixture in headless Chrome twice, as written and as inlined, and compares
every element's computed style. 548 of 582 render identically. The rest are
listed with their reason in `testdata/skip/cascade.txt`. Most are juice bugs
this package still copies for parity: `:first-child` seeing a tree without `<style>` elements,
`[width=600]` and `*a` applied though browsers drop them, `content` on
`<img>`, pseudo-class rules dropped from `extraCss`, `@supports` blocks
dropped, and a few more. Each is a candidate for a deliberate divergence.

juice 12 is what this package implements by default, except where juice
disagrees with how browsers read the CSS: there the default follows the
browser, and `testdata/skip/default.txt` lists each case.
`CompatibilityMode(Juice12)` keeps juice's behaviour there too, for output
that has to match juice 12 byte for byte. So far:

- **`<style media="...">`** that does not apply to every screen is left in
  the document as written, as a browser applies it only where the query
  matches; juice ignores the attribute and inlines mobile-only rules for
  everyone. With `RemoveStyleTags` on and `PreserveMediaQueries` off it is
  removed, as `@media` blocks are.
- **Pseudo-classes score as classes**, as CSS scores them, so
  `p:first-child` beats `.x`. juice scores them as types, where `.x` wins.
  Pseudo-elements still score as types.
- **Pseudo-element `content` is text.** With `InlinePseudoElements`, a
  browser shows `content:"<b>x</b>"` as those characters; juice writes them
  into the document as markup. The default escapes `&`, `<` and `>` in
  content strings.

juice 9 is still widely deployed, so `CompatibilityMode(Juice9)` switches to
its behaviour where the two differ: stylesheets are read and preserved rules re-emitted through a port of
juice 9's CSS parser (mensch), selectors are scored as juice 9 scores them,
and there is no CSS variable resolution and none of juice 12's newer options
and control comments. Every `<style>` block is read as one stylesheet, nested
CSS is read as juice 9 reads it, and where juice 9 throws, `Inline` returns an
error wrapping `ErrJuiceFails`. The juice 9 suite runs with it set and passes in full, juice 9's own
quirks included.
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
every change and 50,000 with a fresh seed nightly. The generator exercises
every supported option, nesting and juice's ignore comments, and avoids the
deliberate divergences below, so any mismatch is a bug. `TARGET=juice-9` runs
the same documents through juice 9 and `CompatibilityMode(Juice9)`; CI runs both. The juice 12 run uses `CompatibilityMode(Juice12)`, so it stays byte-exact.

`TestMensch` checks the mensch port against every stylesheet in mensch's own
test suite (`testdata/mensch`), token for token.

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
are covered by tests in `divergence_test.go`, the rest by fixtures. The first
three hold in every mode; `CompatibilityMode(Juice12)` matches juice on the
last two.

1. **Liquid templates.** juice's default `codeBlocks` covers Handlebars and
   EJS but not Liquid's `{% %}`, so it corrupts
   `<td {% if x %}class="a"{% endif %}>` into `class="a" endif %}`. Liquid is
   in the default set here.
2. **Cyclic custom properties.** `--a:var(--b);--b:var(--a)` recurses until
   juice's stack overflows, which makes a cyclic stylesheet a denial of
   service, and a property that refers to itself twice never finishes.
   Resolution here is capped in depth and in substitutions per value.
3. **Malformed style attributes.** juice throws, failing the whole document,
   on a `style` attribute postcss cannot parse: `ttt { 123 }`, `----`, or
   `font-family:&quot;A B&quot;` (`decodeStyleAttributes` defaults off, so the
   raw entity reaches postcss). This package inlines the rest of the
   document. Telling exactly which attributes postcss rejects would take a
   port of its parser, so these stay `ALLOW` in `testdata/skip/juice-12.txt`.
4. **HTML comment markers around a stylesheet.** Old email templates wrap
   CSS as `<style><!-- p { color: red } --></style>`. CSS ignores `<!--` and
   `-->` there, so browsers and premailer apply the rule; juice reads
   `<!-- p` as a selector and silently drops it. The default applies it, as
   premailer's own `test_commented_out_styles_in_the_body` asserts too;
   `CompatibilityMode(Juice12)` drops it as juice does.
5. **Each `<style>` block is its own stylesheet.** juice concatenates every
   block before parsing, so an unclosed `@media` in one swallows the rules of
   the next and they are lost. Browsers parse each block separately, and so
   does the default here; `CompatibilityMode(Juice12)` joins them as juice
   does.

## Status

The cascade, at-rule preservation, attribute promotion, CSS variables,
Selectors L4 specificity, `:is()`/`:where()`, CSS nesting (flattened as
juice does, through postcss-nesting's rules), `::before`/`::after`
materialization (`InlinePseudoElements`, off by default as in juice) and
`/* juice ignore */` comments are implemented, as is every juice 12 option
except `url`, which only matters for fetching.

External resources are out of scope. This package does no network I/O; resolve
`<link rel=stylesheet>` yourself and pass the CSS via `ExtraCSS`.
