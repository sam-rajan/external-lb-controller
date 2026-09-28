package lb

import "context"

// Detector probes the runtime environment and returns a Provider if this
// implementation is applicable, or (nil, false) if not.
type Detector func(ctx context.Context, cfg ProviderConfig) (Provider, bool)

type namedDetector struct {
	name   string
	detect Detector
}

var registry []namedDetector

// Register adds a named Detector to the registry. Cloud-specific packages
// call this from their init() functions. name is the value operators pass to
// --cloud-provider to select this provider explicitly (e.g. "aws").
func Register(name string, d Detector) {
	registry = append(registry, namedDetector{name: name, detect: d})
}
