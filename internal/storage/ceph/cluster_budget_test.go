package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestClusterHostBoundIsTheLargestDeadlineUnderTheRunCeiling(t *testing.T) {
	if maxClusterHosts != 11 || clusterApplyDeadline(11) != 21540*time.Second || clusterApplyDeadline(12) != 22200*time.Second || clusterApplyDeadline(12) <= lifecycle.MaxDeadline {
		t.Fatalf("bound %d: 11 hosts take %s, 12 take %s", maxClusterHosts, clusterApplyDeadline(11), clusterApplyDeadline(12))
	}
}

func TestClusterBudgetAdmitsHostSpecificOSDServicesUpToTheBound(t *testing.T) {
	for _, hosts := range []int{6, 7, 11} {
		for _, services := range []int{2 + hosts, 2 + 2*hosts, 2 + 3*hosts} {
			request := packageRequest(t)
			for len(request.Hosts) < hosts {
				request.Hosts = append(request.Hosts, request.Hosts[0])
			}
			for len(request.Services) < services {
				request.Services = append(request.Services, request.Services[2])
			}
			deadline, err := cephDeadline(request, "apply")
			if err != nil || deadline != clusterApplyDeadline(hosts) || deadline > lifecycle.MaxDeadline {
				t.Fatalf("%d hosts with %d core services: deadline %s: %v", hosts, services, deadline, err)
			}
		}
	}
}

func TestClusterBudgetPlansThreeHostsThroughTheBoundWithOneServiceSubmission(t *testing.T) {
	for hosts := 3; hosts <= maxClusterHosts; hosts++ {
		catalog := planCatalog(hosts, hosts, nil)
		request, err := planned(t, catalog, api.CephCluster, "example")
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		for services := 3; services <= 3*hosts+2; services++ {
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

func TestHostPastTheBoundRefusesClusterAndDependentResources(t *testing.T) {
	const reason = "CephCluster example declares 12 Ceph hosts, whose cluster run deadline of 6h10m0s is past the 6h0m0s every adapter run is held to"
	const remedy = "declare at most 11 entries in spec.topology.nodes of CephCluster example; B79 tracks larger clusters"
	for hosts, refused := range map[int]bool{11: false, 12: true} {
		catalog := pinnedCatalogOf(t, hosts, nil)
		catalog = api.NewCatalog(append(catalog.Objects(), childOf(api.CephPlacementPolicy, "policy", "replicated", requestMap("size", 2))))
		state := compilation.NewState(catalog, catalog, nil)
		for kind, name := range map[api.Kind]string{api.CephCluster: "example", api.CephPlacementPolicy: "policy", api.CephPool: "volumes", api.CephClient: "consumer"} {
			refusals := New(kind, nil).Unsupported(state)
			if !refused {
				if len(refusals) != 0 {
					t.Fatalf("%d hosts: %s refused %+v", hosts, kind, refusals)
				}
				if _, err := planned(t, catalog, kind, name); err != nil {
					t.Fatalf("%d hosts: %s did not plan: %v", hosts, kind, diagnostics.Of(err))
				}
				continue
			}
			if len(refusals) != 1 || refusals[0].Name != name || refusals[0].Reason != reason || refusals[0].Remediation != remedy {
				t.Fatalf("%s: %+v", kind, refusals)
			}
			_, err := planned(t, catalog, kind, name)
			refusedWith(t, err, "lifecycle.unsupported", reason, remedy)
		}
	}
	request := packageRequest(t)
	request.Hosts = slices.Repeat(request.Hosts[:1], 12)
	for _, action := range []string{"cluster", "pool", "client", "policy", "filesystem"} {
		request.Action = action
		for _, operation := range []string{"apply", "observe", "destroy"} {
			_, err := cephDeadline(request, operation)
			refusedWith(t, err, "lifecycle.unsupported", reason, remedy)
		}
	}
}

func TestClusterBudgetExamples(t *testing.T) {
	for _, row := range []struct{ hosts, services, seconds int }{{3, 3, 16260}, {6, 4, 18240}, {6, 14, 18240}, {3, 9, 16260}, {7, 3, 18900}, {7, 23, 18900}, {11, 3, 21540}, {11, 35, 21540}} {
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

func TestFilesystemDeadlineAtTheBoundRefusesByItsOwnName(t *testing.T) {
	request := packageRequest(t)
	request.Hosts = slices.Repeat(request.Hosts[:1], maxClusterHosts)
	request.Action = "filesystem"
	for _, row := range []struct{ pools, groups, seconds int }{{1, 0, 18120}, {6, 0, 21420}, {1, 1, 19680}, {2, 2, 21000}} {
		request.Filesystem = &Filesystem{Name: "files", DataPools: slices.Repeat([]string{"fs-data"}, row.pools), Groups: slices.Repeat([]FilesystemGroup{{Name: "csi"}}, row.groups)}
		if deadline, err := cephDeadline(request, "apply"); err != nil || deadline != time.Duration(row.seconds)*time.Second {
			t.Fatalf("%+v: %s %v", row, deadline, err)
		}
	}
	request.Filesystem = &Filesystem{Name: "files", DataPools: slices.Repeat([]string{"fs-data"}, 3), Groups: slices.Repeat([]FilesystemGroup{{Name: "csi"}}, 2)}
	_, err := cephDeadline(request, "apply")
	refusedWith(t, err, "lifecycle.unsupported",
		"CephFilesystem files needs a run deadline of 6h1m0s, past the 6h0m0s every adapter run is held to: two native calls for each of the 11 Ceph hosts of CephCluster example and one for each of its 3 data pools and 2 subvolume groups",
		"declare fewer spec.dataPoolRefs or spec.subvolumeGroups on CephFilesystem files, or fewer entries in spec.topology.nodes of CephCluster example")
}

func TestPlanGoldenAtSevenHostsAndTheBound(t *testing.T) {
	type block struct {
		Apply          int    `json:"apply,omitempty"`
		Destroy        int    `json:"destroy,omitempty"`
		ID             string `json:"id"`
		Implementation string `json:"implementation"`
		Observe        int    `json:"observe,omitempty"`
	}
	type row struct {
		Blocks []block `json:"blocks"`
		Hosts  int     `json:"hosts"`
	}
	rows := []row{}
	for _, hosts := range []int{7, maxClusterHosts} {
		catalog := pinnedCatalogOf(t, hosts, nil)
		catalog = api.NewCatalog(append(catalog.Objects(), childOf(api.CephPlacementPolicy, "policy", "replicated", requestMap("size", 2))))
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
		runner := &recordingRunner{}
		var definitions []reconciliation.BlockDefinition
		for _, capability := range []lifecycle.Capability{NewImage(runner), NewHost(runner), New(api.CephCluster, runner), New(api.CephPlacementPolicy, runner), New(api.CephPool, runner), New(api.CephClient, runner)} {
			plan, err := capability.Plan(context.Background(), input)
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			definitions = append(definitions, plan.Definitions...)
		}
		synthetic := func(kind, name string) reconciliation.BlockDefinition {
			return reconciliation.BlockDefinition{ID: strings.ToLower(kind) + "-" + name, Description: "synthetic " + name, Stage: reconciliation.StageMachines, Kind: kind, Object: name, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)}
		}
		for node := 1; node <= hosts; node++ {
			definitions = append(definitions, synthetic("Machine", fmt.Sprintf("node-%d", node)))
		}
		definitions = append(definitions, synthetic("MachineInstallProfile", "rhel"))
		plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		if runner.calls != 0 {
			t.Fatal("planning reached the runner")
		}
		out := row{Hosts: hosts, Blocks: []block{}}
		hostBlocks := 0
		for _, planned := range plan.Blocks {
			if !strings.HasPrefix(planned.ID, "ceph-") {
				continue
			}
			entry := block{ID: planned.ID, Implementation: planned.Implementation}
			switch planned.Implementation {
			case Implementation:
				request, err := DecodeRequest(planned.Request)
				if err != nil {
					t.Fatal(err)
				}
				for operation, seconds := range map[string]*int{"apply": &entry.Apply, "observe": &entry.Observe, "destroy": &entry.Destroy} {
					deadline, err := cephDeadline(request, operation)
					if err != nil || deadline > lifecycle.MaxDeadline {
						t.Fatalf("%d hosts: %s %s %s: %v", hosts, planned.ID, operation, deadline, diagnostics.Of(err))
					}
					*seconds = int(deadline / time.Second)
				}
			case HostImplementation:
				hostBlocks++
				entry.Apply, entry.Observe, entry.Destroy = int(hostDeadline("apply")/time.Second), int(hostDeadline("observe")/time.Second), int(hostDeadline("destroy")/time.Second)
			}
			out.Blocks = append(out.Blocks, entry)
		}
		if hostBlocks != hosts {
			t.Fatalf("%d hosts planned %d host blocks", hosts, hostBlocks)
		}
		slices.SortFunc(out.Blocks, func(a, b block) int { return strings.Compare(a.ID, b.ID) })
		rows = append(rows, out)
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "plan-budget", data, false)
}

func physicalCatalogOf(hosts, disks int) (api.Catalog, api.Object) {
	objects := poolRequestCatalog(hosts, hosts).Objects()
	var cluster api.Object
	for i, object := range objects {
		if object.Kind() == api.InfraProvider {
			objects[i] = object.WithSpec(requestMap("baremetal", requestMap()))
		}
		if object.Kind() != api.CephCluster {
			continue
		}
		services := object.Spec().Get("services").Items()[:2]
		for node := 1; node <= hosts; node++ {
			for disk := 0; disk < disks; disk++ {
				path := fmt.Sprintf("/dev/disk/by-id/wwn-0x5%015x", node*64+disk)
				services = append(services, requestMap("service_type", "osd", "service_id", fmt.Sprintf("data-%d-%d", node, disk), "placement", requestMap("hosts", api.StringList(fmt.Sprintf("node-%d", node))), "spec", requestMap("data_devices", requestMap("paths", api.StringList(path)))))
			}
		}
		cluster = object.WithSpec(object.Spec().WithPath(api.ListValue(services...), "services"))
		objects[i] = cluster
	}
	return api.NewCatalog(objects), cluster
}

func TestRequestSizesAtTheBound(t *testing.T) {
	catalog, cluster := physicalCatalogOf(maxClusterHosts, 3)
	r, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(r.Hosts) != maxClusterHosts || len(r.Services) != 2+3*maxClusterHosts {
		t.Fatalf("planned %d hosts and %d services", len(r.Hosts), len(r.Services))
	}
	for i := range r.Hosts {
		r.Hosts[i].Hardware = &PhysicalIdentity{UUID: "11111111-1111-4111-8111-111111111111", MACs: slices.Repeat([]string{"02:00:00:00:00:01"}, 32)}
		r.Hosts[i].KnownHosts = r.Hosts[i].Name + " ecdsa-sha2-nistp256 " + strings.Repeat("A", 180) + "\n"
	}
	data, err := r.Canonical()
	if err != nil || len(data) > reconciliation.MaxRequestBytes/4 {
		t.Fatalf("the largest physical cluster request takes %d bytes: %v", len(data), err)
	}
	t.Logf("the %d-host physical cluster request with %d services takes %d of %d bytes", len(r.Hosts), len(r.Services), len(data), reconciliation.MaxRequestBytes)
	retirement := physicalRetirementRequest(catalog, r)
	frozen, err := retirement.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodePhysicalRetirementRequest(frozen); err != nil || len(decoded.Hosts) != maxClusterHosts {
		t.Fatalf("the largest physical retirement policy did not decode: %v", err)
	}
	retirement.Hosts = append(retirement.Hosts, PhysicalRetirementHost{Machine: "node-99", Name: "node-99", Devices: []Device{}, EraseWWNs: []string{}})
	frozen, err = retirement.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePhysicalRetirementRequest(frozen); err == nil {
		t.Fatal("a physical retirement policy past the host bound decoded")
	}
}

func TestReadyEvidenceAtTheBoundFitsItsDecoder(t *testing.T) {
	r := runtimeRequest()
	r.Hosts = nil
	names, paths := []string{}, []string{}
	for disk := 0; disk < 7; disk++ {
		paths = append(paths, fmt.Sprintf("/dev/vd%c", 'b'+disk))
	}
	for node := 1; node <= maxClusterHosts; node++ {
		name := fmt.Sprintf("%s-%02d", strings.Repeat("h", 125), node)
		names = append(names, name)
		host := Host{Name: name, Machine: name, Address: fmt.Sprintf("192.0.2.%d", node), User: "operator", Port: 22, SSHKeyRef: "fleet-key"}
		for disk, path := range paths {
			host.Devices = append(host.Devices, Device{Path: path, Serial: fmt.Sprintf("%s-%d", name, disk), SizeBytes: 1 << 30})
		}
		r.Hosts = append(r.Hosts, host)
	}
	placement, _ := json.Marshal(names)
	devices, _ := json.Marshal(paths)
	r.Services = []json.RawMessage{
		json.RawMessage(`{"placement":{"hosts":` + string(placement) + `},"service_id":"data","service_type":"osd","spec":{"data_devices":{"paths":` + string(devices) + `}}}`),
		json.RawMessage(`{"placement":{"hosts":` + string(placement) + `},"service_type":"mon"}`),
		json.RawMessage(`{"placement":{"hosts":` + string(placement) + `},"service_type":"mgr"}`),
	}
	r.Bootstrap = names[0]
	e := readyEvidence(r)
	e.OSDs, e.MonAddrs = []OSDEvidence{}, []MonitorAddress{}
	for node, name := range names {
		for disk, path := range paths {
			e.OSDs = append(e.OSDs, OSDEvidence{Host: name, Path: path, ID: 10000 + node*7 + disk, UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", node*7+disk)})
		}
		e.MonAddrs = append(e.MonAddrs, MonitorAddress{Host: name, Address: fmt.Sprintf("2001:db8:1111:2222:3333:4444:5555:%x", 0x6600+node)})
	}
	e.MGRActive, e.MGRStandbys = names[0], names[1:]
	data := evidenceBytes(t, e)
	if len(data) > 60*1024 {
		t.Fatalf("ready evidence of %d hosts and %d OSDs takes %d bytes", len(e.Hosts), len(e.OSDs), len(data))
	}
	t.Logf("ready evidence of %d hosts and %d OSDs takes %d of %d bytes", len(e.Hosts), len(e.OSDs), len(data), 60*1024)
	if effect, err := EvaluateEvidence(data, r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready evidence at the host bound: %s %v", effect, diagnostics.Of(err))
	}
}
