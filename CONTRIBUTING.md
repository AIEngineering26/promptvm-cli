# Contributing to PromptVM CLI

Thanks for your interest in contributing!

## Getting Started

1. Fork the repository
2. Clone your fork and create a branch:
   ```bash
   git clone https://github.com/<your-username>/promptvm-cli
   cd promptvm-cli
   git checkout -b my-feature
   ```
3. Install dependencies and build:
   ```bash
   make deps
   make build
   ```
4. Run tests and linting:
   ```bash
   make test
   make lint
   ```

## Development

The CLI imports the [Go SDK](https://github.com/AIEngineering26/promptvm-go-sdk). For local SDK development, create a `go.work` file (git-ignored):

```bash
go work init .
go work use ../go-sdk
```

## Submitting Changes

1. Ensure `make test` and `make lint` pass
2. Write clear commit messages
3. Open a pull request against `main`
4. Describe what changed and why

## Reporting Issues

Use [GitHub Issues](https://github.com/AIEngineering26/promptvm-cli/issues) for bugs and feature requests. For security vulnerabilities, see [SECURITY.md](./SECURITY.md).

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](./LICENSE).
