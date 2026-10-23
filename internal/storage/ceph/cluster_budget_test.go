package ceph

import (
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func TestSixHostClusterBudgetAdmitsHostSpecificOSDServices(t *testing.T) {
	for _, services := range []int{8, 9, 14} {
		request := packageRequest(t)
		for len(request.Hosts) < 6 {
			request.Hosts = append(request.Hosts, request.Hosts[0])
		}
		for len(request.Services) < services {
			request.Services = append(request.Services, request.Services[2])
		}
		deadline, err := cephDeadline(request, "apply")
		if err != nil || deadline > 6*time.Hour {
			t.Fatalf("six hosts with %d core services: deadline %s: %v", services, deadline, err)
		}
	}
}

func TestClusterBudgetPlansThreeThroughSixHostsWithOneServiceSubmission(t *testing.T) {
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
			want := time.Duration((16+hosts)*660+1860+1800+60) * time.Second
			got, err := cephDeadline(request, "apply")
			if err != nil || got != want || got > 6*time.Hour {
				t.Fatalf("%d hosts %d services: %s %v, want %s", hosts, services, got, err, want)
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
	for _, row := range []struct{ hosts, services, seconds int }{{3, 3, 16260}, {6, 4, 18240}, {6, 14, 18240}, {3, 9, 16260}} {
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
