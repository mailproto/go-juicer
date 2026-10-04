Inputs from the test suite of the Ruby
[premailer](https://github.com/premailer/premailer) gem, v1.31.0: every
document its `test_premailer`, `test_misc`, `test_links`, `test_warnings` and
`test_adapter` tests pass to `Premailer.new`, recorded by running that suite.
Directories follow the test file and names the test method; a `-2` suffix is a
second document from the same test. A `css_string` option becomes `extraCss`.

Expected outputs come from juice, like every other fixture. premailer's own
output is not byte-comparable: it re-serializes through Nokogiri/libxml2.

Copyright (c) 2007-2017, Alex Dunae. BSD 3-Clause License:
https://github.com/premailer/premailer/blob/v1.31.0/LICENSE.md
