package instance_test

import (
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/addons/packages"
)

func TestGitOpsBootstrapProgramAndPlacementNamespace(t *testing.T) {
	registry := packages.Builtin()
	pkg, _ := registry.Find("gitops")
	config := api.MapValue(api.FieldValue{Name: "subscription", Value: api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("latest")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("gitops.v1")})}, api.FieldValue{Name: "bootstrap", Value: api.MapValue(api.FieldValue{Name: "repoURL", Value: api.StringValue("https://example.test/repo.git")}, api.FieldValue{Name: "targetRevision", Value: api.StringValue("main")}, api.FieldValue{Name: "path", Value: api.StringValue(".")}, api.FieldValue{Name: "destinationNamespace", Value: api.StringValue("application")}, api.FieldValue{Name: "credentialsRef", Value: api.StringValue("repository-login")})})
	binding := api.NewObject(api.ClusterAddonBinding, "hub", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "gitops", Value: config})}))
	binding, _ = addons.NormalizePackages(binding, registry)
	selected := addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: "cluster", Binding: "hub", Config: binding.Spec().Get("packages", "gitops")}}
	request, _, err := instance.Render(selected, addons.ClusterFacts{Release: "4.20"}, "controller")
	if err != nil {
		t.Fatal(err)
	}
	program, err := co.ThawVersion(request.Program, co.Options{Namespace: pkg.OLM.Namespace, PlacementNamespaces: pkg.PlacementNamespaces})
	if err != nil {
		t.Fatal(err)
	}
	if request.BudgetSeconds != 1200 || len(request.Secrets) != 1 || request.Secrets[0] != "repository-login" {
		t.Fatal(request)
	}
	placed, available := false, false
	for _, step := range program.Steps {
		if step.Create != nil && step.Create.Kind == "OperatorGroup" {
			var body map[string]any
			if err := json.Unmarshal(step.Create.Body, &body); err != nil {
				t.Fatal(err)
			}
			if _, ok := body["spec"]; ok {
				t.Fatal("AllNamespaces group must omit spec")
			}
		}
		if step.Kind == "wait" {
			for _, check := range step.Wait {
				if p := check.StatusPhase; p != nil && p.Kind == "ArgoCD" && p.Values[0] == "Available" {
					available = true
				}
			}
		}
		if step.Place != nil {
			if !available || step.Place.Namespace != "openshift-gitops" {
				t.Fatal("credential placed before readiness or in wrong namespace")
			}
			placed = true
		}
		if step.Create != nil && step.Create.Kind == "Application" && !placed {
			t.Fatal("Application precedes credential")
		}
	}
	if !placed || !available {
		t.Fatal("missing bootstrap phase")
	}
	pkg.PlacementNamespaces[0] = "foreign"
	fresh, _ := registry.Find("gitops")
	if fresh.PlacementNamespaces[0] != "openshift-gitops" {
		t.Fatal("mutable registry namespace")
	}
}

func TestFDFProducedPlacementFreezesExactDependency(t *testing.T) {
	registry := packages.Builtin()
	pkg, ok := registry.Find("fdf")
	if !ok {
		t.Fatal("fdf not registered")
	}
	config := api.MapValue(api.FieldValue{Name: "subscription", Value: api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("odf.v1")}, api.FieldValue{Name: "source", Value: api.StringValue("redhat-operators")})}, api.FieldValue{Name: "cephConsumerRef", Value: api.StringValue("export")})
	binding := api.NewObject(api.ClusterAddonBinding, "df", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "fdf", Value: config})}))
	binding, _ = addons.NormalizePackages(binding, registry)
	selected := addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: "cluster", Binding: "df", Config: binding.Spec().Get("packages", "fdf")}}
	facts := addons.ClusterFacts{Release: "4.20", Exports: []addons.ConsumerFacts{{Name: "export", Gateway: true}}}
	request, requires, err := instance.Render(selected, facts, "controller")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.ProducedInputs) != 2 || request.ProducedInputs[1].Kind != "CephConsumer" || request.ProducedInputs[1].Object != "export" || request.ProducedInputs[1].Output != "external-details" || len(request.Secrets) != 0 {
		t.Fatal(request)
	}
	if len(requires) != 2 || requires[1].Kind != "CephConsumer" || requires[1].Object != "export" {
		t.Fatal(requires)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	placed := false
	gatewayCheck := false
	for _, step := range program.Steps {
		if step.Place != nil {
			placed = true
			if step.Place.Produced == nil || step.Place.Secret != "" || step.Place.ProducedKey != "external_cluster_details" {
				t.Fatal(step)
			}
		}
		if step.Create != nil && step.Create.Kind == "StorageCluster" && !placed {
			t.Fatal("StorageCluster precedes import")
		}
	}
	for _, check := range program.Checks {
		if check.ResourceExists != nil && check.ResourceExists.Name == "ocs-external-storagecluster-ceph-rgw" {
			gatewayCheck = true
		}
	}
	if !placed || !gatewayCheck {
		t.Fatal("missing import or RGW readiness")
	}
	request.ProducedInputs = request.ProducedInputs[:1]
	if _, err := request.Canonical(); err == nil {
		t.Fatal("missing producer admitted")
	}
}

func TestACMFinalProgramRequiresPinnedEngineAndRunningHub(t *testing.T) {
	registry := packages.Builtin()
	pkg, _ := registry.Find("acm")
	stringsObject := func(pairs ...string) api.Value {
		fields := []api.FieldValue{}
		for i := 0; i < len(pairs); i += 2 {
			fields = append(fields, api.FieldValue{Name: pairs[i], Value: api.StringValue(pairs[i+1])})
		}
		return api.MapValue(fields...)
	}
	config := api.MapValue(api.FieldValue{Name: "subscription", Value: stringsObject("channel", "release-2.14", "startingCSV", "acm.v2.14.0")}, api.FieldValue{Name: "mce", Value: stringsObject("channel", "stable-2.9", "startingCSV", "engine.v2.9.0")}, api.FieldValue{Name: "hub", Value: stringsObject("availabilityConfig", "Basic")})
	binding := api.NewObject(api.ClusterAddonBinding, "hub", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "acm", Value: config})}))
	binding, _ = addons.NormalizePackages(binding, registry)
	request, _, err := instance.Render(addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: "cluster", Binding: "hub", Config: binding.Spec().Get("packages", "acm")}}, addons.ClusterFacts{Release: "4.20"}, "controller")
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	engine, hub := 0, 0
	for _, check := range program.Checks {
		if c := check.CSVSucceeded; c != nil && c.Namespace == "multicluster-engine" {
			if c.Subscription != "multicluster-engine" || c.CSV != "engine.v2.9.0" {
				t.Fatalf("unpinned engine readiness: %+v", c)
			}
			engine++
		}
		if c := check.StatusPhase; c != nil && c.Kind == "MultiClusterHub" && c.Namespace == "open-cluster-management" && c.Name == "multiclusterhub" && c.Path == "status.phase" && len(c.Values) == 1 && c.Values[0] == "Running" {
			hub++
		}
	}
	if engine != 1 || hub != 1 {
		t.Fatalf("final readiness: engine=%d hub=%d", engine, hub)
	}
}
