# Contributing

## Branching and pull requests

All changes must go through a pull request targeting `main`. Direct pushes to `main` are
not permitted. Each PR must have at least one approving review before merging.

Use **squash merge** so that every commit on `main` corresponds to exactly one PR and
carries a well-formed conventional commit message.

## Commit message format

This project follows the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)
specification. The **PR title** is used as the squash-merge commit message, so it must
conform to the format. A GitHub Actions check enforces this on every PR.

### Structure

```
<type>(<optional scope>): <short description>

[optional body]

[optional footer(s)]
```

### Allowed types

| Type | When to use | Version bump |
|---|---|---|
| `feat` | A new feature | minor |
| `fix` | A bug fix | patch |
| `perf` | A performance improvement | patch |
| `refactor` | Code change with no behaviour change | none |
| `docs` | Documentation only | none |
| `test` | Adding or fixing tests | none |
| `build` | Changes to build system or dependencies | none |
| `ci` | Changes to CI/CD workflows | none |
| `chore` | Maintenance tasks (e.g. dependency bumps) | none |
| `style` | Formatting, whitespace (no logic change) | none |
| `revert` | Reverting a previous commit | none |

A **breaking change** — regardless of type — must include `BREAKING CHANGE:` in the commit
footer, or append `!` after the type (e.g. `feat!:`). This triggers a **major** version bump.

### Examples

```
feat: add Prometheus metrics endpoint
fix: remove driver from store when pod enters Failed phase
docs: document HTTPRoute manager in DESIGN.md
ci: merge vet into test step to halve cold-cache build time
feat!: replace flag-based config with environment variables

BREAKING CHANGE: all -flag arguments are now environment variables
```

## Versioning

Releases and version tags are created automatically by
[release-please](https://github.com/googleapis/release-please) when commits are merged to
`main`. You do not need to create tags or update version files manually.

The version is determined by the conventional commit types since the last release:

- `fix` / `perf` → patch bump (e.g. `1.2.3` → `1.2.4`)
- `feat` → minor bump (e.g. `1.2.3` → `1.3.0`)
- `BREAKING CHANGE` / `!` → major bump (e.g. `1.2.3` → `2.0.0`)

When the release PR created by release-please is merged, the repository is tagged and the
Docker image is published to `ghcr.io/unaiur/k8s-spark-ui-assist` with the corresponding
semver tags (e.g. `1.2.3` and `1.2`).

## Development workflow

```sh
# Run format check, vet and tests
make test

# Build the binary locally
make build
```

See [DESIGN.md](DESIGN.md) for an overview of the internal architecture.
