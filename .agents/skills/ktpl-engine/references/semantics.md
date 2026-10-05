# ktpl engine semantics (reference)

## Objects and identity

- Identity = `(namespace, group, kind, name)`. `group` comes from `apiVersion` (`apps/v1` -> `apps`, `v1` -> `""`).
- `ref` identity string: `ns/kind/name` (namespaced) or `kind/name` (no namespace). `kind` may be `kind.group`
  (`service.serving.knative.dev`). Kind match is case-insensitive. Unqualified kind matching several groups -> error.
- Floating object: `metadata.name` or `metadata.namespace` is a pending template at S0. Never indexed for `ref`.
  `ref` not-found errors add the hint `(objects with a templated metadata.name or metadata.namespace cannot be referenced)`
  when at least one floating object exists.
- `apiVersion` or `kind` containing a template -> load error.
- Duplicate identity within one input folder -> load error. Across folders -> merge (overlay). Duplicate after final
  render (floating objects included) -> lint error.

## Pending registration

- Walk each object's tree in document order. A scalar node with tag `!!str` containing the left delimiter is a
  pending field, unless the object has `ktpl.io/ignore: "true"` or the path is under a `ktpl.io/ignore-key` entry.
- Templates are parsed at registration: syntax errors are load errors with `file:line`.
- `single` = the parse tree is exactly one `ActionNode` (no text nodes, no other actions).

## Execution of a field at iteration k

- Funcs are re-bound per execution so `ref` closes over the snapshot and the current object (for errors).
- `ref(id, path)`:
  1. resolve id in the snapshot index (non-floating objects only) -> not found: error;
  2. walk `path`; if any node on the way (or the final node) is pending -> `errDeferred`;
  3. if any pending field is a descendant of the final node -> `errDeferred`;
  4. missing key/index -> error `path not found`;
  5. return the Go value (`convert.FromNode`), and remember `value -> source node` for order-preserving re-insertion.
- `errDeferred` is detected with `errors.Is` on the error returned by `Execute` (text/template wraps func errors).
- Single action: append a `__ktpl_capture` command to the action's pipe (parse-tree manipulation, not text rewrite);
  the capture func stores the value and returns `""`. The node is built from the captured value:
  - value is a map/slice previously returned by `ref` -> deep copy of the source node (key order kept);
  - other map -> keys sorted; string -> `!!str` (encoder decides quoting); int/float/bool/nil -> matching tags.
- Multi action / text: the output string replaces the scalar value (`!!str`), comments kept.

## End of iteration

- Apply all results to the next state, record `trace[path] = k` per object (document order).
- Lint the full state (pending fields skipped). Errors -> `iteration k: lint failed, N error(s):` + one line each.
- No field rendered and pending left -> `iteration k: no progress, N pending field(s), dependency cycle:` + per pending
  field `id path (file:line) <- blockerId blockerPath`.
- `k == max` and pending left -> `N pending field(s) after k iteration(s) (max-iterations=max):` + same lines.
- `--stop-after k` -> emit current state (pending scalars untouched) and exit 0.

## Output

- Order: input folder order, then file path order, then document order; overlay-only objects appended in their order.
- Local objects (`ktpl.io/local` / `config.kubernetes.io/local-config` = `"true"`) skipped unless `--keep-local`.
- `ktpl.io/rendered`: compact JSON object, keys = canonical paths in registration order, values = iteration; single
  quoted YAML style; appended at the end of `metadata.annotations` (created at the end of `metadata` if absent).
- Encoder indent 2; `---` between documents; head comments of documents preserved.
