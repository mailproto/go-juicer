# Contributing

`go test ./...` runs the unit tests and the goldens. The oracles (`make goldens-verify`,
`make cascade`, `make differential`, `make premailer-verify`) need Node, Chrome or Ruby;
each Makefile target's comment says which.

## Pull requests

Titles start with `feat:`, `fix:` or `chore:` (or `docs:`, `refactor:`); release notes are
grouped by them. New fixtures and their goldens go in a PR of their own, ahead of the code
change that needs them.

## Releases

Maintainers tag `vX.Y.Z` on `main` with an annotated tag whose message body summarizes the
release for users: what changed and whether upgrading needs anything. It heads the release
notes, above the commits grouped by type. The release workflow refuses a tag that isn't on
`main` or doesn't pass the tests, then publishes the release and warms the Go module proxy.

```bash
git tag -a v0.4.0 -F notes.txt origin/main
git push origin v0.4.0
```
