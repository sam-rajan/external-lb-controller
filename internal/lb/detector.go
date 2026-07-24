package lb

import "context"

// Detector probes the runtime environment and returns a Provider if this
// implementation is applicable, or (nil, false) if not.
type Detector func(ctx context.Context, cfg ProviderConfig) (Provider, bool)

var registry []Detector

// Register adds a Detector to the auto-detection registry.
// Cloud-specific packages call this from their init() functions.
func Register(d Detector) {
	registry = append(registry, d)
}
