//go:build linux && amd64

package main

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const servicesExample = "libvirt-6-nodes-ceph-services"

func TestExampleServicesPlansAllProtocols(t *testing.T) {
	sources := exampleDirectory(t, servicesExample)
	state, report := compileAcceptance(t, sources)
	if report.Counts.FilesSeen != 45 || report.Counts.ObjectsDecoded != 45 {
		t.Fatalf("counts: %+v", report.Counts)
	}
	budget := exampleBudget(t, profileDeclarations(t, sources))
	if budget != (profileBudget{guests: 7, cpu: 26, memoryMiB: 83 * 1024, root: 400, data: 360}) {
		t.Fatalf("budget: %+v", budget)
	}
	if len(state.Effective().OfKind(api.Machine)) != 8 {
		t.Fatal("storage closure must retain controller, six hosts and the standalone client")
	}
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("capabilities refuse %+v", refused)
	}
	definitions := productionDefinitions(t, state, servicesExample)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ceph-filesystem-files", "ceph-subvolume-cephfs-canary", "ceph-nfs-export-docs", "ceph-nfs-export-objects", "ceph-client-cephfs-canary", "ceph-object-user-services-canary", "ceph-object-bucket-services-canary-bucket", "ceph-smb-files-smb", "ceph-smb-files-users", "ceph-smb-smb-documents", "ceph-smb-smb-archive", "ceph-gateway-objects", "ceph-service-ceph-lab-nfs-shares"} {
		if !slices.ContainsFunc(plan.Blocks, func(b reconciliation.Block) bool { return b.ID == id }) {
			t.Errorf("missing %s", id)
		}
	}
	for blockID, dependencies := range map[string][]string{
		"ceph-nfs-export-objects": {"ceph-object-user-services-canary"},
		"ceph-nfs-export-docs":    {"ceph-subvolume-docs"},
		"ceph-smb-smb-documents":  {"ceph-subvolume-smb-documents", "ceph-smb-files-smb"},
		"ceph-smb-smb-archive":    {"ceph-subvolume-smb-archive", "ceph-smb-files-smb"},
	} {
		index := slices.IndexFunc(plan.Blocks, func(b reconciliation.Block) bool { return b.ID == blockID })
		if index < 0 {
			t.Errorf("missing %s", blockID)
			continue
		}
		for _, dependency := range dependencies {
			if !slices.Contains(plan.Blocks[index].Dependencies, dependency) {
				t.Errorf("%s missing dependency %s", blockID, dependency)
			}
		}
	}
}
