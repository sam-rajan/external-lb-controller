# external-lb-controller

An out-of-tree Kubernetes controller that provisions cloud load balancers for
`Service` objects of `type: LoadBalancer` that opt in with
`loadBalancerClass: external-lb.io/lb`.

AWS (ELBv2: NLB and ALB) is the first supported provider. GCP and Azure are on
the roadmap.

> **Status: early development.** This is **not** a drop-in replacement for the
> [AWS Load Balancer Controller](https://github.com/kubernetes-sigs/aws-load-balancer-controller).
> If you need production-grade AWS features (or `Ingress` support), use that
> project. See [docs/design.md](docs/design.md) for scope and positioning.

## Description

The controller reconciles **only** Services that set
`spec.loadBalancerClass: external-lb.io/lb`. It does not touch other
`type: LoadBalancer` Services, so it can run next to the in-tree cloud provider
or `cloud-provider-aws`, or in clusters with no cloud provider at all (for
example self-managed nodes on EC2).

For each opted-in Service the controller:

1. Adds the finalizer `external-lb.io/lb-cleanup`.
2. Creates a load balancer, target groups, listeners and (unless you supply
   your own) a security group on AWS. Every resource is tagged
   `external-lb.io/service`.
3. Registers targets: cluster nodes on the Service's NodePort
   (`target-type: instance`, the default) or ready endpoint IPs
   (`target-type: ip`).
4. Writes the load balancer address to `status.loadBalancer.ingress`.
5. Deletes the cloud resources and removes the finalizer when the Service is
   deleted.

A validating webhook rejects invalid Services at admission time (see
[Validation rules](#validation-rules)).

## Usage

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-scheme: internet-facing
spec:
  type: LoadBalancer
  loadBalancerClass: external-lb.io/lb
  selector:
    app: web
  ports:
    - name: http
      protocol: TCP
      port: 80
      targetPort: 8080
```

More examples are in [examples/](examples/) (`service.yml` shows TLS termination with
an ACM certificate and a pre-existing security group; replace the placeholder IDs and ARN with your own).

### Supported annotations (AWS)

Keys reuse the upstream `service.beta.kubernetes.io/aws-load-balancer-*`
names. All are optional.

| Annotation suffix | Values | Default | Notes |
|---|---|---|---|
| `type` | `nlb`, `alb` | `nlb` | |
| `scheme` | `internet-facing`, `internal` | `internet-facing` | Immutable once set |
| `name` | 1-32 chars, alphanumeric/hyphen, not starting or ending with a hyphen | `<namespace>-<name>` | Immutable once set. A `-lb` suffix is appended if missing |
| `target-type` | `instance`, `ip` | `instance` | Immutable once set |
| `subnets` | comma-separated subnet IDs | auto-discovered | See [Subnet discovery](#subnet-discovery) |
| `security-groups` | comma-separated security group IDs | controller creates one | |
| `ssl-cert` | comma-separated ACM certificate ARNs | none | The first ARN is the default certificate |
| `ssl-ports` | comma-separated ports, or `*` | all ports | Only used when `ssl-cert` is set |
| `attributes` | comma-separated `key=value` ELBv2 load balancer attributes | none | e.g. `load_balancing.cross_zone.enabled=true` |
| `healthcheck-protocol` | | ELBv2 default | Target group health check |
| `healthcheck-port` | | ELBv2 default | |
| `healthcheck-path` | | ELBv2 default | |
| `healthcheck-interval-seconds` | | ELBv2 default | |
| `healthcheck-timeout-seconds` | | ELBv2 default | |
| `healthy-threshold-count` | | ELBv2 default | |
| `unhealthy-threshold-count` | | ELBv2 default | |

Prefix every suffix with `service.beta.kubernetes.io/aws-load-balancer-`.

Controller-level annotation:

| Annotation | Purpose |
|---|---|
| `external-lb.io/skip-lb-cleanup: "true"` | Escape hatch (see [docs/operations.md](docs/operations.md)). On deletion, skip deleting the cloud load balancer and just remove the finalizer. Use only when cleanup can never succeed (revoked credentials, cluster moved off the provider). **This can orphan cloud resources.** |

### Validation rules

The webhook (`failurePolicy: Fail`) checks Services with
`loadBalancerClass: external-lb.io/lb` on create and update:

- `spec.type` must be `LoadBalancer`.
- `spec.ports` must not be empty, and every port must use `TCP`.
- `type`, `scheme`, `name` and `target-type` annotations must hold valid values.
- `spec.loadBalancerClass`, and the `scheme`, `name` and `target-type`
  annotations, cannot be changed once set.

### Subnet discovery

If a Service has no `subnets` annotation, the controller needs `--cluster-name`
and looks for subnets tagged:

- `kubernetes.io/cluster/<cluster-name>` = `shared` or `owned`, and
- `kubernetes.io/role/elb` (internet-facing) or `kubernetes.io/role/internal-elb` (internal).

## Configuration

Controller flags (see [cmd/flags.go](cmd/flags.go)):

| Flag | Default | Description |
|---|---|---|
| `--cloud-provider` | `auto` | Provider to use (`aws`), or `auto` to probe registered providers and use the first whose environment check succeeds. For AWS the check is a successful `sts:GetCallerIdentity` |
| `--cluster-name` | `""` | Cluster name used for subnet auto-discovery |
| `--leader-elect` | `false` | Enable leader election |
| `--health-probe-bind-address` | `:8081` | Health probe address |
| `--metrics-bind-address` | `0` (disabled) | Metrics address, e.g. `:8443` |
| `--metrics-secure` | `true` | Serve metrics over HTTPS |
| `--webhook-cert-path` | `""` | Directory with the webhook TLS cert/key |
| `--webhook-cert-name`, `--webhook-cert-key` | `tls.crt`, `tls.key` | Webhook cert file names |
| `--metrics-cert-path`, `--metrics-cert-name`, `--metrics-cert-key` | | Metrics server TLS settings |
| `--zap-*` | | Logging options from controller-runtime |

AWS credentials and region come from the standard AWS SDK default chain
(environment variables, shared config, IRSA / web identity, EC2 instance
profile).

### AWS IAM permissions

The controller's identity needs the following actions (derived from the calls
the AWS provider makes):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "elasticloadbalancing:CreateLoadBalancer",
        "elasticloadbalancing:DeleteLoadBalancer",
        "elasticloadbalancing:ModifyLoadBalancerAttributes",
        "elasticloadbalancing:SetSubnets",
        "elasticloadbalancing:CreateTargetGroup",
        "elasticloadbalancing:DeleteTargetGroup",
        "elasticloadbalancing:ModifyTargetGroup",
        "elasticloadbalancing:RegisterTargets",
        "elasticloadbalancing:DeregisterTargets",
        "elasticloadbalancing:DescribeTargetHealth",
        "elasticloadbalancing:CreateListener",
        "elasticloadbalancing:ModifyListener",
        "elasticloadbalancing:DeleteListener",
        "elasticloadbalancing:AddListenerCertificates",
        "elasticloadbalancing:RemoveListenerCertificates",
        "elasticloadbalancing:DescribeListenerCertificates",
        "elasticloadbalancing:DescribeLoadBalancers",
        "elasticloadbalancing:DescribeTargetGroups",
        "elasticloadbalancing:DescribeListeners",
        "elasticloadbalancing:DescribeTags",
        "elasticloadbalancing:AddTags",
        "ec2:DescribeVpcs",
        "ec2:DescribeSubnets",
        "ec2:DescribeSecurityGroups",
        "ec2:CreateSecurityGroup",
        "ec2:DeleteSecurityGroup",
        "ec2:AuthorizeSecurityGroupIngress",
        "ec2:RevokeSecurityGroupIngress",
        "ec2:CreateTags"
      ],
      "Resource": "*"
    }
  ]
}
```

`sts:GetCallerIdentity` is also called but needs no policy statement.
`AddTags` and `ec2:CreateTags` are needed because resources are created with
the `external-lb.io/service` tag; the `Describe*` entries cover the paginated lookups.
Consider scoping the write actions with conditions on that tag. Validate the
policy in a test account before relying on it.

## Getting Started

### Prerequisites

- Go v1.26+ (to build)
- Docker (to build the image)
- `kubectl` and a Kubernetes cluster you can reach (the controller uses
  `EndpointSlice`, so v1.21+ is required)
- AWS credentials with the permissions above
- A TLS Secret for the webhook (see below)

### Install from a release

Tagged releases publish a multi-arch (`linux/amd64`, `linux/arm64`) image to
`ghcr.io/sam-rajan/external-lb-controller:<tag>` (stable releases also move the
`latest` tag; pre-releases such as `v0.1.0-rc1` do not) and attach a
versioned `install.yaml` to the
[GitHub Release](https://github.com/sam-rajan/external-lb-controller/releases).
No release has been published yet. Once one exists:

```sh
kubectl apply -f https://github.com/sam-rajan/external-lb-controller/releases/download/<tag>/install.yaml
```

The bundle does not include the webhook serving certificate or the webhook
`caBundle`, and the pod needs AWS credentials and `--cluster-name`; follow
steps 2 and 3 below before the controller will work. There is no Helm chart.

### Deploy from source

1. Build and push the image:

   ```sh
   make docker-build docker-push IMG=<registry>/external-lb-controller:<tag>
   ```

2. Create the webhook serving certificate. The manager mounts a Secret named
   `webhook-server-cert` (keys `tls.crt`, `tls.key`) in
   the `external-lb-controller-system` namespace. The repository does not
   provision it (no cert-manager integration yet), and the
   `ValidatingWebhookConfiguration` needs a matching `caBundle`.

3. Set `--cluster-name` and give the pod AWS credentials (for example IRSA or
   the node instance profile) by patching `config/manager/manager.yaml`.

4. Deploy:

   ```sh
   make deploy IMG=<registry>/external-lb-controller:<tag>
   ```

   There are no CRDs, so `make install` is a no-op.

5. Create a Service as shown in [Usage](#usage).

### Run locally

```sh
make run
```

This generates a self-signed webhook certificate and runs the manager against
your current kubeconfig context.

### Uninstall

Delete the Services that use the class first, so the controller can remove
their cloud load balancers (see [docs/operations.md](docs/operations.md) if one
gets stuck), then:

```sh
make undeploy
```

## Development

```sh
make help       # list all targets
make test       # unit tests (envtest)
make lint-fix   # lint and auto-fix
make test-e2e   # e2e tests against a dedicated Kind cluster
```

E2E tests create their own Kind cluster; do not point them at a real cluster.

## Documentation and community

- [docs/operations.md](docs/operations.md): ownership, AWS tagging model, deletion and the `skip-lb-cleanup` escape hatch, troubleshooting
- [docs/design.md](docs/design.md): scope, positioning and open decisions
- [CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), [CHANGELOG.md](CHANGELOG.md)
- [SECURITY.md](SECURITY.md): report vulnerabilities privately; do not open a public issue

## License

Copyright 2026 The external-lb-controller Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
