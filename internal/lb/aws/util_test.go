package aws

import (
	"reflect"
	"sort"
	"testing"

	discoveryv1 "k8s.io/api/discovery/v1"
)

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a,b", []string{"a", "b"}},
		{" a , b ,, c ,", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		if got := splitCSV(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func slice(eps ...discoveryv1.Endpoint) discoveryv1.EndpointSlice {
	return discoveryv1.EndpointSlice{Endpoints: eps}
}

func ep(ready *bool, node *string, addrs ...string) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses:  addrs,
		NodeName:   node,
		Conditions: discoveryv1.EndpointConditions{Ready: ready},
	}
}

func TestEndpointTargets(t *testing.T) {
	slices := []discoveryv1.EndpointSlice{
		slice(
			ep(new(true), nil, "10.0.0.1"),
			ep(new(false), nil, "10.0.0.2"),
			ep(nil, nil, "10.0.0.3"),
		),
		slice(ep(new(true), nil, "10.0.1.1", "10.0.1.2")),
	}
	want := []string{"10.0.0.1", "10.0.1.1", "10.0.1.2"}
	if got := endpointTargets(slices); !reflect.DeepEqual(got, want) {
		t.Errorf("endpointTargets = %v, want %v", got, want)
	}
	if got := endpointTargets(nil); len(got) != 0 {
		t.Errorf("endpointTargets(nil) = %v, want empty", got)
	}
}

func TestReadyNodeNames(t *testing.T) {
	slices := []discoveryv1.EndpointSlice{
		slice(
			ep(new(true), new("node-a"), "10.0.0.1"),
			ep(new(true), new("node-a"), "10.0.0.2"),
			ep(new(false), new("node-b"), "10.0.0.3"),
			ep(new(true), nil, "10.0.0.4"),
		),
		slice(ep(new(true), new("node-c"), "10.0.1.1")),
	}
	got := readyNodeNames(slices)
	sort.Strings(got)
	if want := []string{"node-a", "node-c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("readyNodeNames = %v, want %v", got, want)
	}
}

func TestOwnerTagValue(t *testing.T) {
	if got := ownerTagValue(svcWith(nil)); got != "ns/web" {
		t.Errorf("ownerTagValue = %q, want ns/web", got)
	}
}

func TestSubnetSetEqual(t *testing.T) {
	tests := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
	}
	for _, tt := range tests {
		if got := subnetSetEqual(tt.a, tt.b); got != tt.want {
			t.Errorf("subnetSetEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseLBAttributes(t *testing.T) {
	got := parseLBAttributes(" load_balancing.cross_zone.enabled = true, bogus ,deletion_protection.enabled=true")
	if len(got) != 2 {
		t.Fatalf("got %d attributes, want 2: %v", len(got), got)
	}
	if *got[0].Key != "load_balancing.cross_zone.enabled" || *got[0].Value != "true" {
		t.Errorf("attr[0] = %s=%s", *got[0].Key, *got[0].Value)
	}
	if *got[1].Key != "deletion_protection.enabled" || *got[1].Value != "true" {
		t.Errorf("attr[1] = %s=%s", *got[1].Key, *got[1].Value)
	}
	if got := parseLBAttributes(""); len(got) != 0 {
		t.Errorf("empty input produced %v", got)
	}
}

func TestHealthCheckInt32(t *testing.T) {
	const key = annotationHealthCheckIntervalSeconds
	if v, err := healthCheckInt32(svcWith(nil), key); err != nil || v != nil {
		t.Errorf("unset: got (%v, %v), want (nil, nil)", v, err)
	}
	if v, err := healthCheckInt32(svcWith(map[string]string{key: "30"}), key); err != nil || v == nil || *v != 30 {
		t.Errorf("valid: got (%v, %v), want 30", v, err)
	}
	if _, err := healthCheckInt32(svcWith(map[string]string{key: "abc"}), key); err == nil {
		t.Error("invalid value: want error, got nil")
	}
}

func TestTCPIPPermission(t *testing.T) {
	p := tcpIpPermission(443, nil)
	if *p.IpProtocol != "tcp" || *p.FromPort != 443 || *p.ToPort != 443 {
		t.Errorf("unexpected permission %+v", p)
	}
}
