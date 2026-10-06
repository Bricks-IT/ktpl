# Day-2 Operations, Auditing & Troubleshooting

This guide covers operational practices, debugging workflows, and error diagnostics for running `ktpl` in production.

---

## 1. Production Telemetry & Metadata

`ktpl` automatically instruments every generated manifest with audit metadata annotations (unless disabled via `--no-annotations`).

### Tracking Rendered Fields: `ktpl.io/rendered`

The `ktpl.io/rendered` annotation records a JSON map of every rendered field path and the iteration $k$ in which it was resolved:

```yaml
metadata:
  annotations:
    ktpl.io/rendered: '{"spec.replicas":1,"spec.template.spec.containers[0].env[0].value":3}'
```

**SRE Incident Triage Benefit:**
When diagnosing an unexpected production configuration, you can inspect the live cluster state:
```bash
kubectl get deployment payment-service -o jsonpath='{.metadata.annotations.ktpl\.io/rendered}'
```
This tells you instantly whether a field was rendered dynamically or statically defined, and at what depth in the dependency chain.

### Tracking Layer Provenance: `ktpl.io/sources`

When manifests are merged across multiple directories or OCI layers (via JSON Merge Patch RFC 7386), `ktpl` records the list of sources in application order:

```yaml
metadata:
  annotations:
    ktpl.io/sources: '["oci://ghcr.io/org/web-service:v1.2.0", "overlays/prod/"]'
```

---

## 2. Interactive Debugging Workflows

### Interactive Step-by-Step Mode (`--step`)

When a complex dependency chain fails or produces unexpected values, run `ktpl --step`:

```bash
ktpl --step manifests/ overlays/prod
```

Output:
```
── Iteration 1/5 ── 9 resolved · 3 pending
  shop/ConfigMap/app-config   data.BASE_URL         = https://shop.example.com
  shop/Deployment/api         spec.replicas         = 3
  ...
  pending: shop/Ingress/api spec.rules[0].host <- shop/ConfigMap/app-config data.BASE_URL
[Enter] next iteration · [d] diff · [q] quit
```

- Press `[Enter]` to step into the next iteration.
- Press `[d]` to view a terminal diff of objects changed in this iteration.
- Press `[q]` to abort.

### Partial State Emission (`--stop-after N`)

To inspect the raw intermediate state without an interactive terminal (useful in CI or test scripts):

```bash
ktpl --stop-after 2 manifests/ overlays/prod -o /tmp/partial-state/
```

Pending templates that have not yet been reached in iteration 2 will remain in their unrendered format (e.g. `'{{ ref ... }}'`), allowing you to inspect the snapshot state.

---

## 3. Diagnosing Common Errors

### A. Dependency Cycles

A cycle occurs when two or more objects reference each other's pending fields:

```
$ ktpl templates/
Error: iteration 1: no progress, 2 pending field(s), dependency cycle:
  demo/ConfigMap/a data.x (templates/configmaps.yaml:8) <- demo/ConfigMap/b data.y
  demo/ConfigMap/b data.y (templates/configmaps.yaml:16) <- demo/ConfigMap/a data.x
```

**Resolution:**
Extract the shared values into a single local parameters ConfigMap (`ktpl.io/local: "true"`), and let both objects reference that static source.

### B. Max Iterations Exceeded

If a long dependency chain exceeds the default maximum (5 iterations):

```
$ ktpl templates/
Error: 1 pending field(s) after 5 iteration(s) (max-iterations=5):
  demo/ConfigMap/step6 data.value (templates/chain.yaml:65) <- demo/ConfigMap/step5 data.value
```

**Resolution:**
1. If the chain is legitimate, increase the threshold: `ktpl -i 10 manifests/`.
2. Flatten intermediate hops: reference the root source value directly instead of chaining through intermediate ConfigMaps.

### C. Referencing Templated Identities (Floating Objects)

An object whose `metadata.name` or `metadata.namespace` contains a template is classified as a **floating object**:

```
$ ktpl templates/
Error: templates/ingress.yaml:12: cannot reference floating object "demo/service/dynamic-svc":
  metadata.name is templated
```

**Why this rule exists:**
Referencing an object whose identity is not known until runtime would create race conditions and break determinism in iteration 0.

**Resolution:**
Keep `metadata.name` static, and template the interior configuration fields instead.

### D. Per-Iteration Lint Failures

`ktpl` validates every object after loading and after each iteration:

```
$ ktpl templates/
Error: iteration 1: lint failed, 1 error(s):
  demo/ConfigMap/web metadata.labels.settings (templates/objects.yaml:16): label value must be a string, got map
```

`ktpl` ensures that:
- `apiVersion` and `kind` are valid non-empty strings.
- Resolved `metadata.name` conforms to RFC 1123 subdomains (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`).
- Resolved `metadata.namespace` conforms to RFC 1123 labels.
- Label and annotation keys/values are strictly valid Kubernetes strings.

---

## 4. Handling Templates Inside CRDs (Prometheus, Grafana, Argo)

Certain Kubernetes manifests (e.g. `PrometheusRule`, Grafana dashboard ConfigMaps, Argo Workflow templates) naturally contain `{{ ... }}` syntax that should not be evaluated by `ktpl`.

### Option 1: Ignore the Entire Object (`ktpl.io/ignore`)

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: node-alerts
  annotations:
    ktpl.io/ignore: "true"
spec:
  # Any {{ $labels.instance }} inside here is untouched
```

### Option 2: Ignore Specific Subtrees (`ktpl.io/ignore-key`)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-dashboards
  annotations:
    ktpl.io/ignore-key: '["data[\"dashboard.json\"]"]'
data:
  dashboard.json: |
    { "title": "{{ job }} latency" }
```

### Option 3: Go Template String Escaping

```yaml
expr: 'rate(http_requests_total[5m]) > {{ "{{" }} threshold {{ "}}" }}'
```

### Option 4: Alternative Delimiters

If your templates conflict heavily with Go template syntax, change delimiters:

```bash
ktpl --left-delim '[[' --right-delim ']]' manifests/
```

```yaml
# Now ktpl only interprets [[ ref ... ]]
env:
  - name: APP_PORT
    value: '[[ ref "demo/service/api" "spec.ports[0].port" ]]'
```
