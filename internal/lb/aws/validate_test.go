package aws

import (
	"strings"
	"testing"
)

func TestValidateAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantErrs    int
		wantSubstr  string
	}{
		{"no annotations", nil, 0, ""},
		{"all valid", map[string]string{
			annotationScheme: "internal", annotationLBType: "alb",
			annotationLBName: "my-lb", annotationTargetType: "ip",
		}, 0, ""},
		{"bad scheme", map[string]string{annotationScheme: "public"}, 1, annotationScheme},
		{"bad type", map[string]string{annotationLBType: "clb"}, 1, annotationLBType},
		{"bad target type", map[string]string{annotationTargetType: "pod"}, 1, annotationTargetType},
		{"name leading hyphen", map[string]string{annotationLBName: "-lb"}, 1, annotationLBName},
		{"name trailing hyphen", map[string]string{annotationLBName: "lb-"}, 1, annotationLBName},
		{"name underscore", map[string]string{annotationLBName: "my_lb"}, 1, annotationLBName},
		{"name too long", map[string]string{annotationLBName: strings.Repeat("a", 33)}, 1, annotationLBName},
		{"name max length", map[string]string{annotationLBName: strings.Repeat("a", 32)}, 0, ""},
		{"single char name", map[string]string{annotationLBName: "a"}, 0, ""},
		{"empty name", map[string]string{annotationLBName: ""}, 1, annotationLBName},
		{"multiple problems", map[string]string{annotationScheme: "x", annotationLBType: "y"}, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateAnnotations(svcWith(tt.annotations))
			if len(errs) != tt.wantErrs {
				t.Fatalf("got %d errors %v, want %d", len(errs), errs, tt.wantErrs)
			}
			if tt.wantSubstr != "" && !strings.Contains(errs[0], tt.wantSubstr) {
				t.Errorf("error %q does not mention %q", errs[0], tt.wantSubstr)
			}
		})
	}
}

func TestValidateAnnotationImmutability(t *testing.T) {
	tests := []struct {
		name     string
		old, new map[string]string
		wantErrs int
	}{
		{"unchanged", map[string]string{annotationScheme: "internal"}, map[string]string{annotationScheme: "internal"}, 0},
		{"scheme changed", map[string]string{annotationScheme: "internal"}, map[string]string{annotationScheme: "internet-facing"}, 1},
		{"name added", nil, map[string]string{annotationLBName: "x"}, 1},
		{"target type removed", map[string]string{annotationTargetType: "ip"}, nil, 1},
		{"all three changed", nil, map[string]string{annotationScheme: "internal", annotationLBName: "x", annotationTargetType: "ip"}, 3},
		{"mutable annotation changes freely",
			map[string]string{annotationSSLCert: "a"}, map[string]string{annotationSSLCert: "b", annotationSubnets: "s"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateAnnotationImmutability(svcWith(tt.old), svcWith(tt.new))
			if len(errs) != tt.wantErrs {
				t.Errorf("got %d errors %v, want %d", len(errs), errs, tt.wantErrs)
			}
		})
	}
}
