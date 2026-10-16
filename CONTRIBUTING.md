# Contributing

Thanks for considering a contribution. This document explains the
workflow, conventions, and checks expected of any change.

## Requirements

You will need:

- Go 1.26.2 or newer
- `make`
- `golangci-lint` v2.13.2, matching CI. Install it with the project
  toolchain:

  ```sh
  GOTOOLCHAIN=go1.26.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
  ```

`make lint` selects the Go version declared in `go.mod`. The Go toolchain
and linter must already be available, or toolchain downloads must be
enabled. Current HTTP modes do not require raw sockets.

## Workflow

1. Fork the repository and clone your fork.
2. Create a topic branch from `main`:

   git checkout -b feat/my-feature

3. Make your change. Keep commits focused — one logical change per
   commit.
4. Before opening a PR, run:

   make tidy fmt lint test

   All checks must pass.
5. Push to your fork and open a pull request against `main`.

## Commit messages

This project follows Conventional Commits:

  https://www.conventionalcommits.org/

Common prefixes:

- feat(scope): ...   — new feature
- fix(scope): ...    — bug fix
- docs: ...          — documentation only
- chore: ...         — maintenance, tooling, dependencies
- refactor: ...      — code change that neither fixes a bug nor adds a feature
- test: ...          — tests only
- perf: ...          — performance improvement

Examples:

  feat(generator): add HTTP/1.1 mode with RPS throttling
  fix(metrics): correct latency averaging under high load
  docs: add CICIDS2017 dataset instructions

## Code style

`make fmt` applies gofmt; `make lint` runs the linters configured in
`.golangci.yml`. The formatter settings
in `.golangci.yml` are used by `golangci-lint fmt`, which is not part of
`make fmt` or `make lint`.

A few rules worth calling out:

- Errors are returned, not logged and swallowed.
- context.Context is the first argument of any function that may block.
- HTTP requests always carry a context (noctx linter).
- Comments on exported identifiers start with the identifier name.
- Update affected documentation and comments when behavior, configuration,
  tooling, or test coverage changes.

## Pull requests

Fill out the PR template. If your change is not obvious, explain the
why — the what is visible in the diff.

Keep PRs small. A 50-line PR with a clear motivation will be reviewed
and merged far faster than a 500-line one without.

## Reporting bugs and requesting features

Use the issue templates under `.github/ISSUE_TEMPLATE/`. For security
issues, do not open a public issue — see SECURITY.md.
