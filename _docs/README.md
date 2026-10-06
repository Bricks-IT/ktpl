# ktpl Platform & SRE Documentation

This directory contains technical guides and architectural references for **Platform Engineers**, **Site Reliability Engineers (SREs)**, and **Kubernetes Operators** evaluating, adopting, and operating `ktpl` at scale.

## Index of Guides

| Guide | Target Audience & Focus |
|---|---|
| [01. Platform Architecture & Core Invariants](01-platform-architecture.md) | Architectural foundations, mental model, comparison with Helm/Kustomize/CUE, fixed-point Jacobi iterations, typing rules, and design guarantees. |
| [02. Enterprise Golden Paths & OCI Distribution](02-golden-paths-and-oci.md) | Designing reusable organizational blueprints, multi-layer OCI compositions, registries, supply chain security (Cosign), and air-gapped deployment patterns. |
| [03. GitOps Integration (Argo CD, Flux & CI/CD)](03-gitops-integration.md) | Production setup for Argo CD (CMP v2 sidecar), Flux v2, automated CI validation pipelines (`kubeconform`, `trivy`), and caching strategies. |
| [04. Day-2 Operations, Auditing & Troubleshooting](04-day2-operations-and-troubleshooting.md) | Production telemetry (`ktpl.io/rendered`, `ktpl.io/sources`), step-by-step debugging (`--step`, `--stop-after`), cycle detection, and escaping templates in CRDs. |

---

## Quick Architecture Summary

```
                      +---------------------------------------+
                      |   Standard OCI Registry (GHCR/ECR)    |
                      |  oci://my-org/platform-base:v1.2.0    |
                      +-------------------+-------------------+
                                          |
                                          | (ktpl pull / stream)
                                          v
+-----------------------+     +-----------+-----------+     +-----------------------+
|  Local Manifests      |     |  ktpl Iteration       |     |  Rendered Kubernetes  |
|  or Environment       | --> |  Fixed-Point Engine   | --> |  Manifests            |
|  Overlays (prod/)     |     |  (Jacobi Snapshots)   |     |  (Valid, Typed YAML)  |
+-----------------------+     +-----------+-----------+     +-----------+-----------+
                                          |                             |
                                          v                             v
                              Per-Iteration Linters              Argo CD / kubectl apply
                              (RFC 1123, Types, Cycles)
```

## Why ktpl for Platform Teams?

1. **Elimination of the "Values Indirection Tax"**: In Helm, every configuration point must be wired through `values.yaml` and helper templates. In `ktpl`, native Kubernetes manifests reference each other directly (`ref`).
2. **Valid YAML Everywhere**: All source manifests are valid YAML. IDE syntax completion, Kubernetes JSON schemas, and security scanners (`kubeconform`, `trivy`, `checkov`) work directly on source files.
3. **Composable OCI Artifacts**: Platform teams ship versioned, signed OCI artifacts; application teams apply lightweight GitOps overlays without cloning submodules or vendoring code.
4. **Zero Infrastructure & Complete Determinism**: Static Go binary, zero cluster connection required, hermetic rendering, byte-identical outputs, and total traceability via metadata annotations.
