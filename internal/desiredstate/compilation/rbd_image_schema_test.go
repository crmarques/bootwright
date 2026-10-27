package compilation_test

import (
	"slices"
	"strings"
	"testing"
)

func rbdImageProbe(spec string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: CephRBDImage\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  poolRef: missing\n" + spec
}

func probeFindingsAt(t *testing.T, spec, field string) []string {
	t.Helper()
	onProbe, _ := strictYAMLCompile(t, rbdImageProbe(spec))
	found := []string{}
	for _, finding := range onProbe {
		if strings.HasSuffix(finding, " "+field) {
			found = append(found, finding)
		}
	}
	return found
}

func TestRBDImageSizeZeroAndOmissionRefuse(t *testing.T) {
	for _, tc := range []struct{ spec, want string }{
		{"  sizeBytes: 0\n", "api.value $.spec.sizeBytes"},
		{"", "api.required $.spec.sizeBytes"},
		{"  sizeBytes: 1.5\n", "api.type $.spec.sizeBytes"},
		{"  sizeBytes: 1\n", ""},
	} {
		got := probeFindingsAt(t, tc.spec, "$.spec.sizeBytes")
		if tc.want == "" && len(got) != 0 || tc.want != "" && !slices.Equal(got, []string{tc.want}) {
			t.Fatalf("sizeBytes %q: findings %v, want %q", tc.spec, got, tc.want)
		}
	}
}

func TestRBDImageSizeBoundIsSigned64(t *testing.T) {
	if got := probeFindingsAt(t, "  sizeBytes: 9223372036854775807\n", "$.spec.sizeBytes"); len(got) != 0 {
		t.Fatalf("the largest signed 64-bit size refused: %v", got)
	}
	if got := probeFindingsAt(t, "  sizeBytes: 9223372036854775808\n", "$.spec.sizeBytes"); !slices.Equal(got, []string{"api.value $.spec.sizeBytes"}) {
		t.Fatalf("a size beyond signed 64 bits: findings %v", got)
	}
}

func TestRBDImageInternalFeaturesRefuse(t *testing.T) {
	for _, feature := range []string{"striping", "data-pool", "operations", "migrating", "non-primary", "dirty-cache"} {
		if got := probeFindingsAt(t, "  sizeBytes: 1\n  features: ["+feature+"]\n", "$.spec.features[0]"); !slices.Equal(got, []string{"api.value $.spec.features[0]"}) {
			t.Fatalf("feature %s: findings %v", feature, got)
		}
	}
	onProbe, _ := strictYAMLCompile(t, rbdImageProbe("  sizeBytes: 1\n  features: [layering, layering]\n"))
	refused := false
	for _, finding := range onProbe {
		refused = refused || strings.Contains(finding, "$.spec.features")
	}
	if !refused {
		t.Fatalf("a duplicate feature was admitted: %v", onProbe)
	}
}
