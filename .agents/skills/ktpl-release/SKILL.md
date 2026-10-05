---
name: ktpl-release
description: >-
  Use this skill before declaring any ktpl task done (preflight: fmt, vet, lint, tests, build), when editing the
  GitHub Actions workflows or GoReleaser config, or when preparing and tagging a release.
---

# ktpl release & CI

## Preflight (run before every "done")

```bash
.agents/skills/ktpl-release/scripts/preflight.sh
```

It runs gofmt, `go vet`, `go mod tidy` diff check, golangci-lint (if installed), `go test -race` and a static build.
All steps must pass.

## CI (`.github/workflows/ci.yml`)

Triggers: push to `main`, pull requests. Jobs (Go version from `go.mod` via `actions/setup-go` `go-version-file`):

| Job | Does |
|---|---|
| `lint` | `golangci/golangci-lint-action` with golangci-lint v2 and `.golangci.yml` |
| `test` | `go test -race -coverprofile=coverage.out ./...`, coverage summary in job summary |
| `build` | matrix `linux,darwin,windows` x `amd64,arm64`, `CGO_ENABLED=0 go build ./cmd/ktpl` |

## Release (`.github/workflows/release.yml`)

Trigger: tag `v*`. GoReleaser v2 (`.goreleaser.yaml`) builds static binaries for the same matrix, injects
`main.version`, `main.commit`, `main.date` via ldflags, publishes archives + `checksums.txt` to the GitHub release.
No container image, no external registry.

## Tagging

1. Preflight green on `main`, CI green.
2. `goreleaser check` (if installed) and `goreleaser release --snapshot --clean` locally.
3. `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z`.

## Rules

- Pin actions to a major version; bump them deliberately in a `ci:` commit.
- Never add secrets beyond the default `GITHUB_TOKEN`.
