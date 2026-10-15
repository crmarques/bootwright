package compilation_test

import (
	"slices"
	"strings"
	"testing"
)

func subvolumeFindingsAt(t *testing.T, spec, field string) []string {
	t.Helper()
	onProbe, _ := strictYAMLCompile(t, "apiVersion: bootwright.io/v1alpha1\nkind: StorageSubvolume\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  filesystemRef: missing\n"+spec)
	found := []string{}
	for _, finding := range onProbe {
		if strings.HasSuffix(finding, " "+field) {
			found = append(found, finding)
		}
	}
	return found
}

func TestSubvolumeSizeZeroRefuses(t *testing.T) {
	for _, tc := range []struct{ spec, want string }{
		{"  sizeBytes: 0\n", "api.value $.spec.sizeBytes"},
		{"  sizeBytes: 1\n", ""},
		{"  sizeBytes: 9223372036854775807\n", ""},
		{"  sizeBytes: 9223372036854775808\n", "api.value $.spec.sizeBytes"},
		{"", ""},
	} {
		got := subvolumeFindingsAt(t, tc.spec, "$.spec.sizeBytes")
		if tc.want == "" && len(got) != 0 || tc.want != "" && !slices.Equal(got, []string{tc.want}) {
			t.Fatalf("sizeBytes %q: findings %v, want %q", tc.spec, got, tc.want)
		}
	}
}

func TestSubvolumeEmptyGroupRefuses(t *testing.T) {
	if got := subvolumeFindingsAt(t, "  group: \"\"\n", "$.spec.group"); !slices.Equal(got, []string{"api.value $.spec.group"}) {
		t.Fatalf("empty group: findings %v", got)
	}
	if got := subvolumeFindingsAt(t, "  group: csi\n", "$.spec.group"); len(got) != 0 {
		t.Fatalf("group csi: findings %v", got)
	}
}

func TestSubvolumeUnsupportedFieldsRefuseAsUnknown(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"namespaceIsolated", "true"}, {"namespace_isolated", "true"}, {"earmark", "smb"}, {"normalization", "nfc"}, {"casesensitive", "false"}} {
		if got := subvolumeFindingsAt(t, "  "+tc.key+": "+tc.value+"\n", "$.spec."+tc.key); !slices.Equal(got, []string{"api.field $.spec." + tc.key}) {
			t.Fatalf("%s: findings %v", tc.key, got)
		}
	}
}
