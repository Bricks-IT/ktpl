# ktpl

> **Status: design draft.** Syntax and behaviour described here are being validated; nothing is implemented yet. This application is actually vibe coded for proof of concept.

**ktpl** is a templating engine for Kubernetes that only knows **native Kubernetes objects**.
No `values.yaml`, no `Chart.yaml`, no `kustomization.yaml`: the objects in your folder *are* the values.
Any field can reference a field of another object using Go templates (with the full [Sprig](https://masterminds.github.io/sprig/)
library), and ktpl resolves everything through **successive iterations**.

```bash
ktpl ./manifests > rendered.yaml
```

## Why?

| | Helm | Kustomize | **ktpl** |
|---|---|---|---|
| Source format | Text templates + `values.yaml` | Native YAML + `kustomization.yaml` | **Native YAML only** |
| Cross-object references | No (values only) | Limited (`replacements`) | **Yes, any field** |
| Sources are valid YAML | No | Yes | **Yes** |
| Overlays | Values files | Yes | **Yes, multiple folders** |
| Infrastructure required | None | None | **None** |

## Principles

- **Zero infrastructure**: a single static Go binary. Inputs and outputs are flat files. No database, no server, no cache, no persisted state, no network, no cluster access.
- **Kubernetes native**: every document is a Kubernetes object (`apiVersion`, `kind`, `metadata.name`).
- **Sources stay valid YAML**: a template is always a quoted string.
- **Structured rendering**: YAML is parsed first; only scalar string values are templated. Key order and comments are preserved.
- **Iterative and deterministic**: same input, same output, whatever the file order.
- **Fail fast**: every iteration is linted; the first invalid state stops ktpl with an explicit error.
- **Traceable**: each rendered field is recorded, with the iteration that produced it, in a `ktpl.io/rendered` annotation.

## Installation

```bash
go install github.com/bricks-it/ktpl/cmd/ktpl@latest
```

Or download a static binary from the [GitHub releases](https://github.com/bricks-it/ktpl/releases).

## Quick start

`manifests/configmap.yaml`

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: db
  namespace: demo
data:
  host: postgres.demo.svc.cluster.local
  port: "5432"
```

`manifests/deployment.yaml` (excerpt)

```yaml
env:
  - name: DATABASE_URL
    value: 'postgres://{{ ref "demo/configmap/db" "data.host" }}:{{ ref "demo/configmap/db" "data.port" }}/app'
```

```bash
$ ktpl manifests/
```

```yaml
metadata:
  annotations:
    ktpl.io/rendered: '{"spec.template.spec.containers[0].env[0].value":1}'
# ...
env:
  - name: DATABASE_URL
    value: postgres://postgres.demo.svc.cluster.local:5432/app
```

## References: the `ref` function

```
{{ ref "<identity>" "<path>" }}
```

### Identity

| Form | Meaning |
|---|---|
| `shop/configmap/app-config` | Object `app-config` of kind ConfigMap in namespace `shop` |
| `clusterrole/admin` | Object **without** `metadata.namespace` (cluster-scoped, or namespace left to `kubectl -n`) |
| `shop/service.serving.knative.dev/api` | Kind qualified with its API group, to remove an ambiguity |

- The namespace is either **explicit or absent**: ktpl never guesses it and has no `--namespace` flag (that's `kubectl`'s job).
- The **kind** is case-insensitive: `configmap`, `ConfigMap`, `CONFIGMAP`. If two kinds with the same name exist in different groups, the unqualified form is an error.

### Path

- Dots and indexes: `spec.ports[0].port`.
- Keys containing `.` or `/` use brackets: `metadata.labels['app.kubernetes.io/name']` or `data["config.yaml"]`.
- An empty path (`""`) returns the whole object.
- Unknown object or path → **immediate error** (the set of objects is static).

### Templated identity

`metadata.name` and `metadata.namespace` **may** be templated (`apiVersion` and `kind` may not).
However, an object whose identity is templated **cannot be referenced** by any object (including itself),
because its identity is unknown until it is rendered. See [04-templated-identity](examples/04-templated-identity)
and [05-ref-templated-identity](examples/05-ref-templated-identity).

## The iteration engine

1. ktpl loads every object: this is state **S0**. Every string containing a template is a **pending** field.
2. At iteration *k*, each pending field is rendered **if all its references point to resolved values** in S(k‑1)
   (a map or list is resolved when none of its descendants is pending). Otherwise it is deferred.
3. All reads during iteration *k* use the snapshot S(k‑1): the result does not depend on file order.
4. After each iteration, every object is **linted** (see [Lint](#lint)); any error stops ktpl immediately.
5. ktpl stops:
   - ✅ as soon as no pending field remains;
   - ❌ if an iteration makes **no progress** (dependency cycle);
   - ❌ if the maximum number of iterations is reached with pending fields left (**5** by default, `--max-iterations`).

A field's iteration number is therefore its **dependency depth**:

```
platform.data.domain ──► app-config.data.BASE_URL ──► ingress.spec.rules[0].host ──► deployment...env[0].value
     (static)                 iteration 1                    iteration 2                    iteration 3
```

A rendered field is **never re-parsed**: if its result contains `{{`, that text is kept as is.

## Typing

A template produces text. To avoid `replicas: "3"`:

- If the value is **exactly one action** (`'{{ ... }}'` with no surrounding text), the **native type of the pipeline result** is kept:
  integer, float, boolean, map, list, or string.

  ```yaml
  containerPort: '{{ ref "shop/service/api" "spec.ports[0].port" }}'     # 8080 (int, as in the Service)
  replicas: '{{ ref "shop/configmap/platform" "data.replicas" | int }}'  # "3" -> 3 (Sprig cast)
  labels: '{{ ref "shop/deployment/api" "metadata.labels" }}'            # map
  ```

- Otherwise (text around the action, or several actions) the result is a string.
- Force a string with `| toString`.

## Annotations

### Written by ktpl

Each modified object gets a single annotation, a JSON object mapping each rendered path to its iteration, in document order:

```yaml
metadata:
  annotations:
    ktpl.io/rendered: '{"spec.replicas":1,"spec.template.spec.containers[0].env[0].value":3}'
```

An object merged from several input folders (see [overlays](#multiple-folders-overlays)) also gets the list of those
folders, in application order, as typed on the command line (cleaned):

```yaml
metadata:
  annotations:
    ktpl.io/sources: '["templates/base","templates/prod"]'
```

`--no-annotations` disables both.

### Read by ktpl

| Annotation | Effect |
|---|---|
| `ktpl.io/local: "true"` | The object can be referenced but is **not emitted** (a "values" object). `--keep-local` emits it anyway. |
| `config.kubernetes.io/local-config: "true"` | Same as above (kustomize convention). |
| `ktpl.io/ignore: "true"` | The object is never templated (it can still be referenced). |
| `ktpl.io/ignore-key: '["data[''alerts.yaml'']", "spec.groups"]'` | JSON list of paths whose subtrees are never templated. |

## Objects that already contain `{{ }}`

PrometheusRule, Argo Workflows, Grafana dashboards… use one of:

- `ktpl.io/ignore` or `ktpl.io/ignore-key` (see above, and [06-ignore](examples/06-ignore));
- standard Go escaping: `'{{ "{{" }} $labels.instance {{ "}}" }}'`;
- alternative delimiters: `--left-delim '[[' --right-delim ']]'`.

## Template functions

- `ref`: cross-object reference (see above).
- The full [Sprig v3](https://masterminds.github.io/sprig/) library: `default`, `upper`, `trimPrefix`, `b64enc`, `int`, `toJson`, `dict`, `list`…
- Helm-compatible extras: `toYaml`, `fromYaml`, `fromYamlArray`, `fromJson`, `required`.
- Go builtins: `printf`, `index`, `len`, `eq`, `and`, `or`, `not`, `if`, `with`, `range`…

> [!WARNING]
> Non-deterministic Sprig functions (`now`, `date`, `randAlphaNum`, `uuidv4`, `genPrivateKey`, `env`, …) break
> reproducibility. Use `--hermetic` to forbid them. `getHostByName` is always disabled (no network).

## Lint

After loading and after **every** iteration, each object is checked. The first iteration with errors stops ktpl and
**all** errors of that iteration are reported with file, line, object, path and iteration.

- the document round-trips through the YAML encoder;
- `apiVersion` and `kind` are non-empty strings, `metadata` is a map, `metadata.name` is a non-empty string;
- resolved `metadata.name` is a valid RFC 1123 subdomain, resolved `metadata.namespace` a valid RFC 1123 label;
- resolved label keys/values and annotation keys are valid; label and annotation values are strings;
- after the last iteration: no two objects share the same identity.

Pending (not yet rendered) fields are skipped until they are resolved.

## Multiple folders (overlays)

```bash
ktpl base/ prod/
```

- Folders are loaded in order. An object in a later folder with the **same identity** as an earlier one is merged onto it
  using [JSON Merge Patch](https://datatracker.ietf.org/doc/html/rfc7386) semantics: maps are merged recursively, lists and
  scalars are replaced, `null` deletes a key.
- Objects only present in a later folder are appended.
- Duplicate identities **inside the same folder** are an error.
- Merging happens **before** iteration 0; objects with a templated identity are never merged.
- A merged object records its folders in the `ktpl.io/sources` annotation.

**Parameters pattern.** Put the tunables in one local ConfigMap (e.g. `ktpl-parameter`) in `base/`, read it with `ref`
everywhere, and override only the keys that change in `prod/ktpl-parameter.yaml`. Since references are resolved after
the merge, every base manifest picks up the prod values:

```yaml
# base/ktpl-parameter.yaml                     # prod/ktpl-parameter.yaml
apiVersion: v1                                 apiVersion: v1
kind: ConfigMap                                kind: ConfigMap
metadata:                                      metadata:
  name: ktpl-parameter                           name: ktpl-parameter
  namespace: argocd                              namespace: argocd
  annotations:                                 data:
    ktpl.io/local: "true"                        domain: argocd.prod.example.com
data:
  domain: argocd.example.com
```

See [09-overlay](examples/09-overlay) and [10-argocd](examples/10-argocd).

## Step-by-step mode

```bash
# Interactive: pause after each iteration (requires a TTY; prompts go to stderr)
ktpl --step manifests/
```

```
── Iteration 1/5 ── 9 resolved · 3 pending
  shop/ConfigMap/app-config   data.BASE_URL         = https://shop.example.com
  shop/Deployment/api         spec.replicas         = 3
  ...
  pending: shop/Ingress/api spec.rules[0].host <- shop/ConfigMap/app-config data.BASE_URL
[Enter] next iteration · [d] diff · [q] quit
```

```bash
# Non-interactive: emit the partial state after N iterations (remaining templates left as is), exit code 0
ktpl --stop-after 2 manifests/ -o out/
```

## Errors

Errors go to stderr; exit code `1` for rendering/lint errors, `2` for usage errors.

```
$ ktpl examples/03-cycle/templates
Error: iteration 1: no progress, 2 pending field(s), dependency cycle:
  demo/ConfigMap/a data.x (templates/configmaps.yaml:8) <- demo/ConfigMap/b data.y
  demo/ConfigMap/b data.y (templates/configmaps.yaml:16) <- demo/ConfigMap/a data.x
```

```
$ ktpl --max-iterations 2 templates
Error: 1 pending field(s) after 2 iteration(s) (max-iterations=2):
  demo/ConfigMap/step3 data.value (templates/chain.yaml:31) <- demo/ConfigMap/step2 data.value
```

```
$ ktpl templates
Error: iteration 1: lint failed, 1 error(s):
  demo/ConfigMap/web metadata.labels.settings (templates/objects.yaml:16): label value must be a string, got map
```

## CLI reference

```
ktpl [flags] <folder>...

  -i, --max-iterations int   maximum number of iterations (default 5)
  -s, --step                 interactive mode, pause after each iteration
      --stop-after int       stop after N iterations and emit the partial state
      --render-dst string    output destination: 'stdout' or 'dir://<dir>' (default "stdout")
  -o, --output string        output folder (shorthand for --render-dst dir://<dir>)
      --no-annotations       do not write the ktpl.io/rendered and ktpl.io/sources annotations
      --keep-local           also emit local objects
      --hermetic             forbid non-deterministic template functions
      --left-delim string    left template delimiter (default "{{")
      --right-delim string   right template delimiter (default "}}")
      --version              print version and exit
```

Inputs: `.yaml`, `.yml`, `.json` files, walked recursively, multi-document, sorted by path.
A root can also be a single file, or `-` to read manifests from standard input (`stdin`).
With `--render-dst dir://<dir>` (or `-o <dir>`), each object is written to the path of the file it came from
(relative to its input folder), preserving the source directory structure.

## Examples

Each folder in [`examples/`](examples) is also a golden test: `templates/` is the input, `rendered/output.yaml` the expected
stdout (or `rendered/error.txt` the expected stderr), and an optional `args` file holds the CLI arguments.

| Example | Shows |
|---|---|
| [01-basic](examples/01-basic) | Simple reference, 1 iteration |
| [02-chained](examples/02-chained) | 3-iteration chain, local object, typing, map copy |
| [03-cycle](examples/03-cycle) | Dependency cycle → error |
| [04-templated-identity](examples/04-templated-identity) | Templated `metadata.name` / `metadata.namespace` |
| [05-ref-templated-identity](examples/05-ref-templated-identity) | Referencing a templated identity → error |
| [06-ignore](examples/06-ignore) | `ktpl.io/ignore` and `ktpl.io/ignore-key` |
| [07-max-iterations](examples/07-max-iterations) | Iteration limit reached → error |
| [08-lint-error](examples/08-lint-error) | Per-iteration lint → error |
| [09-overlay](examples/09-overlay) | Multiple folders, merge and append, `ktpl.io/sources` |
| [10-argocd](examples/10-argocd) | Real app: Argo CD chart converted to ktpl, `ktpl-parameter` ConfigMap, prod overlay |

## Development

```bash
go test ./...                       # unit tests + golden tests on examples/
make golden                                    # regenerate golden files (review the diff!)
golangci-lint run                   # lint
go build ./cmd/ktpl                 # build
```

GitHub Actions runs lint, test and build on every push and pull request; tagged releases (`v*`) publish static binaries.

## License

TBD.
