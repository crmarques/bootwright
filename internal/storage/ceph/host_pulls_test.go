package ceph

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func placedService(serviceType string, placement api.Value) api.Value {
	return requestMap("service_type", serviceType, "placement", placement)
}

func plannedPulls(t *testing.T, services []api.Value, images ...string) (map[string][]ImagePull, map[string][]string) {
	t.Helper()
	state := withImages(t, auxiliaryGraph(t, services), images...)
	notRefused(t, state, "services with their authored images")
	pulls, groups := map[string][]ImagePull{}, map[string][]string{}
	for id, d := range plannedAuxiliary(t, state) {
		if !strings.HasPrefix(id, "ceph-host-") {
			continue
		}
		r, err := DecodeHostRequest(d.Request)
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		pulls[r.Host] = r.Pulls
		for _, group := range d.Groups {
			groups[r.Host] = append(groups[r.Host], group.ID)
		}
	}
	return pulls, groups
}

func TestHostPullsOnlyTheAuxiliaryImagesItsServicesCanScheduleThere(t *testing.T) {
	pulls, groups := plannedPulls(t, []api.Value{
		placedService("grafana", requestMap("hosts", api.StringList("node-1"))),
		placedService("prometheus", requestMap("hosts", api.StringList("node-1", "node-2"), "count", 1)),
		placedService("alertmanager", requestMap("label", "mon", "count", 1)),
		placedService("node-exporter", requestMap("host_pattern", "*")),
	}, "alertmanager", "grafana", "node_exporter", "prometheus")
	want := map[string][]ImagePull{
		"node-1": {{Group: "pull-alertmanager-grafana-node-exporter", Names: []string{"alertmanager", "grafana", "node_exporter"}}, {Group: "pull-prometheus", Names: []string{"prometheus"}}},
		"node-2": {{Group: "pull-alertmanager-node-exporter-prometheus", Names: []string{"alertmanager", "node_exporter", "prometheus"}}},
		"node-3": {{Group: "pull-alertmanager-node-exporter", Names: []string{"alertmanager", "node_exporter"}}},
	}
	if !reflect.DeepEqual(pulls, want) {
		t.Fatalf("host pulls %+v, want %+v", pulls, want)
	}
	if !slices.Equal(groups["node-3"], []string{"install-packages", "pull-ceph-image", "pull-alertmanager-node-exporter", "prove-runtime", "prove-route", "identify-devices"}) {
		t.Fatalf("node-3 groups %v", groups["node-3"])
	}
}

func TestAnSMBResourceKeepsItsSambaImagesOnEveryHost(t *testing.T) {
	candidates := pullCandidates(api.NewCatalog([]api.Object{api.NewObject(api.CephSMBResource, "share", api.Value{}, requestMap("clusterRef", "example"))}),
		api.NewObject(api.CephCluster, "example", api.Value{}, requestMap("topology", requestMap("nodes", api.ListValue(
			requestMap("name", "node-1"), requestMap("name", "node-2"))), "services", api.ListValue(placedService("smb", requestMap("hosts", api.StringList("node-1")))))),
		[]AuxiliaryReference{{Name: "samba"}, {Name: "samba_metrics"}})
	for _, name := range []string{"samba", "samba_metrics"} {
		if !slices.Equal(candidates[name], []string{"node-1", "node-2"}) {
			t.Fatalf("%s candidates %v", name, candidates[name])
		}
	}
}

func TestAnUnresolvedPlacementKeepsTheImageOnEveryHost(t *testing.T) {
	cluster := api.NewObject(api.CephCluster, "example", api.Value{}, requestMap("topology", requestMap("nodes", api.ListValue(
		requestMap("name", "node-1", "labels", api.StringList("mon")), requestMap("name", "node-2"))),
		"services", api.ListValue(placedService("grafana", requestMap("hosts", api.StringList("node-1"))), placedService("grafana", requestMap("label", "absent")))))
	candidates := pullCandidates(api.NewCatalog(nil), cluster, []AuxiliaryReference{{Name: "grafana"}})
	if !slices.Equal(candidates["grafana"], []string{"node-1", "node-2"}) {
		t.Fatalf("grafana candidates %v", candidates["grafana"])
	}
}

func TestEveryAuxiliaryImageFitsTheGroupBoundInBatchesOfThree(t *testing.T) {
	auxiliary := []AuxiliaryReference{}
	for _, name := range AuxiliaryNames {
		auxiliary = append(auxiliary, AuxiliaryReference{Name: name})
	}
	all := map[string][]string{}
	for _, name := range AuxiliaryNames {
		all[name] = []string{"node-1"}
	}
	pulls := hostPulls(all, auxiliary, "node-1")
	groups := hostGroups(reconciliation.Apply, "node-1", pulls)
	if len(pulls) != 5 || len(groups) > reconciliation.MaxGroups {
		t.Fatalf("%d pulls, %d groups", len(pulls), len(groups))
	}
	r := HostRequest{Cluster: ClusterScope{Auxiliary: auxiliary}, Pulls: pulls}
	if !validPulls(r) {
		t.Fatalf("planned pulls refused %+v", pulls)
	}
	definition := reconciliation.BlockDefinition{Groups: groups}
	for _, group := range definition.Groups {
		if !reconciliation.ValidSegment(group.ID) || len(group.Description) > 200 {
			t.Fatalf("group %+v", group)
		}
	}
	for name, change := range map[string]func([]ImagePull) []ImagePull{
		"nil":     func([]ImagePull) []ImagePull { return nil },
		"renamed": func(p []ImagePull) []ImagePull { p[0].Group = "pull-images"; return p },
		"short first": func(p []ImagePull) []ImagePull {
			p[0].Names = p[0].Names[:2]
			p[0].Group = pullGroup(p[0].Names)
			return p
		},
		"reordered":    func(p []ImagePull) []ImagePull { p[0], p[1] = p[1], p[0]; return p },
		"foreign name": func(p []ImagePull) []ImagePull { p[4].Names = []string{"ceph"}; p[4].Group = "pull-ceph"; return p },
		"oversized": func(p []ImagePull) []ImagePull {
			p[4].Names = append(p[4].Names, "x", "y", "z")
			p[4].Group = pullGroup(p[4].Names)
			return p
		},
	} {
		changed := HostRequest{Cluster: r.Cluster, Pulls: change(slices.Clone(hostPulls(all, auxiliary, "node-1")))}
		if validPulls(changed) {
			t.Fatalf("%s pulls admitted", name)
		}
	}
}

func TestHostApplyMarginCoversEveryRoleStepOfTheLargestPullPlan(t *testing.T) {
	auxiliary, candidates := []AuxiliaryReference{}, map[string][]string{}
	for _, name := range AuxiliaryNames {
		auxiliary = append(auxiliary, AuxiliaryReference{Name: name})
		candidates[name] = []string{"node-1"}
	}
	pulls := hostPulls(candidates, auxiliary, "node-1")
	steps := len(hostGroups(reconciliation.Apply, "machine-1", pulls)) + 1
	if len(pulls) != 5 || steps != 11 || maxHostRoleSteps() != steps || hostRoleMargin() < steps*hostRoleStepOverhead {
		t.Fatalf("%d batches, %d role steps, margin %d s for %d steps", len(pulls), steps, hostRoleMargin(), maxHostRoleSteps())
	}
	calls := hostPackagesCall + hostImagesBudget + hostCallSlack + hostLaterCalls*nativeCall
	if hostDeadline("apply") != time.Duration(calls+steps*hostRoleStepOverhead)*time.Second {
		t.Fatalf("host apply deadline %s", hostDeadline("apply"))
	}
}
