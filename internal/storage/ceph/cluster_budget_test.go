package ceph

import (
	"fmt"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func TestClusterBudgetPlansThreeThroughSixHostsAndServiceCeiling(t *testing.T) {
	for hosts := 3; hosts <= 6; hosts++ {
		catalog := planCatalog(hosts, hosts, nil)
		request, err := planned(t, catalog, api.StorageCluster, "example")
		if err != nil {
			t.Fatal(err)
		}
		for services := 3; services <= 11; services++ {
			request.Services = request.Services[:3:3]
			for len(request.Services) < services {
				request.Services = append(request.Services, request.Services[2])
			}
			want := time.Duration((15+hosts+services)*660+1860+1800+60) * time.Second
			got, err := cephDeadline(request, "apply")
			if want <= 6*time.Hour {
				if err != nil || got != want {
					t.Fatalf("%d hosts %d services: %s %v, want %s", hosts, services, got, err, want)
				}
			} else {
				refusedWith(t, err, "lifecycle.unsupported", fmt.Sprintf("StorageCluster example needs a cluster run deadline of %s, past the 6h0m0s every adapter run is held to", want), "declare fewer hosts in spec.ceph.topology.nodes or fewer mon, mgr, osd and mds services in spec.ceph.services of StorageCluster example")
			}
		}
	}
}

func TestSeventhHostRefusesClusterAndDependentResources(t *testing.T) {
	catalog := pinnedCatalog(t, func(spec api.Value) api.Value {
		nodes := spec.Get("ceph", "topology", "nodes").Items()
		for len(nodes) < 7 {
			nodes = append(nodes, nodes[0])
		}
		return spec.WithPath(api.ListValue(nodes...), "ceph", "topology", "nodes")
	})
	catalog = api.NewCatalog(append(catalog.Objects(), childOf(api.StoragePlacementPolicy, "policy", "replicated", requestMap("size", 2))))
	state := compilation.NewState(catalog, catalog, nil)
	for kind, name := range map[api.Kind]string{api.StorageCluster: "example", api.StoragePlacementPolicy: "policy", api.StoragePool: "volumes", api.StorageClient: "consumer"} {
		refusals := New(kind, nil).Unsupported(state)
		reason, remedy := clusterHostBudget("example", 7)
		if len(refusals) != 1 || refusals[0].Name != name || refusals[0].Reason != reason || refusals[0].Remediation != remedy {
			t.Fatalf("%s: %+v", kind, refusals)
		}
	}
}

func TestClusterBudgetExamples(t *testing.T) {
	for _, row := range []struct{ hosts, services, seconds int }{{3, 3, 17580}, {6, 4, 20220}, {6, 6, 21540}, {3, 9, 21540}} {
		request := packageRequest(t)
		for len(request.Hosts) < row.hosts {
			request.Hosts = append(request.Hosts, request.Hosts[0])
		}
		for len(request.Services) < row.services {
			request.Services = append(request.Services, request.Services[2])
		}
		deadline, err := cephDeadline(request, "apply")
		if err != nil || deadline != time.Duration(row.seconds)*time.Second {
			t.Fatalf("%+v: %s %v", row, deadline, err)
		}
	}
}
