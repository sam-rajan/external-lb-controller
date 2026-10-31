package lb

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

type stubProvider struct {
	name      string
	ensureErr error
	ensured   int
	deleted   int
}

func (s *stubProvider) EnsureLB(context.Context, *corev1.Service, []discoveryv1.EndpointSlice) (string, error) {
	s.ensured++
	return s.name + ".example.com", s.ensureErr
}

func (s *stubProvider) DeleteLB(context.Context, *corev1.Service) error {
	s.deleted++
	return nil
}

func detector(p Provider, ok bool, probed *[]string, name string) Detector {
	return func(context.Context, ProviderConfig) (Provider, bool) {
		*probed = append(*probed, name)
		if !ok {
			return nil, false
		}
		return p, true
	}
}

// withRegistry swaps the global registry for the duration of the test.
func withRegistry(t *testing.T, entries ...namedDetector) {
	t.Helper()
	saved := registry
	registry = entries
	t.Cleanup(func() { registry = saved })
}

func TestNewManagerAuto(t *testing.T) {
	var probed []string
	a, b := &stubProvider{name: "a"}, &stubProvider{name: "b"}
	withRegistry(t,
		namedDetector{"a", detector(a, false, &probed, "a")},
		namedDetector{"b", detector(b, true, &probed, "b")},
	)

	for _, mode := range []string{"auto", ""} {
		probed = nil
		m, err := NewManager(context.Background(), ProviderConfig{CloudProvider: mode})
		if err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		host, _ := m.EnsureLB(context.Background(), &corev1.Service{}, nil)
		if host != "b.example.com" {
			t.Errorf("mode %q: used provider %q, want b", mode, host)
		}
		if strings.Join(probed, ",") != "a,b" {
			t.Errorf("mode %q: probed %v, want [a b]", mode, probed)
		}
	}
}

func TestNewManagerAutoNoneDetected(t *testing.T) {
	var probed []string
	withRegistry(t, namedDetector{"a", detector(nil, false, &probed, "a")})
	if _, err := NewManager(context.Background(), ProviderConfig{CloudProvider: "auto"}); err == nil {
		t.Fatal("want error when no provider detected")
	}
}

func TestNewManagerExplicit(t *testing.T) {
	var probed []string
	a, b := &stubProvider{name: "a"}, &stubProvider{name: "b"}
	withRegistry(t,
		namedDetector{"a", detector(a, true, &probed, "a")},
		namedDetector{"b", detector(b, true, &probed, "b")},
	)

	m, err := NewManager(context.Background(), ProviderConfig{CloudProvider: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if host, _ := m.EnsureLB(context.Background(), &corev1.Service{}, nil); host != "b.example.com" {
		t.Errorf("used %q, want b", host)
	}
	if strings.Join(probed, ",") != "b" {
		t.Errorf("probed %v, want only [b]", probed)
	}
}

func TestNewManagerExplicitErrors(t *testing.T) {
	var probed []string
	withRegistry(t, namedDetector{"a", detector(nil, false, &probed, "a")})

	_, err := NewManager(context.Background(), ProviderConfig{CloudProvider: "a"})
	if err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Errorf("failing probe: got %v", err)
	}
	_, err = NewManager(context.Background(), ProviderConfig{CloudProvider: "gcp"})
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("unknown provider: got %v", err)
	}
}

func TestManagerDelegates(t *testing.T) {
	var probed []string
	p := &stubProvider{name: "a", ensureErr: errors.New("boom")}
	withRegistry(t, namedDetector{"a", detector(p, true, &probed, "a")})
	m, _ := NewManager(context.Background(), ProviderConfig{CloudProvider: "a"})

	if _, err := m.EnsureLB(context.Background(), &corev1.Service{}, nil); err == nil {
		t.Error("EnsureLB error was swallowed")
	}
	if err := m.DeleteLB(context.Background(), &corev1.Service{}); err != nil || p.deleted != 1 {
		t.Errorf("DeleteLB: err=%v deleted=%d", err, p.deleted)
	}
}

func TestValidatorRegistries(t *testing.T) {
	savedV, savedU := validators, updateValidators
	validators, updateValidators = nil, nil
	t.Cleanup(func() { validators, updateValidators = savedV, savedU })

	RegisterValidator(func(*corev1.Service) []string { return []string{"v1"} })
	RegisterValidator(func(*corev1.Service) []string { return []string{"v2", "v3"} })
	RegisterUpdateValidator(func(_, _ *corev1.Service) []string { return []string{"u1"} })

	if got := ValidateAnnotations(&corev1.Service{}); strings.Join(got, ",") != "v1,v2,v3" {
		t.Errorf("ValidateAnnotations = %v", got)
	}
	if got := ValidateAnnotationsUpdate(&corev1.Service{}, &corev1.Service{}); strings.Join(got, ",") != "u1" {
		t.Errorf("ValidateAnnotationsUpdate = %v", got)
	}
}
