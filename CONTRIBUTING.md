# Contributing

1. Fork and branch from `main`.
2. Run `gofmt -l .`, `go vet ./...`, `staticcheck ./...` and `go test -race ./...`; all must be clean.
3. Unit tests must not hit the network. `TestLive` runs only when `TRACKIFIED_API_KEY` is set.
4. Open a pull request with a short description and a CHANGELOG entry.
