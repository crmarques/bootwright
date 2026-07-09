package desiredstate

import (
	"os"
	"path/filepath"
	"testing"
)

const nfsExportOnlyYAML = `apiVersion: bootwright.io/v1alpha1
kind: StorageNFSExport
metadata:
  name: nfs-demo
spec:
  storageClusterRef: demo
  ceph:
    serviceID: nfs
    placement:
      countPerHost: 1
`

func TestLoadStorageNFSExportOnlyDocumentSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nfs-export.yaml")
	if err := os.WriteFile(path, []byte(nfsExportOnlyYAML), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	state, err := Load([]string{dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.StorageNFSExports) != 1 || state.StorageNFSExports[0].Metadata.Name != "nfs-demo" {
		t.Fatalf("StorageNFSExports = %+v, want one named nfs-demo", state.StorageNFSExports)
	}
}
