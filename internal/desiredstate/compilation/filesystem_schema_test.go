package compilation_test

import (
	"context"
	"slices"
	"strings"
	"testing"
)

const (
	retiredMDSPlacement   = "an entry of its StorageCluster's spec.ceph.services with service_type mds and service_id equal to this StorageFilesystem's name, and set spec.mdsServiceRef to mds.<name>"
	retiredMDSServiceSpec = "the native fields (networks, extra_container_args, extra_entrypoint_args) of its StorageCluster's spec.ceph.services entry with service_type mds and service_id equal to this StorageFilesystem's name"
)

func filesystemProbe(spec string) string {
	return "apiVersion: bootwright.io/v1alpha1\nkind: StorageFilesystem\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  mdsServiceRef: mds.probe\n  metadataPoolRef: missing\n  dataPoolRefs: [missing]\n" + spec
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

func TestFilesystemRetiredMDSPlacementNamesReplacement(t *testing.T) {
	for _, row := range []struct{ body, field, replacement string }{
		{"  mds:\n    placement: {hosts: [node-1]}\n", "$.spec.mds.placement", retiredMDSPlacement},
		{"  mds:\n    serviceSpec: {networks: [192.0.2.0/24]}\n", "$.spec.mds.serviceSpec", retiredMDSServiceSpec},
	} {
		input := sources(environmentYAML + "---\n" + filesystemProbe(row.body))
		state, report, err := regressionCompiler().Compile(context.Background(), input)
		sink := requireCompilationFailure(t, state, report, err)
		if len(sink) != 1 || sink[0].Code != "api.field" || sink[0].Field != row.field || sink[0].Remediation != "replace "+row.field+" with "+row.replacement || !strings.Contains(sink[0].Message, "native-service migration") {
			t.Fatalf("retired %s lost its replacement: %+v", row.field, sink)
		}
	}
	input := sources(environmentYAML + "  defaults:\n    StorageFilesystem:\n      mds:\n        placement: {count: 2}\n")
	state, report, err := regressionCompiler().Compile(context.Background(), input)
	sink := requireCompilationFailure(t, state, report, err)
	const field = "$.spec.defaults.StorageFilesystem.mds.placement"
	if len(sink) != 1 || sink[0].Field != field || sink[0].Remediation != "replace "+field+" with "+retiredMDSPlacement {
		t.Fatalf("retired default lost its owner: %+v", sink)
	}
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
