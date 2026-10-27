package ceph

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

func TestLogAgentPlansOwnedFileLogging(t *testing.T) {
	for _, agent := range []string{"alloy", "promtail", ""} {
		t.Run(agent, func(t *testing.T) {
			services := []api.Value{auxiliaryService("loki", "", api.Value{})}
			if agent != "" {
				services = append(services, auxiliaryService(agent, "", api.Value{}))
			}
			unit := monitoringUnit(t, services, nil)
			data, err := json.Marshal(unit)
			if err != nil {
				t.Fatal(err)
			}
			var frozen struct {
				Config map[string]map[string]string `json:"config"`
			}
			if err := json.Unmarshal(data, &frozen); err != nil {
				t.Fatal(err)
			}
			if agent == "" {
				if frozen.Config != nil {
					t.Fatalf("a unit without a log agent owns config: %+v", frozen.Config)
				}
			} else if len(frozen.Config) != 1 || len(frozen.Config["global"]) != 2 || frozen.Config["global"]["log_to_file"] != "true" || frozen.Config["global"]["mon_cluster_log_to_file"] != "true" {
				t.Fatalf("a declared log agent has no frozen file logging: %+v", frozen.Config)
			}
		})
	}
}

func loggingRequest(t *testing.T) Request {
	t.Helper()
	r := frozenGoldenRequest()
	unit := monitoringUnit(t, []api.Value{auxiliaryService("loki", "", requestMap("port", 3100)), auxiliaryService("alloy", "", api.Value{})}, nil)
	r.Action, r.Service = "service", &unit
	for _, document := range unitDocuments(r) {
		r.Delegated = append(r.Delegated, DelegatedService{Name: documentName(document), Hosts: document.Placement.Hosts})
	}
	for i, name := range []string{"alloy", "loki"} {
		r.Auxiliary = append(r.Auxiliary, AuxiliaryReference{Name: name, Image: PinnedRegistry + "/synthetic/" + name + "@sha256:" + strings.Repeat(string("de"[i]), 64)})
	}
	return r
}

func TestLoggingRequestGolden(t *testing.T) {
	r := loggingRequest(t)
	if len(r.Delegated) != 2 || r.Delegated[0].Name != "alloy" || r.Delegated[1].Name != "loki" || len(r.Service.Bindings) != 0 {
		t.Fatalf("the logging request delegates %+v and binds %+v", r.Delegated, r.Service.Bindings)
	}
	hosts := `"placement":{"count":1,"hosts":["node-1","node-2","node-3"]}`
	want := []string{`{` + hosts + `,"service_type":"alloy"}`, `{` + hosts + `,"service_type":"loki","spec":{"port":3100}}`}
	if !slices.Equal(documentsOf(*r.Service), want) {
		t.Fatalf("the logging unit documents are:\n%s", strings.Join(documentsOf(*r.Service), "\n"))
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-cluster-logging", data, false)
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := decoded.Canonical()
	if err != nil || !bytes.Equal(data, roundtrip) {
		t.Fatal("the logging request did not round-trip")
	}
	for _, document := range decoded.Service.Documents {
		if bytes.Contains(document, []byte(`"service_id"`)) {
			t.Fatalf("the singleton document %s carries a service_id", document)
		}
	}
}

func loggingState(t *testing.T, services []api.Value, images ...string) *compilation.State {
	t.Helper()
	catalog := settingsCatalog(t, services)
	return withImages(t, compilation.NewState(catalog, catalog, nil), images...)
}

func TestLogCollectionPlansWithSelectedImages(t *testing.T) {
	state := loggingState(t, []api.Value{auxiliaryService("loki", "", api.Value{}), auxiliaryService("promtail", "", api.Value{})}, "loki", "promtail")
	if got := New(api.CephCluster, nil).Unsupported(state); len(got) != 0 {
		t.Fatalf("log collection with its images refuses %+v", got)
	}
	plan := cephPlan(t, state.Effective())
	unit := blockOf(t, plan, "ceph-service-example-monitoring")
	if !slices.Equal(unit.Dependencies, []string{"ceph-cluster-example", "ceph-image-example", "ceph-settings-example"}) {
		t.Fatalf("the monitoring block depends on %v", unit.Dependencies)
	}
	core, err := DecodeRequest(blockOf(t, plan, "ceph-cluster-example").Request)
	if err != nil || core.Service != nil {
		t.Fatalf("the cluster block carries %+v (%v)", core, err)
	}
	delegated := map[string][]string{}
	for _, service := range core.Delegated {
		delegated[service.Name] = service.Hosts
	}
	for _, name := range []string{"loki", "promtail"} {
		if len(delegated[name]) == 0 {
			t.Fatalf("the core request does not delegate %s: %+v", name, core.Delegated)
		}
	}
}

func TestLogCollectionWithoutItsImagesRefusesNamingTheField(t *testing.T) {
	services := []api.Value{auxiliaryService("loki", "", api.Value{}), auxiliaryService("alloy", "", api.Value{})}
	state := loggingState(t, services)
	cluster, _ := state.Effective().Find(api.CephCluster, "example")
	for _, issue := range cephcluster.Validate(cluster, state.Effective()) {
		if strings.HasPrefix(issue.Field, "$.spec.services") || strings.HasPrefix(issue.Field, "$.spec.images") {
			t.Fatalf("admission refuses log collection: %+v", issue)
		}
	}
	unauthoredRefusal(t, state, "alloy", "alloy", "alloy")
	unauthoredRefusal(t, loggingState(t, services, "alloy"), "loki", "loki", "loki")
}
