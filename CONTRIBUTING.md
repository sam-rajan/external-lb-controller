# Contributing

Thanks for your interest. Please read the [Code of Conduct](CODE_OF_CONDUCT.md)
first, and report security issues privately as described in
[SECURITY.md](SECURITY.md).

## Before you start

Read [docs/design.md](docs/design.md): it records scope (multi-cloud, AWS
first), the `external-lb.io/lb` class contract and open decisions. For anything
larger than a small fix, open an issue first so we can agree on the approach.

## Development setup

Requires Go 1.26+ and Docker.

```sh
make test       # unit tests
make lint       # golangci-lint
make lint-fix   # auto-fix style issues
make test-e2e   # e2e tests; creates its own Kind cluster, never use a real one
make run        # run the manager locally against your current kubeconfig
```

Run `make help` for every target.

## Project layout

| Path | Contents |
|---|---|
| `cmd/` | Manager entry point and flags |
| `internal/controller/` | `Service` reconciler (class filter, finalizer, status) |
| `internal/lb/` | Provider interface, registry and provider selection |
| `internal/lb/aws/` | AWS ELBv2 provider |
| `internal/webhook/service/` | Validating admission webhook |
| `docs/` | Design and operations documentation |
| `examples/` | Example Services (use placeholder IDs only) |

Generated files (`config/rbac/role.yaml`, `config/webhook/manifests.yaml`,
`zz_generated.*`) come from `make manifests generate`. Do not edit them by
hand; change the `+kubebuilder` markers instead. Do not remove
`+kubebuilder:scaffold` markers.

## Pull requests

- Keep changes focused, and add or update unit tests. Code that talks to a
  cloud API should be structured so its logic can be tested without credentials.
- Run `make test` and `make lint` before pushing.
- Update the README or `docs/` if you change behavior, annotations, flags or
  required IAM permissions, and add a line under "Unreleased" in
  [CHANGELOG.md](CHANGELOG.md).
- Log messages follow the Kubernetes style: start with a capital, no trailing
  period, past tense for failures, and balanced key/value pairs.
- Never commit credentials, real account IDs, ARNs, subnet or security group
  IDs. Use placeholders such as `111122223333` and `subnet-0123456789abcdef0`.

## Adding a cloud provider

Implement `lb.Provider`, register it with `lb.Register("<name>", detect)` from
the provider package's `init()`, and register any annotation validators with
`lb.RegisterValidator`. Use provider-specific annotation prefixes. Cloud-specific
code stays under `internal/lb/<provider>/`.

## Releases

Maintainers release by pushing a `v*` tag. The
[release workflow](.github/workflows/release.yml) runs the tests, publishes
`ghcr.io/sam-rajan/external-lb-controller:<tag>` and attaches
`install.yaml` to a GitHub Release. Move the "Unreleased" changelog entries
under the new version first. After a release, point the committed manifest at
the new image with `make install-manifest IMG=ghcr.io/sam-rajan/external-lb-controller:<tag>`;
CI fails if `deploy/install.yaml` no longer matches `config/`.

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
