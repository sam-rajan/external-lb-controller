package lb

import "context"

// Detector returns a Provider if it applies to the current environment.
type Detector func(ctx context.Context, cfg ProviderConfig) (Provider, bool)

type namedDetector struct {
	name   string
	detect Detector
}

var registry []namedDetector

func Register(name string, d Detector) {
	registry = append(registry, namedDetector{name: name, detect: d})
}
