---
name: ktpl-lint-rules
description: >-
  Use this skill when adding or modifying a lint rule that ktpl runs after loading and after every iteration
  (object structure, metadata name/namespace validity, labels, annotations, duplicate identities).
---

# ktpl lint rules

Rules live in `internal/lint/`, one file per rule, all registered in `rules.go` in a fixed order.

```go
type Rule interface {
    Name() string
    // Check must skip pending paths (ctx.IsPending(obj, path)) and must not mutate the object.
    Check(ctx *Context, obj *object.Object) []Error
}

// Global rules (e.g. duplicate identities) run once per iteration over all objects.
type GlobalRule interface {
    Name() string
    CheckAll(ctx *Context, objs []*object.Object) []Error
}
```

`Error` carries identity, canonical path, `file:line` and message. The engine prints:

```
Error: iteration <k>: lint failed, <n> error(s):
  <id> <path> (<file>:<line>): <message>
```

## Adding a rule

1. Create `internal/lint/<rule>.go` + `<rule>_test.go` (table-driven, include a pending-path case that must be skipped).
2. Register it in `rules.go`. Order matters for output stability: append, don't insert.
3. Messages: lowercase, no trailing period, state what is expected and what was found
   (`label value must be a string, got map`).
4. Validation regexes (RFC 1123, qualified names) are implemented locally in `internal/lint/k8sname.go`; do **not**
   import `k8s.io/*`.
5. Add an error golden case (skill `ktpl-golden-tests`) and update the README "Lint" section.

## Verify

```bash
go test -race ./internal/lint/...
go test ./internal/cli -run TestGolden
```
