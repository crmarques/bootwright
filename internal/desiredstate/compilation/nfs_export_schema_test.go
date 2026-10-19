package compilation_test

import (
	"context"
	"testing"
)

func TestNFSExportRetiredFieldsOnDecode(t *testing.T) {
	const field = "$.spec.exports"
	probe := "apiVersion: bootwright.io/v1alpha1\nkind: StorageNFSExport\nmetadata:\n  name: probe\nspec:\n  clusterRef: missing\n  serviceID: probe\n  filesystemRef: probe\n  native:\n    pseudo: /probe\n    protocols: [4]\n    fsal:\n      name: CEPH\n  exports:\n    - pseudo: /probe\n"
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+"---\n"+probe))
	sink := requireCompilationFailure(t, state, report, err)
	if len(sink) != 1 || sink[0].Code != "api.field" || sink[0].Field != field || sink[0].Remediation != "replace "+field+" with one StorageNFSExport per export entry, each binding spec.filesystemRef or spec.subvolumeRef and holding that entry's native export under spec.native" {
		t.Fatalf("retired NFS exports lost their replacement: %+v", sink)
	}
}
