# Design & Project Decisions

This document records the foundational decisions for the project. It is the
source of truth for identity, scope, and positioning; implementation details
live in code and in `docs/` topic pages.

## Identity

| Attribute | Value |
|---|---|
| Project name | `external-lb-controller` |
| Go module | `github.com/sam-rajan/external-lb-controller` |
| Repository | https://github.com/sam-rajan/external-lb-controller |
| Kubebuilder domain | `external-lb.io` |
| `loadBalancerClass` | `external-lb.io/lb` |
| Finalizer | `external-lb.io/lb-cleanup` |
| Cloud resource tag | `external-lb.io/service` |
| License | Apache-2.0 (`LICENSE`, `NOTICE`) |
| Minimum Go | 1.26 |

The `external-lb.io` domain is used only as an opaque API identifier (class string,
annotation prefixes, finalizer, tags). It is not required to resolve. The
`loadBalancerClass` string is a **hard API contract**: once users adopt it, it
cannot change without a migration path.

## Scope

**Goal: multi-cloud.** The controller is a generic, out-of-tree implementation
of the Kubernetes `loadBalancerClass` mechanism. AWS (ELBv2) is the first
provider; GCP and Azure are on the roadmap.

Consequences:

- Naming, documentation, and CLI stay cloud-neutral. Cloud-specific behavior is
  reached only through the `internal/lb.Provider` interface and provider
  sub-packages (`internal/lb/aws`, future `internal/lb/gcp`, ...).
- The provider registry / detector seam (`internal/lb/detector.go`) is kept and
  hardened rather than removed.
- Service annotations remain provider-namespaced. AWS reuses the
  `service.beta.kubernetes.io/aws-load-balancer-*` keys for familiarity with
  the upstream AWS Load Balancer Controller; other providers use their own
  prefixes.
- A single controller binary bundles all providers; the active provider is
  selected at startup (see "Provider selection" below — currently detection is
  a known weak point tracked on the roadmap).

## Positioning

### vs. in-tree cloud providers / `cloud-provider-aws`

The in-tree/`cloud-provider-aws` service controller reconciles **every**
`type: LoadBalancer` Service in the cluster and is coupled to the cluster's
cloud provider. This project reconciles **only** Services that explicitly opt
in with `loadBalancerClass: external-lb.io/lb`, so it can run alongside an existing
cloud provider during migration, or in clusters with no cloud provider at all
(e.g. self-managed nodes on EC2).

### vs. AWS Load Balancer Controller (`kubernetes-sigs/aws-load-balancer-controller`)

That project is the mature, feature-complete choice for AWS and also serves
`Ingress`. This project is intentionally smaller in surface area, multi-cloud
in ambition, and focused solely on the `Service` + `loadBalancerClass` path. It
is **not** a drop-in replacement today. The README must state this plainly and
point users who need production AWS features to the SIGs project until parity
milestones are reached.

## Architecture (current)

```
Service (type=LoadBalancer, loadBalancerClass=external-lb.io/lb)
  │
  ▼
internal/controller (ServiceReconciler)
  • class filter + finalizer
  • gathers EndpointSlices
  • writes status.loadBalancer.ingress
  │
  ▼
internal/lb.Manager ──selects──▶ internal/lb.Provider
                                   │
                                   ▼
                                 internal/lb/aws (ELBv2: NLB/ALB, target groups,
                                 listeners, security groups, subnet discovery)

internal/webhook/service — validating webhook for the annotations, delegates
provider-specific checks to registered lb.AnnotationValidator funcs.
```

## Provider selection

Today the AWS provider "detects" itself by attempting to load an AWS SDK
config, which nearly always succeeds and is therefore not a real check. The
target design is an explicit `--cloud-provider` flag (`aws`, `gcp`, `azure`,
`auto`) with `auto` doing a genuine environment probe (IMDS / metadata server /
verified credentials). Tracked in the roadmap as a Phase 1 correctness item.

## Open decisions (revisit before v0.1.0)

- Whether to keep the AWS annotation keys under the `aws-load-balancer-*`
  namespace long-term or move to `external-lb.io/*` for all providers.
- Single LB per Service (current) vs. optional LB sharing/consolidation.
- Versioning policy for the `loadBalancerClass` string.
