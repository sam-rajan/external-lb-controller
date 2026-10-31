# Security Policy

This controller holds cloud credentials and creates, modifies and deletes
cloud load balancers, security groups and listeners. Security reports are
taken seriously.

## Supported versions

The project is pre-release. Only the latest commit on `main` and the most
recent tagged release (once one exists) receive fixes.

## Reporting a vulnerability

**Do not open a public issue.** Report privately using GitHub's
[private vulnerability reporting](https://github.com/sam-rajan/out-tree-loadbalancer-controller/security/advisories/new).

Please include the affected version or commit, reproduction steps, and the
impact you see. Expect an acknowledgement within a few days. This is a
volunteer project, so there is no guaranteed response time, but reports are
prioritized over other work.

## Areas of particular interest

- Anything that lets a user without cloud permissions cause the controller to
  create, modify or delete cloud resources it does not own (the controller
  identifies its resources by the `otlbc.io/service` tag).
- Bypasses of the admission webhook, or of the immutability rules on
  `loadBalancerClass` and the `scheme`, `name` and `target-type` annotations.
- Overly broad default RBAC or IAM permissions.
- Credential exposure through logs, events or Service status.

## Deployment guidance

- Grant the controller the narrowest IAM policy that works; see the README.
  Prefer IRSA / workload identity over node instance profiles.
- Anyone who can create a Service with `loadBalancerClass: otlbc.io/lb` can
  make the controller create cloud resources. Restrict that with
  admission policy or RBAC if your cluster has untrusted tenants.
- Annotations such as `security-groups` and `subnets` let a Service author
  choose cloud resources the controller then attaches to. Treat Service write
  access as cloud-resource access.
