//go:build linux && amd64

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

type containerPreflightGraph struct{ catalog api.Catalog }

func (g containerPreflightGraph) Effective(context.Context, string) (api.Catalog, error) {
	return g.catalog, nil
}

type containerPreflightOwnership map[string]machine.OwnershipState

func (o containerPreflightOwnership) Ownership(context.Context, string) (map[string]machine.OwnershipState, error) {
	return o, nil
}

type containerCapacityFacts capacity.Facts

func (f containerCapacityFacts) Observe(context.Context, capacity.Target) (capacity.Facts, error) {
	return capacity.Facts(f), nil
}

func TestContainerPreflightUsesCompletedDNSAndApplyCapacityRows(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno"))
	catalog := state.Effective()
	allocations, err := libvirtAllocations(catalog, "lab-sno")
	if err != nil || len(allocations) != 1 {
		t.Fatalf("allocations %v %v", allocations, err)
	}
	allocation := allocations[0]
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: libvirt.MachineKind, Object: allocation.Machine}}
	answers := map[string]string{}
	for _, cluster := range catalog.OfKind(api.ContainerCluster) {
		for _, endpoint := range containercluster.Endpoints(catalog, cluster) {
			answers[endpoint.Name] = endpoint.Address
			if endpoint.Subtree {
				answers["console-openshift-console."+endpoint.Name] = endpoint.Address
			}
		}
	}
	for _, owned := range []lifecycle.ObjectOwnership{{Verb: "apply", State: "done"}, {Verb: "destroy", State: "done"}} {
		t.Run(owned.Verb, func(t *testing.T) {
			facts := containerCapacityFacts{Available: capacity.Quantities{CPU: 1024, Memory: 1 << 50, Disk: 1 << 50}, CPUKnown: true, MemoryKnown: true, DiskKnown: true, DiskIdentity: "fixture-pool", Reusable: map[string]capacity.Quantities{allocation.Machine: allocation.Quantities}}
			budget := capacity.Budget{Observer: facts}
			var gateRows []capacity.Check
			var gateAllocations []capacity.Allocation
			checker := capacityCheckFunc(func(ctx context.Context, a []capacity.Allocation, dry bool) ([]capacity.Check, error) {
				gateAllocations = a
				gateRows, err = budget.Check(ctx, a, dry)
				return gateRows, err
			})
			identity := "Machine/" + allocation.Machine
			if err := admitLibvirtCapacity(context.Background(), "lab-sno", catalog, map[string]lifecycle.ObjectOwnership{identity: owned}, []reconciliation.Block{block}, checker); err != nil {
				t.Fatal(err)
			}
			if gateAllocations[0].Allocated != (owned.Verb == "apply") {
				t.Fatalf("destroyed Machine credited: %+v", gateAllocations)
			}
			service := wireContainerPreflight(nil, lifecycle.Service{}, serviceDependencies{})
			service.Graph = containerPreflightGraph{catalog}
			service.Ownership = containerPreflightOwnership{identity: {Verb: owned.Verb, State: owned.State}}
			service.Blocks = func(context.Context, string) (map[string]string, error) {
				blocks := map[string]string{}
				for _, server := range catalog.OfKind(api.DNSServer) {
					blocks[dnsserver.Definition().BlockID(server.Name())] = "done"
				}
				if len(blocks) == 0 {
					t.Fatal("fixture has no managed DNS")
				}
				return blocks, nil
			}
			service.Capacity = budget
			resolved := 0
			service.Resolver = containerpreflight.ResolverFunc(func(_ context.Context, argv []string) ([]byte, error) {
				if len(argv) != 3 || argv[0] != "getent" || argv[1] != "ahosts" || answers[argv[2]] == "" {
					t.Fatalf("unexpected resolution %v", argv)
				}
				resolved++
				return []byte(answers[argv[2]] + " STREAM\n"), nil
			})
			report, err := service.Check(context.Background(), containerpreflight.PreflightRequest{ContextName: "lab-sno"})
			if err != nil || resolved != len(answers) {
				t.Fatalf("preflight %v, resolved %d/%d", err, resolved, len(answers))
			}
			var rows []capacity.Check
			for _, row := range report.Checks {
				if strings.HasPrefix(row.ID, "capacity.") {
					rows = append(rows, row)
				}
			}
			// Report ordering differs from the resource checker's ordering.
			byID := func(checks []capacity.Check) map[string]capacity.Check {
				m := map[string]capacity.Check{}
				for _, check := range checks {
					m[check.Target+"/"+check.ID] = check
				}
				return m
			}
			if !reflect.DeepEqual(byID(rows), byID(gateRows)) {
				t.Fatalf("preflight rows %+v; gate %+v", rows, gateRows)
			}
		})
	}
}
