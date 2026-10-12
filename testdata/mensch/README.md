Test inputs for the mensch port (`mensch.go`), the CSS parser juice 9 uses.

`in/` holds every stylesheet mensch's own test suite (v0.3.4,
https://github.com/brettstimmerman/mensch) feeds it, named after the test, under
mensch's MIT license (`LICENSE`). `out/` holds mensch's tokens for each, and each
top-level node stringified as juice 9 preserves it, written by
`testdata/oracle/mensch.mjs` (`make goldens`).
