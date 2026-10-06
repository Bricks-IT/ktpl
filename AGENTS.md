# AGENTS.md — ktpl

Guidelines for any AI agent (or human) working on this repository. The [README](README.md) is the **functional
specification**; this file is the **engineering contract**. When they disagree, stop and ask.

## What ktpl is

A CLI (`ktpl <folder>...`) that renders native Kubernetes YAML manifests where string values may contain Go templates
(Sprig + `ref`). `ref "<ns>/<kind>/<name>" "<path>"` reads a field of another object. Rendering is an iterative fixed
point (default max 5 iterations) with a lint pass after each iteration.

## Non-negotiable invariants

1. **Zero infrastructure.** Single static binary (`CGO_ENABLED=0`). Inputs/outputs are flat files. No database, no
   daemon, no cache, no state persisted between runs, **no network**, no Kubernetes API access.
2. **Determinism.** Same inputs + flags ⇒ byte-identical output. Never iterate over a Go map to produce output or to
   decide processing order; sort or keep document order. Field processing is **sequential**.
3. **Snapshot (Jacobi) semantics.** During iteration *k*, every `ref` reads the snapshot S(k‑1). Writes go to the next
   state only.
4. **Pending is tracked by a registry, never by scanning for `{{`.** A rendered field is never re-parsed.
5. **Deferral, not failure.** A `ref` that reaches a pending node (the target itself, an ancestor on the path, or any
   descendant) returns the `errDeferred` sentinel; the field is retried next iteration.
6. **Identity rules.** `apiVersion`/`kind` can't be templated. Objects with a templated `metadata.name` or
   `metadata.namespace` are *floating*: they render normally but can **never** be referenced.
7. **Namespaces are explicit or absent.** 3-segment identity = namespaced, 2-segment = no namespace. No relative form,
   no `--namespace` flag.
8. **Fail fast with location.** Every user-facing error includes object identity, field path and `file:line`, and
   iteration when relevant. Load/parse errors abort before iteration 1. Lint errors abort the iteration they appear in.
9. **Typing.** A scalar that is exactly one template action keeps the native type of its pipeline result; anything
   else is a string.
10. **Structured YAML.** Work on `yaml.Node` trees (`go.yaml.in/yaml/v3`) to preserve key order and comments. Never
    render whole files as text.

## Repository layout (target)

```
cmd/ktpl/            main package: only calls cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr) and os.Exit
internal/cli/        cobra command, flags, step mode, -o writer, exit codes, golden tests (golden_test.go)
internal/loader/     folder walk, file sort, multi-doc split, yaml.Node parsing, source positions
internal/object/     Object, Identity (parse/format/match), local/ignore/floating flags
internal/path/       path parse/format ("a.b[0]['k.x']"), Get/Set/Walk on yaml.Node
internal/overlay/    JSON Merge Patch (RFC 7386) on yaml.Node
internal/tmpl/       compile, funcmap (sprig + ref + helm extras), single-action typed capture, delims, hermetic
internal/convert/    Go value <-> yaml.Node
internal/engine/     pending registry, snapshot, iteration loop, deferral, cycle/max detection, trace
internal/lint/       per-iteration validators
internal/render/     ktpl.io/rendered annotation, YAML encoding (indent 2), stream output
examples/<case>/     templates/, rendered/{output.yaml|error.txt}, optional args — golden tests
```

## Commands

```bash
go build ./...
go test -race ./...
make golden                                       # regenerate goldens, then REVIEW the diff
golangci-lint run ./...
gofmt -l . && go vet ./...
```

Run `.agents/skills/ktpl-release/scripts/preflight.sh` before declaring any task done.

## Coding conventions

- Go version: the one in `go.mod` (1.27). Use modern stdlib (`slices`, `maps`, `iter`, `errors.Join`, `t.Chdir`).
- No global mutable state; no `init()` side effects; no panics on user input.
- Errors: wrap with `%w`; user-facing errors use the `engine.FieldError` / `lint.Error` types that carry location.
- Tests: table-driven, `t.Parallel()` except tests using `t.Chdir`; fuzz the path parser and identity parser.
- Every behaviour change ⇒ update README **and** add/adjust an `examples/` case (skill `ktpl-golden-tests`).
- Golden files are the contract: never hand-edit them once the engine exists; regenerate with `-update` and review.
- Comments and docs in **English**.

## Dependency policy

Allowed: `go.yaml.in/yaml/v3`, `github.com/spf13/cobra`, `github.com/Masterminds/sprig/v3`. Anything else needs
explicit approval from the maintainer. Never add `k8s.io/*` (too heavy; validation regexes are implemented locally).

## Skills

| Skill | Use when |
|---|---|
| [ktpl-engine](.agents/skills/ktpl-engine/SKILL.md) | Touching iteration, deferral, typing, identity or `ref` semantics |
| [ktpl-golden-tests](.agents/skills/ktpl-golden-tests/SKILL.md) | Adding/updating an example or golden test |
| [ktpl-template-functions](.agents/skills/ktpl-template-functions/SKILL.md) | Adding or changing a template function |
| [ktpl-lint-rules](.agents/skills/ktpl-lint-rules/SKILL.md) | Adding or changing a lint rule |
| [ktpl-release](.agents/skills/ktpl-release/SKILL.md) | Preflight checks, CI workflows, tagging a release |

## Commits

Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `ci:`, `refactor:`). One logical change per commit.
