---
name: ktpl-golden-tests
description: >-
  Use this skill when adding, updating or debugging an example under examples/, which doubles as a golden test
  (input templates, expected output or expected error, optional CLI args).
---

# ktpl golden tests

## Case layout

```
examples/<NN>-<kebab-name>/
├── templates/              input folder(s) (nested folders allowed, e.g. templates/base, templates/prod)
├── args                    optional, one line of CLI args; default: "templates"
└── rendered/
    ├── output.yaml         expected stdout, exit code 0
    └── error.txt           OR expected stderr, exit code 1 (exactly one of the two files)
```

The harness (`internal/cli/golden_test.go`) runs `cli.Run(args, ...)` **in-process** from the case folder (Ginkgo `Serial` specs, `os.Chdir` + `DeferCleanup`), so
paths in error messages look like `templates/file.yaml:12`.

## Add a case

1. Scaffold: `.agents/skills/ktpl-golden-tests/scripts/new-example.sh <kebab-name>` (picks the next number).
2. Write minimal manifests in `templates/` illustrating **one** behaviour. Templates are single-quoted YAML strings.
3. Generate the expected file: `go test ./internal/cli -update -ginkgo.label-filter=golden -ginkgo.focus='<NN>-<kebab-name>'` (or `make golden` for all).
4. **Review** the generated `rendered/*` line by line against the README semantics. A golden that merely matches
   current behaviour is worthless if the behaviour is wrong.
5. Add the case to the README "Examples" table.

## Rules

- Never hand-edit `rendered/*` once the engine exists; fix the code or the input and regenerate.
- Keep cases small (< 60 lines of input); one concept per case. Exception: `10-argocd`, the real-app showcase.
- Error cases must assert the full message, including `file:line`.

## Verify

```bash
go test ./internal/cli -ginkgo.label-filter=golden -ginkgo.v
git diff --stat examples/
```
