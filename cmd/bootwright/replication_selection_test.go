package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

// TestSelectingOneReplicationEndPassesAdmission narrows the multisite
// example's storage list to either end of its replication and compiles it
// through production admission: the relationship and both clusters stay
// whole, so no peer reference is left unresolved (specs/api/environment.md,
// Resource and cluster selection).
func TestSelectingOneReplicationEndPassesAdmission(t *testing.T) {
	const example = "libvirt-2x4-nodes-ceph-multisite"
	for _, end := range []string{"primary", "secondary"} {
		t.Run(end, func(t *testing.T) {
			sources := exampleDirectory(t, example)
			files := make([]desiredstate.SourceFile, 0, len(sources.Files))
			for _, file := range sources.Files {
				if filepath.Base(file.Path()) == "environment.yaml" {
					body := mustReplaceOnce(t, file.Path(), string(file.Bytes()), "  storageClusters:\n    - primary\n    - secondary\n", "  storageClusters:\n    - "+end+"\n")
					file = desiredstate.NewSourceFile(file.Path(), []byte(body))
				}
				files = append(files, file)
			}
			sources.Files = files
			state, report := compileAcceptance(t, sources)
			if len(report.ExcludedStorageClusters) != 0 {
				t.Errorf("selecting %s excluded %v", end, report.ExcludedStorageClusters)
			}
			kept := []string{}
			for _, object := range state.Effective().Objects() {
				kept = append(kept, object.Identity())
			}
			for _, identity := range []string{"CephCluster/primary", "CephCluster/secondary", "CephReplication/volumes-to-secondary", "CephPool/primary-volumes", "CephPool/secondary-volumes"} {
				if !slices.Contains(kept, identity) {
					t.Errorf("selecting %s dropped %s", end, identity)
				}
			}
		})
	}
}
