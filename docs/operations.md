# Operations

How the controller identifies what it owns, what it creates on AWS, and what
to do when deletion goes wrong. For flags, annotations and the IAM policy see
the [README](../README.md).

## Ownership

A Service is managed only if **both** hold:

- `spec.type` is `LoadBalancer`, and
- `spec.loadBalancerClass` is exactly `otlbc.io/lb`.

Everything else is ignored, including `type: LoadBalancer` Services without
the class. The admission webhook makes `loadBalancerClass` immutable once set,
so a Service cannot be moved out from under its finalizer.

Managed Services carry the finalizer `otlbc.io/lb-cleanup`. It is added
**before** any cloud resource is created, so a Service can't be deleted while
cloud resources may exist that the controller hasn't cleaned up.

## Tagging model (AWS)

The controller finds its resources by tag, not by stored IDs, so it can
recover after restarts. The tag key is `otlbc.io/service`.

| AWS resource | Tag value | Notes |
|---|---|---|
| Load balancer | `<namespace>/<name>` | One load balancer per Service |
| Target group | `<namespace>/<name>:<service port>` | One per Service port |
| Security group | `<namespace>/<name>` | Only when the controller creates it |

Naming (separate from tagging): the load balancer is named
`<namespace>-<name>-lb` (or the `aws-load-balancer-name` annotation plus `-lb`),
the security group `...-sg`. Names over 32 characters are shortened with a hash
so similar long names don't collide.

Consequences:

- **Don't remove or edit the `otlbc.io/service` tag** on these resources. The
  controller would no longer find them: it would create duplicates on the next
  reconcile and could not delete the originals.
- **Renaming or recreating a Service** with a different namespace/name gives a
  new tag value, so it gets a new load balancer.
- Security groups you supply with `aws-load-balancer-security-groups` are never
  tagged or deleted by the controller. Only a security group it created
  (tagged) is removed on cleanup.
- Use the tag in IAM conditions or cost allocation to see everything this
  controller manages.

## Deletion

When a managed Service is deleted the controller, in order:

1. Deletes the listeners and the load balancer.
2. Waits (up to 5 minutes) for AWS to finish deleting the load balancer.
3. Deletes the target groups.
4. Deletes the security group it created, retrying while AWS releases it.
5. Removes the finalizer.

Every step is retried on the next reconcile if it fails. If any step fails the
finalizer stays, so the Service remains in `Terminating` rather than leaking
cloud resources silently. Look at the controller logs for the AWS error;
common causes are missing IAM permissions, a deletion-protected load balancer
(`deletion_protection.enabled=true` in the `attributes` annotation), or a
security group still referenced by another resource.

### Escape hatch: `otlbc.io/skip-lb-cleanup`

If cleanup can never succeed (credentials revoked, AWS account closed, the
cluster no longer runs where the resources were created), a Service can be
stuck in `Terminating` forever. An operator can unblock it:

```sh
kubectl annotate service <name> -n <namespace> otlbc.io/skip-lb-cleanup=true
```

The controller then **skips all cloud deletion**, emits a
`LoadBalancerCleanupSkipped` warning event, and removes the finalizer.

**This can orphan the load balancer, target groups and security group, and you
keep paying for them.** Before using it, try to fix the underlying error. Afterwards,
find the leftovers by the `otlbc.io/service` tag and delete them manually.

Do not set it casually; it is meant for operators, not for routine Service
authors. Restrict who can annotate Services if that matters in your cluster.

## Troubleshooting

| Symptom | Check |
|---|---|
| Service never gets an address | Controller logs; IAM permissions; subnets (annotation or `--cluster-name` tags); `kubectl get endpointslices` for ready endpoints |
| Admission error on create/update | The message names the failing field: non-TCP port, bad annotation value, or an immutable annotation change |
| Service stuck in `Terminating` | Controller logs for the AWS error; see the deletion section above |
| Controller exits at startup | `--cloud-provider=aws` requires working AWS credentials (`sts:GetCallerIdentity` must succeed); with `auto` it needs at least one provider to pass |
| Nothing happens for a Service | Confirm `type: LoadBalancer` and the exact class `otlbc.io/lb` |
