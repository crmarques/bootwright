package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	extensionrender "github.com/crmarques/bootwright/internal/addons/render"
	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
	"go.yaml.in/yaml/v3"
)

const fusionAddonName = "fusion-data-foundation"

type fusionEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *fusionEventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *fusionEventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type fusionOCRunner struct {
	events *fusionEventLog
}

func (r *fusionOCRunner) Run(_ context.Context, _ string, args []string, input []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty oc arguments")
	}
	switch args[0] {
	case "apply":
		var object struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.Unmarshal(input, &object); err != nil {
			return nil, err
		}
		if object.Kind == "" {
			return nil, fmt.Errorf("applied manifest has no kind")
		}
		r.events.add("apply:" + object.Kind)
		return nil, nil
	case "get":
		if len(args) < 2 {
			return nil, fmt.Errorf("incomplete oc get arguments: %v", args)
		}
		switch args[1] {
		case "catalogsource.operators.coreos.com":
			r.events.add("get:CatalogSource")
			return []byte(`{"status":{"connectionState":{"lastObservedState":"READY"}}}`), nil
		case "subscription.operators.coreos.com":
			r.events.add("get:Subscription")
			return []byte(`{"status":{"installedCSV":"odf-operator.v4.21.0"}}`), nil
		case "clusterserviceversion.operators.coreos.com":
			r.events.add("get:CSV")
			return []byte(`{"status":{"phase":"Succeeded"}}`), nil
		case "storagecluster.ocs.openshift.io":
			r.events.add("get:StorageCluster")
			return []byte(`{"status":{"phase":"Ready","conditions":[{"type":"Available","status":"True"}]}}`), nil
		default:
			return nil, fmt.Errorf("unexpected oc get resource %q", args[1])
		}
	default:
		return nil, fmt.Errorf("unexpected oc arguments: %v", args)
	}
}

type fusionEntitlementEffect struct {
	events *fusionEventLog
}

func (e fusionEntitlementEffect) Run(context.Context) error {
	e.events.add("effect:ibm-entitlement")
	return nil
}

type fusionStepRunner struct {
	events *fusionEventLog
}

func (r fusionStepRunner) Run(_ context.Context, lifecycle string) ([]string, error) {
	if lifecycle != v1alpha1.ClusterAddonStepFollowsOperatorReady {
		return nil, nil
	}
	r.events.add("step:operatorReady")
	return []string{
		"Secret/openshift-storage/rook-ceph-external-cluster-details",
		"StorageCluster/openshift-storage/ocs-external-storagecluster",
	}, nil
}

func TestRegisteredFusionDataFoundationPlansRendersAndAppliesAcrossMultidc(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bootwright-root")
	t.Cleanup(workspace.SetRootDirForTest(root))
	release, err := nativecatalog.Resolve(fusionAddonName, "4.21")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	installedDir, err := nativecatalog.Install(release)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	marker, found, err := nativecatalog.ReadMarker(installedDir)
	if err != nil || !found || marker.Name != fusionAddonName || marker.Version != "4.21" {
		t.Fatalf("registered marker = %+v found=%v err=%v", marker, found, err)
	}

	input := fusionMultidcInput(t)
	state, err := desiredstate.LoadNormalizeValidate([]string{input})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	addon := fusionAddonFromState(t, state)
	wantSource := filepath.Join(installedDir, "add-on.yaml")
	if addon.SourcePath != wantSource {
		t.Fatalf("Fusion Data Foundation source = %q, want registered store path %q", addon.SourcePath, wantSource)
	}
	if _, err := os.Stat(filepath.Join(input, "add-ons", fusionAddonName)); !os.IsNotExist(err) {
		t.Fatalf("input unexpectedly authors %s: %v", fusionAddonName, err)
	}

	if _, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state); err != nil {
		t.Fatalf("render.All: %v", err)
	}
	assertFusionOLMRender(t, addon)

	tasks, err := PlanApplyTasksChecked(applyAllTarget(), state)
	if err != nil {
		t.Fatalf("PlanApplyTasksChecked: %v", err)
	}
	clusters := []string{"dc1-child-ocp", "dc1-metal-ocp", "dc2-child-ocp", "dc2-metal-ocp"}
	for _, cluster := range clusters {
		t.Run(cluster, func(t *testing.T) {
			task := assertTaskPresent(t, tasks, "addon."+cluster+"."+fusionAddonName)
			assertTaskHasDeps(t, tasks, task.Entry.ID, "storage.ceph-storage")
			assertTaskDependsTransitively(t, tasks, task.Entry.ID, "wait."+cluster)
			if task.Extension == nil {
				t.Fatal("planned add-on task has no extension plan")
			}
			assertFusionEntitlement(t, *task.Extension)
			applyFusionPlan(t, *task.Extension)
		})
	}
}

func fusionMultidcInput(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	destination := filepath.Join(t.TempDir(), "input")
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatalf("copy multidc input: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(destination, "add-ons", "openshift-data-foundation")); err != nil {
		t.Fatalf("remove authored Data Foundation add-on: %v", err)
	}
	bindings, err := filepath.Glob(filepath.Join(destination, "clusters", "container", "*", "add-on-binding.yaml"))
	if err != nil || len(bindings) != 4 {
		t.Fatalf("find four cluster bindings: %v (%d)", err, len(bindings))
	}
	for _, path := range bindings {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read binding %s: %v", path, err)
		}
		body := strings.ReplaceAll(string(data), "openshift-data-foundation", fusionAddonName)
		needle := "          value: odf-external-ceph\n"
		if strings.Count(body, needle) != 1 {
			t.Fatalf("binding %s does not have one external-storage input", path)
		}
		body = strings.Replace(body, needle, needle+"        - name: ibm-entitlement\n          value: ibm-entitlement-key\n", 1)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write binding %s: %v", path, err)
		}
	}
	secretsPath := filepath.Join(destination, "secrets.yaml")
	secrets, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatalf("read secrets: %v", err)
	}
	secrets = append(secrets, []byte("\n---\napiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: ibm-entitlement-key\nspec:\n  type: opaque\n")...)
	if err := os.WriteFile(secretsPath, secrets, 0o600); err != nil {
		t.Fatalf("write secrets: %v", err)
	}
	return destination
}

func fusionAddonFromState(t *testing.T, state v1alpha1.State) v1alpha1.ClusterAddon {
	t.Helper()
	var found []v1alpha1.ClusterAddon
	for _, addon := range state.ClusterAddons {
		if addon.Metadata.Name == fusionAddonName {
			found = append(found, addon)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Fusion Data Foundation descriptors = %d, want 1", len(found))
	}
	return found[0]
}

func assertFusionOLMRender(t *testing.T, addon v1alpha1.ClusterAddon) {
	t.Helper()
	resources, err := extensionrender.OLMResources(addon)
	if err != nil {
		t.Fatalf("OLMResources: %v", err)
	}
	var identities []string
	for _, resource := range resources {
		identities = append(identities, resource.Kind+"/"+resource.Name)
	}
	want := "CatalogSource/isf-data-foundation-catalog,Namespace/openshift-storage,OperatorGroup/openshift-storage,Subscription/odf-operator"
	if strings.Join(identities, ",") != want {
		t.Fatalf("rendered OLM resources = %v, want %s", identities, want)
	}
	catalogSpec := resources[0].Object["spec"].(map[string]any)
	if catalogSpec["image"] != "icr.io/cpopen/isf-data-foundation-catalog:v4.21" {
		t.Fatalf("CatalogSource image = %v", catalogSpec["image"])
	}
	podConfig, _ := catalogSpec["grpcPodConfig"].(map[string]any)
	if podConfig["securityContextConfig"] != "restricted" {
		t.Fatalf("CatalogSource grpcPodConfig = %v", podConfig)
	}
	subscriptionSpec := resources[3].Object["spec"].(map[string]any)
	if subscriptionSpec["channel"] != "stable-4.21" || subscriptionSpec["source"] != "isf-data-foundation-catalog" {
		t.Fatalf("Subscription spec = %v", subscriptionSpec)
	}
}

func assertFusionEntitlement(t *testing.T, plan extensionplan.ExtensionPlan) {
	t.Helper()
	var supplied bool
	for _, input := range plan.Inputs {
		if input.Name == "ibm-entitlement" && input.Value == "ibm-entitlement-key" {
			supplied = true
		}
	}
	var effect bool
	for _, input := range plan.Extension.Spec.Accepts.Inputs {
		if input.Name != "ibm-entitlement" {
			continue
		}
		for _, candidate := range input.Effects {
			merge := candidate.GlobalPullSecretMerge
			if merge != nil && merge.Registry == "cp.icr.io" && merge.Username == "cp" {
				effect = true
			}
		}
	}
	if !supplied || !effect {
		t.Fatalf("Fusion entitlement supplied=%v effect=%v inputs=%v", supplied, effect, plan.Inputs)
	}
}

func applyFusionPlan(t *testing.T, plan extensionplan.ExtensionPlan) {
	t.Helper()
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	events := &fusionEventLog{}
	runner := &fusionOCRunner{events: events}
	cfg := extensionoc.RunConfig{
		ClustersDir:  filepath.Join(dir, "clusters"),
		Kubeconfig:   kubeconfig,
		RunID:        "integration",
		StartedAt:    time.Now(),
		PollInterval: time.Millisecond,
		ReadRunner:   runner,
		Effects:      fusionEntitlementEffect{events: events},
		Steps:        fusionStepRunner{events: events},
	}
	if _, err := extensionoc.Apply(context.Background(), runner, cfg, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := extensionoc.Wait(context.Background(), runner, cfg, plan); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	record, found, err := extensionrecords.LoadRecord(cfg.ClustersDir, plan.Cluster, plan.Name)
	if err != nil || !found || record.Status != extensionrecords.RecordStatusReady || record.Phase != extensionrecords.RecordPhaseComplete {
		t.Fatalf("ready record = %+v found=%v err=%v", record, found, err)
	}
	got := events.snapshot()
	assertFusionEventOrder(t, got, "effect:ibm-entitlement", "apply:CatalogSource")
	assertFusionEventOrder(t, got, "apply:CatalogSource", "get:CatalogSource", "apply:Subscription")
	assertFusionEventOrder(t, got, "get:Subscription", "get:CSV", "step:operatorReady", "get:StorageCluster")
}

func assertFusionEventOrder(t *testing.T, events []string, ordered ...string) {
	t.Helper()
	position := -1
	for _, want := range ordered {
		found := -1
		for i := position + 1; i < len(events); i++ {
			if events[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("event %q does not follow %v in %v", want, ordered[:1], events)
		}
		position = found
	}
}
