package compilation_test

import (
	"slices"
	"strings"
	"testing"
)

func filesystemProbe(spec string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: CephFilesystem\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  mdsServiceRef: mds.probe\n  metadataPoolRef: missing\n  dataPoolRefs: [missing]\n" + spec
}

func filesystemFindingsAt(t *testing.T, spec, field string) []string {
	t.Helper()
	onProbe, _ := strictYAMLCompile(t, filesystemProbe(spec))
	found := []string{}
	for _, finding := range onProbe {
		if strings.HasSuffix(finding, " "+field) {
			found = append(found, finding)
		}
	}
	return found
}

func TestActiveCountZeroRefuses(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"0", "api.value $.spec.mds.activeCount"},
		{"1", ""},
		{"256", ""},
		{"257", "api.value $.spec.mds.activeCount"},
	} {
		got := filesystemFindingsAt(t, "  mds:\n    activeCount: "+tc.value+"\n", "$.spec.mds.activeCount")
		if tc.want == "" && len(got) != 0 || tc.want != "" && !slices.Equal(got, []string{tc.want}) {
			t.Fatalf("activeCount %s: findings %v, want %q", tc.value, got, tc.want)
		}
	}
}

func TestGroupSizeZeroRefuses(t *testing.T) {
	const field = "$.spec.subvolumeGroups[0].sizeBytes"
	if got := filesystemFindingsAt(t, "  subvolumeGroups: [{name: csi, sizeBytes: 0}]\n", field); !slices.Equal(got, []string{"api.value " + field}) {
		t.Fatalf("zero group size: findings %v", got)
	}
	if got := filesystemFindingsAt(t, "  subvolumeGroups: [{name: csi, sizeBytes: 1}]\n", field); len(got) != 0 {
		t.Fatalf("a one-byte group quota refused: %v", got)
	}
}
