# 10-argocd — a real application

The [Argo CD Helm chart](https://github.com/argoproj/argo-helm/tree/main/charts/argo-cd) (chart `10.9.6`, Argo CD
`v3.5.3`) converted to plain Kubernetes manifests driven by ktpl.

```bash
ktpl templates/base templates/prod   # production (this golden test, see args)
ktpl templates/base                  # defaults
```

## Layout

```
templates/
├── base/
│   ├── ktpl-parameter.yaml          ← every tunable, in one local ConfigMap (never emitted)
│   ├── argocd-configs/              argocd-cm, argocd-cmd-params-cm, argocd-rbac-cm, secrets…
│   ├── argocd-server/               Deployment, Service, Ingress, RBAC…
│   ├── argocd-repo-server/  argocd-application-controller/  argocd-applicationset/
│   ├── argocd-notifications/  dex/  redis/  redis-secret-init/
└── prod/
    ├── ktpl-parameter.yaml          ← only the keys that differ in prod (merged onto base)
    └── argocd-rbac-cm.yaml          ← partial argocd-rbac-cm (merged onto base)
```

## The `ktpl-parameter` ConfigMap

| Key | Used by |
|---|---|
| `version` | `app.kubernetes.io/version` label of every object, `argocdImage` |
| `argocdRepository`, `argocdImage` | Argo CD containers (`argocdImage` is itself a template: `<repository>:<version>`) |
| `dexImage`, `redisImage`, `imagePullPolicy` | Dex and Redis containers, every container |
| `domain` | Ingress host, `argocd-cm` `url` (and through it the notifications `argocdUrl`) |
| `serverReplicas`, `repoServerReplicas`, `applicationSetReplicas`, `controllerReplicas` | `spec.replicas` (`\| int`) |
| `logLevel`, `logFormat` | every `*.log.level` / `*.log.format` of `argocd-cmd-params-cm` |
| `adminEnabled`, `execEnabled`, `statusBadgeEnabled`, `reconciliationTimeout` | `argocd-cm` |

`metadata.namespace` is **not** a parameter on purpose: an object with a templated namespace is floating and can't be
referenced. Keep `argocd`, or change it in the files.

## What it demonstrates

| Feature | Where | Iteration |
|---|---|---|
| Direct parameter read | labels, `imagePullPolicy`, replicas, Ingress host, `argocd-cmd-params-cm` | 1 |
| Typing | `replicas: '{{ … \| int }}'` → `2`; Ingress port read from the Service → `443` | 1 |
| Derived parameter | `argocdImage` → every Argo CD container image | 2 |
| Chained objects | `argocd-cm.data.url` → `argocd-notifications-cm.data.context` (literal block kept) | 2 |
| Checksums like Helm | `checksum/cm: '{{ ref "argocd/configmap/argocd-cm" "data" \| toJson \| sha256sum }}'` waits until `argocd-cm` is fully rendered | 2 |
| Self reference | `ARGOCD_CONTROLLER_REPLICAS` reads the StatefulSet's own `spec.replicas` | 2 |
| Overlay | `prod/` overrides parameters and patches `argocd-rbac-cm`, which gets `ktpl.io/sources: '["templates/base","templates/prod"]'` | – |

## How it was produced

```bash
helm template argocd argo-cd --repo https://argoproj.github.io/argo-helm --version 10.9.6 -n argocd \
  --set crds.install=false --set global.domain=argocd.example.com --set server.ingress.enabled=true
```

then split per `# Source:` file and edited:

- hard-coded values replaced by `ref "argocd/configmap/ktpl-parameter" "data.<key>"`;
- `helm.sh/chart` labels removed, `app.kubernetes.io/managed-by: Helm` → `ktpl`;
- `checksum/*` annotations computed by ktpl instead of Helm.

CRDs are **not** included (≈ 31 000 lines): install them separately, e.g.
`kubectl apply -k "https://github.com/argoproj/argo-cd/manifests/crds?ref=v3.5.3"`.
The `redis-secret-init` objects keep their `helm.sh/hook` annotations, which `kubectl` ignores.
