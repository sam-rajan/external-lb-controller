package aws

import (
	"strings"
	"testing"
)

func TestWithSuffix(t *testing.T) {
	tests := []struct {
		name, in, suffix, want string
	}{
		{"appends suffix", "web", "-lb", "web-lb"},
		{"does not duplicate suffix", "web-lb", "-lb", "web-lb"},
		{"exactly 32 chars is untouched", strings.Repeat("a", 29), "-lb", strings.Repeat("a", 29) + "-lb"},
		{"empty suffix short name", "ns-web-80", "", "ns-web-80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withSuffix(tt.in, tt.suffix); got != tt.want {
				t.Errorf("withSuffix(%q, %q) = %q, want %q", tt.in, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestWithSuffixTruncation(t *testing.T) {
	long1 := strings.Repeat("a", 40) + "-one"
	long2 := strings.Repeat("a", 40) + "-two"

	got1, got2 := withSuffix(long1, "-lb"), withSuffix(long2, "-lb")
	for _, got := range []string{got1, got2} {
		if len(got) > 32 {
			t.Errorf("name %q is %d chars, want <= 32", got, len(got))
		}
		if !strings.HasSuffix(got, "-lb") {
			t.Errorf("name %q lost its suffix", got)
		}
		if strings.Contains(got, "--") {
			t.Errorf("name %q contains a double hyphen", got)
		}
	}
	if got1 == got2 {
		t.Errorf("names sharing a long prefix collided: %q", got1)
	}
	if again := withSuffix(long1, "-lb"); again != got1 {
		t.Errorf("withSuffix is not deterministic: %q vs %q", got1, again)
	}
}

func TestWithSuffixNoTrailingHyphenBeforeHash(t *testing.T) {
	// A cut landing right after a hyphen must not leave "-" + "-<hash>".
	name := strings.Repeat("a", 18) + "-" + strings.Repeat("b", 30)
	for i := range 5 {
		got := withSuffix(name+strings.Repeat("c", i), "-lb")
		if strings.Contains(got, "--") || len(got) > 32 {
			t.Errorf("bad name %q (len %d)", got, len(got))
		}
	}
}

func TestResourceNames(t *testing.T) {
	svc := svcWith(nil)
	if got := loadBalancerName(svc); got != "ns-web-lb" {
		t.Errorf("loadBalancerName = %q, want ns-web-lb", got)
	}
	if got := securityGroupName(svc); got != "ns-web-sg" {
		t.Errorf("securityGroupName = %q, want ns-web-sg", got)
	}

	svc = svcWith(map[string]string{annotationLBName: "custom"})
	if got := loadBalancerName(svc); got != "custom-lb" {
		t.Errorf("custom loadBalancerName = %q, want custom-lb", got)
	}
	if got := securityGroupName(svc); got != "custom-sg" {
		t.Errorf("custom securityGroupName = %q, want custom-sg", got)
	}

	svc = svcWith(map[string]string{annotationLBName: "custom-lb"})
	if got := loadBalancerName(svc); got != "custom-lb" {
		t.Errorf("loadBalancerName with suffix already present = %q, want custom-lb", got)
	}

	if got := tgName(svcWith(nil), 8080); got != "ns-web-8080" {
		t.Errorf("tgName = %q, want ns-web-8080", got)
	}
}
