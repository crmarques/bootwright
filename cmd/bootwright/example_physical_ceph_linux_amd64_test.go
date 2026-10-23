//go:build linux && amd64

package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func TestPhysicalCephExamplePlansOriginalHostAndWWNBindings(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "physical-3-nodes-ceph"))
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("refused %+v", refused)
	}
	definitions := productionDefinitions(t, state, "physical-3-nodes-ceph")
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for _, block := range plan.Blocks {
		if block.Implementation != ceph.HostImplementation {
			continue
		}
		for _, name := range []string{"metal-01", "metal-02", "metal-03"} {
			if !slices.Contains(block.Dependencies, "machine-"+name) || !slices.Contains(block.Dependencies, "os-install-"+name) {
				t.Fatalf("host scope lacks original Machine dependency: %+v", block.Dependencies)
			}
		}
	}
	if got := len(hostBlockIDs(definitions, "ceph-lab")); got != 3 {
		t.Fatalf("got %d storage hosts", got)
	}
	for _, block := range definitions {
		if block.ID != ceph.ClusterBlockID("ceph-lab") {
			continue
		}
		request, err := ceph.DecodeRequest(block.Request)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		for _, host := range request.Hosts {
			if !host.Physical || host.Hardware != nil || host.HostKeyRef == "" || len(host.Devices) != 1 || host.Devices[0].WWN == "" || host.Devices[0].Serial != "" || host.Devices[0].SizeBytes != 0 {
				t.Fatalf("lost physical proof boundary: %+v", host)
			}
		}
		return
	}
	t.Fatal("missing cluster block")
}

func TestPhysicalCephCompositionValidatesOriginalMachinePresence(t *testing.T) {
	var block reconciliation.Block
	for _, definition := range baremetalPlanBlocks(t, reconciliation.Apply) {
		if definition.Implementation == baremetal.Implementation {
			block = reconciliation.Block{BlockDefinition: definition, RequestDigest: strings.Repeat("a", 64)}
			break
		}
	}
	if block.ID == "" {
		t.Fatal("missing physical claim")
	}
	original, err := baremetal.DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	evidence := baremetal.Evidence{Addresses: original.Addresses(), UUID: "11111111-1111-4111-8111-111111111111", Power: "On", Postcondition: true, Request: block.RequestDigest}
	for _, damage := range []string{"", "digest", "mac", "absent", "object", "implementation"} {
		t.Run(damage, func(t *testing.T) {
			proof, value := block, evidence
			switch damage {
			case "digest":
				value.Request = strings.Repeat("b", 64)
			case "mac":
				value.Addresses = []string{"02:00:00:00:00:01"}
			case "absent":
				value.Absent = true
			case "object":
				proof.Object = "another"
			case "implementation":
				proof.Implementation = "foreign"
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			got, recognized, err := (cephDependencyProofs{}).PhysicalMachine(proof, data)
			if damage != "" {
				if err == nil && recognized {
					t.Fatal("foreign original proof accepted")
				}
				return
			}
			if err != nil || !recognized || got.UUID != value.UUID || !slices.Equal(got.MACs, value.Addresses) {
				t.Fatalf("lost original hardware proof: %+v %v", got, diagnostics.Of(err))
			}
		})
	}
}
