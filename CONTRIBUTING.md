# Contributing

Thanks for helping improve this template. The goal is a small set of
well-integrated gRPC-native foundations, so prefer unifying and deleting over
adding speculative features.

## Getting started

```sh
cp .env.example .env
just up        # start postgres + redis
just migrate   # apply migrations
just run       # run the gRPC server
```

See [AGENTS.md](AGENTS.md) for the layout and conventions, and [docs/](docs/)
for deeper notes.

## Before you open a pull request

```sh
just fmt       # gofmt + go mod tidy
just lint      # golangci-lint
just test      # tests with the race detector
just proto     # regenerate protobuf code if any .proto changed
```

CI runs build, vet, tests, `buf lint`, golangci-lint, and markdownlint on every
pull request. Keep all of them green.

## Conventions

- The proto contract is the source of truth for the API. Never edit generated
  code under `internal/gen`; change the `.proto` and run `just proto`.
- Add a migration whenever the schema changes.
- Keep comments short: explain why, not what.
- Use a clear, imperative commit subject.

## Reporting bugs and requesting features

Open an issue using one of the templates. For security issues, follow the
[security policy](SECURITY.md) instead of filing a public issue.
