package aws

import (
	"fmt"
	"hash/fnv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	lbNameSuffix = "-lb"
	sgNameSuffix = "-sg"
)

// loadBalancerName returns the name to use for the load balancer itself:
// annotationLBName if set, otherwise "<namespace>-<name>". Either way a "-lb"
// suffix is appended if not already present, kept within AWS's 32-char limit.
func loadBalancerName(svc *corev1.Service) string {
	base := svc.Annotations[annotationLBName]
	if base == "" {
		base = fmt.Sprintf("%s-%s", svc.Namespace, svc.Name)
	}
	return withSuffix(base, lbNameSuffix)
}

// securityGroupName derives the auto-managed security group's name from the
// resolved load balancer name, swapping its "-lb" suffix for "-sg" so the two
// stay paired regardless of whether the LB name came from the annotation or
// the derived default.
func securityGroupName(svc *corev1.Service) string {
	base := strings.TrimSuffix(loadBalancerName(svc), lbNameSuffix)
	return withSuffix(base, sgNameSuffix)
}

// tgName returns a name ≤32 chars for a target group, using the same
// hash-based truncation as withSuffix so long, similar names don't collide.
func tgName(svc *corev1.Service, port int32) string {
	return withSuffix(fmt.Sprintf("%s-%s-%d", svc.Namespace, svc.Name, port), "")
}

// withSuffix appends suffix to name (if not already present) while staying
// within AWS's 32-character name limit. Names that don't fit get a
// hash-of-the-full-name segment instead of a blind cut, so two names sharing
// a long common prefix don't collide once truncated, and the cut point can't
// land on a trailing hyphen.
func withSuffix(name, suffix string) string {
	name = strings.TrimSuffix(name, suffix)

	const maxLen = 32
	full := name + suffix
	if len(full) <= maxLen {
		return full
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(full))
	hash := fmt.Sprintf("-%08x", h.Sum32())

	base := strings.TrimRight(name[:maxLen-len(hash)-len(suffix)], "-")
	return base + hash + suffix
}
