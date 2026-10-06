---
name: ktpl-template-functions
description: >-
  Use this skill when adding, removing or changing a template function available in ktpl templates (ref, Sprig,
  Helm-compatible extras such as toYaml/fromYaml/required), or when touching the hermetic function filter.
---

# ktpl template functions

All functions are assembled in `internal/tmpl/funcs.go`:

```
base   := sprig.TxtFuncMap()            // full Sprig v3
extras := toYaml, fromYaml, fromYamlArray, fromJson, required
ktpl   := ref, __ktpl_capture           // bound per execution, never user-overridable
remove := getHostByName                 // always (no network)
if --hermetic: remove sprig non-hermetic list + genPrivateKey/genCA/genSelfSignedCert/... + env/expandenv
```

## Adding a function

1. Decide its category: **pure** (deterministic, no I/O) or **non-hermetic** (time, randomness, environment).
   Network or filesystem access is **forbidden** (zero-infrastructure invariant).
2. Implement it in `internal/tmpl/funcs.go` (or `funcs_<topic>.go`), returning `(T, error)` for fallible functions.
3. If non-hermetic, add its name to `nonHermetic` so `--hermetic` removes it.
4. Think about typing: when used as the last stage of a single-action template, its Go return type becomes the YAML
   type (map -> mapping, `[]any` -> sequence, int -> `!!int`). Document it.
5. Unit test in `internal/tmpl/funcs_test.go` (table-driven: input, expected value, expected error).
6. Add a golden case if user-visible behaviour is new (skill `ktpl-golden-tests`).
7. Update the README "Template functions" section.

## Rules

- Never shadow a Sprig function with a different behaviour; Helm users expect Helm semantics for shared names.
- `ref` must not be callable with dynamic side effects: it only reads the snapshot.

## Verify

```bash
go test -race ./internal/tmpl/...
go test ./internal/cli -ginkgo.label-filter=golden
```
