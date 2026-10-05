Inputs from the Rust [css-inline](https://github.com/Stranger6667/css-inline)
crate at commit 67bdcd6 (2026-10-02).

- `tests/`: every document its `test_inlining` and `test_selectors` suites
  pass to the inliner, recorded by running those suites. Names follow the test
  function; a `-2` suffix is a second document from the same test. CSS passed
  separately (`inline_fragment`, `extra_css`) becomes the `extraCss` option.
  Tests that fetch stylesheets over the network are left out.
- `benchmarks/`: the documents from its `benchmarks/benchmarks.json`, except
  the 1.9 MB `big_page`.

Expected outputs come from juice, like every other fixture; css-inline parses
HTML to the HTML5 spec, so its own output is not byte-comparable.

Copyright (c) 2020-2023 Dmitry Dygalo, MIT License:
https://github.com/Stranger6667/css-inline/blob/67bdcd6/LICENSE
