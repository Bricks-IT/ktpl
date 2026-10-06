# Enterprise Golden Paths & OCI Distribution

This guide details how Platform Engineering teams can build, package, and distribute reusable infrastructure standards ("Golden Paths") using `ktpl` and standard OCI registries.

---

## 1. What is an Enterprise Golden Path in ktpl?

In modern platform engineering, a **Golden Path** is an opinionated, supported, and turnkey deployment blueprint provided by the platform team to product engineering teams.

With `ktpl`, a Golden Path consists of:
1. **A Base Blueprint**: Standard Kubernetes manifests (Deployment, Service, HPA, PodDisruptionBudget, ServiceAccount) defining organizational best practices.
2. **A Parameter ConfigMap**: A local, un-emitted ConfigMap (`ktpl.io/local: "true"`) exposing tuneable variables (e.g. `image`, `port`, `minReplicas`, `domain`).
3. **Cross-Cutting Mixins**: Optional OCI layers for security, ingress routing, or observability (ServiceMonitor, PrometheusRule).

```
+-------------------------------------------------------------------------+
|                  Platform Team (Producer)                               |
|                                                                         |
|  templates/base/                                                        |
|  ├── ktpl-parameter.yaml   (ktpl.io/local: "true", un-emitted)          |
|  ├── deployment.yaml       (reads data from ktpl-parameter)             |
|  ├── service.yaml          (reads ports from deployment)                |
|  └── pdb.yaml                                                           |
|                                                                         |
|  $ ktpl package templates/base -o web-service.tar --tag org/web:1.0.0   |
|  $ ktpl push web-service.tar ghcr.io/my-org/golden-paths/web:1.0.0     |
+------------------------------------+------------------------------------+
                                     |
                                     v OCI Registry
+------------------------------------+------------------------------------+
|                  Application Team / GitOps (Consumer)                   |
|                                                                         |
|  overlays/prod/                                                         |
|  └── ktpl-parameter.yaml   (overrides: domain, replicas, image tag)     |
|                                                                         |
|  $ ktpl oci://ghcr.io/my-org/golden-paths/web:1.0.0 overlays/prod/     |
+-------------------------------------------------------------------------+
```

---

## 2. Multi-Layer OCI Composition

`ktpl` supports placing remote OCI artifacts and local folders on **any layer**:

```bash
ktpl oci://ghcr.io/my-org/base-app:v1.0.0 \
     oci://ghcr.io/my-org/observability-mixin:v1.2.0 \
     oci://ghcr.io/my-org/security-hardened-networkpolicy:v1.0.0 \
     overlays/prod/
```

### Layer Evaluation Order

1. **Layer 0 (Base)**: Extracted from `oci://ghcr.io/my-org/base-app:v1.0.0`.
2. **Layer 1 (Observability Mixin)**: Added or merged onto matching identities using JSON Merge Patch (RFC 7386).
3. **Layer 2 (Security Mixin)**: Injects default NetworkPolicy and SecurityContext defaults.
4. **Layer 3 (Environment Overlay)**: Local `overlays/prod/` overrides parameters or patches resources for the target environment.
5. **Iteration Pass**: The fixed-point engine renders all references across the merged result.

Objects merged from multiple layers receive a `ktpl.io/sources` annotation recording their provenance:
```yaml
metadata:
  annotations:
    ktpl.io/sources: '["oci://ghcr.io/my-org/base-app:v1.0.0", "overlays/prod/"]'
```

---

## 3. OCI Packaging & Registry Commands

### Packaging a Local Directory into an OCI Tarball

You can archive and package any template directory into an OCI-compliant image tarball:

```bash
# Package a folder into a tarball
ktpl package manifests/ -o web-service-v1.0.0.tar --tag my-org/web-service:v1.0.0
```

### Publishing to a Registry

`ktpl push` accepts either a pre-built `.tar` archive or a directory directly:

```bash
# Push an existing package archive
ktpl push web-service-v1.0.0.tar ghcr.io/my-org/golden-paths/web-service:v1.0.0

# Push directly from a source directory
ktpl push manifests/ ghcr.io/my-org/golden-paths/web-service:v1.0.0

# For local or insecure registries (skips TLS verification and allows plain HTTP)
ktpl push manifests/ registry.internal:5000/app:v1.0.0 --insecure
```

### Pulling and Extracting Templates

```bash
ktpl pull ghcr.io/my-org/golden-paths/web-service:v1.0.0 -o ./local-manifests
```

### OCI Media Types

`ktpl` packages content according to the Open Container Initiative Image Specification:
- **Manifest Schema**: OCI Image Manifest (`application/vnd.oci.image.manifest.v1+json`)
- **Layer Media Type**: `application/vnd.ktpl.content.v1.tar+gzip`

---

## 4. Authentication & Registry Configuration

`ktpl` integrates natively with `github.com/google/go-containerregistry/pkg/authn`.

### Standard Docker Credentials

By default, `ktpl` reads credentials from `~/.docker/config.json`:
```json
{
  "auths": {
    "ghcr.io": {
      "auth": "<base64-encoded-token>"
    }
  }
}
```

### Cloud Provider Credential Helpers (IAM)

Standard credential helpers configured in your Docker config are automatically invoked:
- **AWS ECR**: `docker-credential-ecr-login`
- **GCP Artifact Registry**: `docker-credential-gcr`
- **Azure Container Registry (ACR)**: `docker-credential-acr`

In Kubernetes or CI environments, mount your Docker config into `/home/nonroot/.docker/config.json` or `$DOCKER_CONFIG/config.json`.

---

## 5. Supply Chain Security & Immutability

### Digest Pinning in Production

While semantic tags (`:v1.2.0`) are convenient during development, production GitOps configurations should pin the immutable OCI digest:

```bash
ktpl oci://ghcr.io/my-org/web-service@sha256:d8b746857e4e1a0b3f8... overlays/prod/
```

This guarantees byte-level reproducibility and eliminates tag-mutation attacks.

### Signing with Cosign (Sigstore)

Because `ktpl` packages are standard OCI artifacts, they integrate natively with `cosign`:

```bash
# 1. Package and push the artifact
ktpl push manifests/ ghcr.io/my-org/web-service:v1.0.0

# 2. Sign the artifact with a private key or keyless (OIDC)
cosign sign --yes ghcr.io/my-org/web-service:v1.0.0

# 3. Verify signature in CI or admission controller
cosign verify --certificate-identity-regexp ".*@my-org.com" \
              --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
              ghcr.io/my-org/web-service:v1.0.0
```

---

## 6. Air-Gapped Environments

For sovereign clouds, defense installations, or on-premise networks without registry access:

1. **Package on a connected workstation**:
   ```bash
   ktpl package manifests/ -o release-v1.0.0.tar
   ```
2. **Transfer the `.tar` archive** across the security boundary.
3. **Render locally using the archive directly**:
   ```bash
   ktpl release-v1.0.0.tar overlays/prod/
   ```
No network daemon, local registry, or container runtime is required on the target host.
