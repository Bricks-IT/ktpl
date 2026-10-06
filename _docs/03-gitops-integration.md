# GitOps Integration: Argo CD, Flux & CI/CD Pipelines

This guide provides production configurations and architectural patterns for integrating `ktpl` into GitOps engines (**Argo CD**, **Flux v2**) and enterprise CI/CD pipelines.

---

## 1. Argo CD Integration (CMP v2)

The recommended approach to run `ktpl` inside Argo CD is via **Config Management Plugin v2 (CMP v2)**, configured as a sidecar container in `argocd-repo-server`.

### A. The CMP Configuration (`plugin.yaml`)

Create a ConfigMap or bundle this file into the CMP container image:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ConfigManagementPlugin
metadata:
  name: ktpl-plugin
spec:
  version: v1.0
  generate:
    command: ["sh", "-c"]
    # Reads KTPL_ARGS from the Application manifest if provided, or defaults to current directory
    args:
      - |
        if [ -n "$ARGOCD_ENV_KTPL_LAYERS" ]; then
          ktpl --hermetic $ARGOCD_ENV_KTPL_LAYERS .
        else
          ktpl --hermetic .
        fi
  discover:
    fileName: "ktpl-parameter.yaml"
```

### B. Patching `argocd-repo-server` with the Sidecar

Add the `ktpl` sidecar container to the `argocd-repo-server` Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-repo-server
  namespace: argocd
spec:
  template:
    spec:
      containers:
        - name: ktpl-plugin
          image: ghcr.io/bricks-it/ktpl:latest
          command: ["/var/run/argocd/argocd-cmp-server"]
          securityContext:
            runAsNonRoot: true
            runAsUser: 999
            readOnlyRootFilesystem: true
          volumeMounts:
            - mountPath: /var/run/argocd
              name: var-run-argocd
            - mountPath: /tmp
              name: cmp-tmp
            - mountPath: /home/ktpl/.docker/config.json
              name: docker-config
              subPath: .dockerconfigjson
              readOnly: true
      volumes:
        - name: cmp-tmp
          emptyDir: {}
        - name: docker-config
          secret:
            secretName: registry-credentials
            optional: true
```

### C. Argo CD Application Definition

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: shop-api-prod
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/my-org/gitops-deployments.git
    targetRevision: main
    path: apps/shop-api/prod
    plugin:
      name: ktpl-plugin
      env:
        # Layer an upstream OCI Golden Path with the local repository folder
        - name: KTPL_LAYERS
          value: "oci://ghcr.io/my-org/golden-paths/web-service:v1.2.0"
  destination:
    server: https://kubernetes.default.svc
    namespace: shop
```

---

## 2. Flux v2 Integration

In Flux, `ktpl` can be invoked using custom post-renderers or via the `KustomizeController` by pre-rendering manifests in CI, or utilizing a custom container generator.

When using Flux with a wrapper script:
```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: shop-api
  namespace: flux-system
spec:
  interval: 10m
  path: ./apps/shop-api/prod
  prune: true
  sourceRef:
    kind: GitRepository
    name: fleet-infra
  postBuild:
    substitute: {}
```

Alternatively, pre-render manifests into an environment branch or release tag via CI before Flux applies them.

---

## 3. Production CI/CD Pipeline (GitHub Actions)

A robust CI/CD workflow validates manifests on pull requests and publishes versioned OCI packages on tags.

### Pull Request Validation Workflow (`.github/workflows/pr-check.yml`)

```yaml
name: Validate Manifests

on:
  pull_request:
    paths:
      - 'manifests/**'
      - 'overlays/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Install ktpl
        uses: actions/setup-go@v5
        with:
          go-version: '1.27'
      - run: go install github.com/bricks-it/ktpl/cmd/ktpl@latest

      - name: Install kubeconform
        run: |
          curl -sL https://github.com/yannh/kubeconform/releases/latest/download/kubeconform-linux-amd64.tar.gz | tar xz
          sudo mv kubeconform /usr/local/bin/

      - name: Render and Lint Environments
        run: |
          mkdir -p /tmp/rendered
          # Render prod environment hermetically
          ktpl --hermetic manifests/ overlays/prod > /tmp/rendered/prod.yaml

          # Strictly validate against Kubernetes schemas
          kubeconform -strict -summary /tmp/rendered/prod.yaml

      - name: Security Scan with Trivy
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: 'config'
          scan-ref: '/tmp/rendered/prod.yaml'
          exit-code: '1'
          severity: 'CRITICAL,HIGH'
```

### OCI Publishing Workflow (`.github/workflows/publish-oci.yml`)

```yaml
name: Publish OCI Blueprint

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
      id-token: write

    steps:
      - name: Checkout Code
        uses: actions/checkout@v4

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Install ktpl
        run: go install github.com/bricks-it/ktpl/cmd/ktpl@latest

      - name: Package and Push OCI Artifact
        run: |
          VERSION=${GITHUB_REF_NAME}
          ktpl push templates/base ghcr.io/${{ github.repository }}/blueprint:${VERSION}

      - name: Sign OCI Artifact with Cosign
        uses: sigstore/cosign-installer@v3
      - run: |
          cosign sign --yes ghcr.io/${{ github.repository }}/blueprint:${GITHUB_REF_NAME}
```

---

## 4. Operational Best Practices in GitOps

1. **Always Use `--hermetic` in Automated Pipelines**:
   Non-deterministic template functions like `now`, `uuidv4`, or `randAlphaNum` cause constant GitOps drift and unending reconciliation loops in Argo CD. `--hermetic` immediately flags and blocks these functions.
2. **Layer Caching**:
   When referencing `oci://...` artifacts in Argo CD, maintain a warm container filesystem or volume cache to prevent redundant pulls during 3-minute reconciliation intervals.
3. **Audit Annotations**:
   Leave `ktpl.io/rendered` enabled in production. When troubleshooting incidents, `kubectl get deployment api -o yaml` provides instantaneous proof of the exact iteration and source file responsible for any computed field.
