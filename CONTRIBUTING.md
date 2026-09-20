# Contributing

Thanks for considering a contribution. This document explains the
workflow, conventions, and checks expected of any change.

## Requirements

You will need:

- Go 1.26 or newer
- `make`
- `golangci-lint` — install with
  `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
- Linux is recommended for running the generator itself (some capture
  modes need raw socket access)

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

Formatting and linting are enforced by golangci-lint — see
`.golangci.yml` for the full ruleset. `make fmt` handles the basics
(gofmt, goimports); `make lint` runs the rest.

A few rules worth calling out:

- Errors are returned, not logged and swallowed.
- context.Context is the first argument of any function that may block.
- HTTP requests always carry a context (noctx linter).
- Comments on exported identifiers start with the identifier name.

## Pull requests

Fill out the PR template. If your change is not obvious, explain the
why — the what is visible in the diff.

Keep PRs small. A 50-line PR with a clear motivation will be reviewed
and merged far faster than a 500-line one without.

## Reporting bugs and requesting features

Use the issue templates under `.github/ISSUE_TEMPLATE/`. For security
issues, do not open a public issue — see SECURITY.md.
