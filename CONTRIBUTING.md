# Contributing

Thanks for helping! The short version:

1. Fork, create a branch, make your change.
2. Run `make check` and `go test -race ./internal/...`.
3. Open a pull request. Keep it focused; add a test when you change logic.

Build and layout notes are in the wiki: [Development](https://github.com/anand34577/geo-tracker/wiki/Development). Documentation lives in `docs/` and is published to the wiki automatically.

Two compatibility rules: `/ingest/*` endpoints never change (phones are configured once), and `/api/v1/*` changes only additively.
