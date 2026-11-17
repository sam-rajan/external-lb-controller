# Changelog

All notable changes to this project are documented here. The format is based
on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
follows [Semantic Versioning](https://semver.org/). Until v1.0.0, minor releases
may include breaking changes.

## [Unreleased]

## [0.1.0] - 2026-11-17

### Added
- Controller for `Service` objects with `loadBalancerClass: external-lb.io/lb`,
  provisioning AWS NLBs and ALBs (target groups, listeners, security groups,
  subnet discovery, TLS via ACM certificates).
- Validating admission webhook for Services using the class.
- `deploy/install.yaml`, a complete install manifest committed to the repository and
  checked in CI against `config/` (`make install-manifest`).
- `CLUSTER_NAME` environment variable on the manager, so the cluster name can be set with
  `kubectl set env`, and `docs/iam-policy.json` with the required IAM actions.
- cert-manager `Issuer` and `Certificate` in `config/certmanager` for the webhook serving
  certificate, with CA injection into the webhook configuration.
- `--cloud-provider`, `--cluster-name` and `--enable-http2` flags (HTTP/2 is off by default).
- `external-lb.io/skip-lb-cleanup` escape hatch for Services whose cloud load
  balancer can never be deleted.
- Unit tests for the controller, webhook, provider selection and AWS logic.
- Release workflow publishing a multi-arch (amd64, arm64) image to GHCR, with a
  `latest` tag for stable releases, and a versioned `install.yaml` to GitHub
  Releases on `v*` tags.
- README, `docs/operations.md`, and community files.

[Unreleased]: https://github.com/sam-rajan/external-lb-controller/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/sam-rajan/external-lb-controller/releases/tag/v0.1.0
