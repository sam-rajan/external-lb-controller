# Changelog

All notable changes to this project are documented here. The format is based
on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
intends to follow [Semantic Versioning](https://semver.org/) once it reaches
v0.1.0. Until then, anything may change.

## [Unreleased]

### Added
- Controller for `Service` objects with `loadBalancerClass: otlbc.io/lb`,
  provisioning AWS NLBs and ALBs (target groups, listeners, security groups,
  subnet discovery, TLS via ACM certificates).
- Validating admission webhook for Services using the class.
- `--cloud-provider` and `--cluster-name` flags.
- `otlbc.io/skip-lb-cleanup` escape hatch for Services whose cloud load
  balancer can never be deleted.
- Unit tests for the controller, webhook, provider selection and AWS logic.
- Release workflow publishing the image to GHCR and a versioned
  `install.yaml` to GitHub Releases on `v*` tags.
- README, `docs/operations.md`, and community files.

[Unreleased]: https://github.com/sam-rajan/out-tree-loadbalancer-controller/commits/main
