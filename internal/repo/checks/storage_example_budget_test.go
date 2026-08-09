package repocheck

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
	"go.yaml.in/yaml/v3"
)

func TestCephIBMLibvirtLabMonProfileClearsItsDeclaredBudget(t *testing.T) {
	var provider v1alpha1.InfraProvider
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "examples/ceph-ibm-libvirt-lab/infra/providers/libvirt.yaml")), &provider); err != nil {
		t.Fatal(err)
	}
	var machine v1alpha1.Machine
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "examples/ceph-ibm-libvirt-lab/clusters/storage/ceph-ibm/nodes/ceph-3.yaml")), &machine); err != nil {
		t.Fatal(err)
	}
	var cluster v1alpha1.StorageCluster
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "examples/ceph-ibm-libvirt-lab/clusters/storage/ceph-ibm/cluster.yaml")), &cluster); err != nil {
		t.Fatal(err)
	}

	var node v1alpha1.StorageCephNode
	foundNode := false
	for _, candidate := range cluster.Spec.Ceph.Topology.Nodes {
		if candidate.MachineRef.Name == machine.Metadata.Name {
			node = candidate
			foundNode = true
			break
		}
	}
	if !foundNode {
		t.Fatalf("StorageCluster/%s has no node for Machine/%s", cluster.Metadata.Name, machine.Metadata.Name)
	}
	profileRef := machine.Spec.Substrate.ProfileRef.Name
	var profile v1alpha1.MachineProfile
	foundProfile := false
	for _, candidate := range provider.Spec.Libvirt.MachineProfiles {
		if candidate.Name == profileRef {
			profile = candidate
			foundProfile = true
			break
		}
	}
	if !foundProfile {
		t.Fatalf("Machine/%s profileRef %q has no profile on InfraProvider/%s", machine.Metadata.Name, profileRef, provider.Metadata.Name)
	}

	budget := topology.NodeRootFilesystemGiB(cluster, node)
	if profile.DiskGiB < budget {
		t.Fatalf("ceph-mon lab profile diskGiB = %d, below node %s computed budget %d GiB", profile.DiskGiB, node.Name, budget)
	}
	if profile.DiskGiB <= topology.RootFilesystemFloorGiB {
		t.Fatalf("ceph-mon lab profile diskGiB = %d leaves no room for an installed OS above the %d GiB live free-space floor", profile.DiskGiB, topology.RootFilesystemFloorGiB)
	}

	docs := readRepoFile(t, "docs/getting-started/ceph.md")
	for _, want := range []string{
		fmt.Sprintf("a %d GiB root", profile.DiskGiB),
		fmt.Sprintf("%d GiB service budget", budget),
		fmt.Sprintf("%d GiB free-space floor", topology.RootFilesystemFloorGiB),
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("getting-started Ceph profile description must mirror the guarded example sizing; missing %q", want)
		}
	}
}
