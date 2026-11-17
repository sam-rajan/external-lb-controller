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

// loadBalancerName returns annotationLBName or "<namespace>-<name>", with a "-lb" suffix, within 32 chars.
func loadBalancerName(svc *corev1.Service) string {
	base := svc.Annotations[annotationLBName]
	if base == "" {
		base = fmt.Sprintf("%s-%s", svc.Namespace, svc.Name)
	}
	return withSuffix(base, lbNameSuffix)
}

func securityGroupName(svc *corev1.Service) string {
	base := strings.TrimSuffix(loadBalancerName(svc), lbNameSuffix)
	return withSuffix(base, sgNameSuffix)
}

func tgName(svc *corev1.Service, port int32) string {
	return withSuffix(fmt.Sprintf("%s-%s-%d", svc.Namespace, svc.Name, port), "")
}

// withSuffix appends suffix within AWS's 32-char limit. Overlong names get a hash segment, not a blind
// cut, so similar long names don't collide and the cut can't land on a hyphen.
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
