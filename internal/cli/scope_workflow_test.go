package cli

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/ownership"
)

func TestPrepareScopedWorkflowDestroyCountsOwnershipRecords(t *testing.T) {
	records := []ownership.ResourceRecord{{
		Kind: "libvirt-domain",
		Name: "cluster-a-machine-0",
		Host: "provider-0",
	}}

	withRecords, err := prepareScopedWorkflow(v1alpha1.State{}, converge.InfraScope, false, false, records)
	if err != nil {
		t.Fatalf("prepareScopedWorkflow with records: %v", err)
	}
	if withRecords.NoRemoteWork {
		t.Fatal("noRemoteWork = true with host-bearing ownership records: the destroy prompt would be skipped while workflow.Run tears down recorded hosts")
	}

	withoutRecords, err := prepareScopedWorkflow(v1alpha1.State{}, converge.InfraScope, false, false, nil)
	if err != nil {
		t.Fatalf("prepareScopedWorkflow without records: %v", err)
	}
	if !withoutRecords.NoRemoteWork {
		t.Fatal("noRemoteWork = false for empty desired state and no records: expected the no-remote-work short-circuit")
	}
}
