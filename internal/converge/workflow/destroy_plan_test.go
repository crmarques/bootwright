package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	renderinventory "github.com/crmarques/bootwright/internal/render/inventory"
	"github.com/crmarques/bootwright/internal/roles"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func destroyTaskByID(t *testing.T, tasks []ApplyTask, id string) ApplyTask {
	t.Helper()
	for _, task := range tasks {
		if task.Entry.ID == id {
			return task
		}
	}
	t.Fatalf("destroy plan has no task %q", id)
	return ApplyTask{}
}

func destroyTaskIDs(tasks []ApplyTask) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.Entry.ID)
	}
	return out
}

func assertDestroyOrderingEdges(t *testing.T, tasks []ApplyTask, want map[string][]string) {
	t.Helper()
	for _, task := range tasks {
		wanted, ok := want[task.Entry.ID]
		if !ok {
			t.Fatalf("unexpected destroy task %q in plan %v", task.Entry.ID, destroyTaskIDs(tasks))
		}
		got := task.Entry.OrderingDependencies
		if len(got) == 0 && len(wanted) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, wanted) {
			t.Fatalf("task %q ordering deps = %v, want %v", task.Entry.ID, got, wanted)
		}
	}
	if len(tasks) != len(want) {
		t.Fatalf("planned %v, want edges for %d tasks", destroyTaskIDs(tasks), len(want))
	}
}

func TestDestroyTaskKindsRegistryCoversEveryConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "destroy_plan.go", nil, 0)
	if err != nil {
		t.Fatalf("parse destroy_plan.go: %v", err)
	}
	declared := map[string]string{}
	declaredCount := 0
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "DestroyTaskKind") {
				continue
			}
			if i >= len(spec.Values) {
				t.Errorf("%s has no explicit string value; every destroy task kind must enter the outcome registry", name.Name)
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s is not an explicit string destroy task kind", name.Name)
				continue
			}
			declaredCount++
			value := strings.Trim(lit.Value, `"`)
			if previous := declared[value]; previous != "" {
				t.Errorf("%s and %s both declare destroy task kind %q; every kind must have one canonical constant", previous, name.Name, value)
			}
			declared[value] = name.Name
			if !IsDestroyTaskKind(value) {
				t.Errorf("%s = %q is not in destroyTaskKinds; successful teardown could not clear its apply evidence", name.Name, value)
			}
		}
		return true
	})
	if declaredCount == 0 {
		t.Fatal("found no DestroyTaskKind constants to check; the guard would pass vacuously")
	}
	for kind := range destroyTaskKinds {
		if declared[kind] == "" {
			t.Errorf("destroy task registry holds undeclared or retired kind %q", kind)
		}
	}
	if declaredCount != len(destroyTaskKinds) || len(declared) != len(destroyTaskKinds) {
		t.Fatalf("destroy task registry has %d entries for %d constants with %d distinct values", len(destroyTaskKinds), declaredCount, len(declared))
	}
}

func TestPlanDestroyTasksInfraChain(t *testing.T) {
	limit := "bootwright_provider_hosts:bootwright_infra_component_hosts:bootwright_infra_hosts"
	extra := []string{"bootwright_infra_destroy_context_sweep=true"}
	tasks, err := PlanDestroyTasks("infra", v1alpha1.State{}, limit, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"destroy.controller-name-resolution-preflight",
		"destroy.machine-registration",
		"destroy.machine-infra",
		"destroy.infra-components",
		"destroy.provider-services",
		"destroy.controller-name-resolution-cleanup",
	}
	if got := destroyTaskIDs(tasks); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("infra destroy plan = %v, want %v; teardown is the inverse of build-up, where every machines-phase task depends on the fabric services, so fabric teardown comes last", got, wantIDs)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.controller-name-resolution-preflight": nil,
		"destroy.machine-registration":                 nil,
		"destroy.machine-infra":                        {"destroy.machine-registration"},
		"destroy.infra-components":                     {"destroy.machine-infra", "destroy.machine-registration"},
		"destroy.provider-services":                    {"destroy.machine-infra", "destroy.infra-components"},
		"destroy.controller-name-resolution-cleanup":   nil,
	})
	for i, task := range tasks {
		wantLimit := limit
		if task.Entry.ID == "destroy.controller-name-resolution-preflight" || task.Entry.ID == "destroy.controller-name-resolution-cleanup" {
			wantLimit = render.GroupControllerHosts
		} else if task.Entry.ID == "destroy.machine-registration" {
			wantLimit = "bootwright_storage_hosts"
		} else if task.Entry.ID == "destroy.machine-infra" {
			wantLimit = "bootwright_machine_task_hosts:bootwright_provider_hosts:bootwright_infra_hosts"
		}
		if task.Limit != wantLimit {
			t.Fatalf("task[%d] limit = %q, want %q", i, task.Limit, wantLimit)
		}
		if len(task.ExtraVarPairs) != 1 || task.ExtraVarPairs[0] != extra[0] {
			t.Fatalf("task[%d] extra-vars = %v, want %v", i, task.ExtraVarPairs, extra)
		}
		wantDependencies := []string(nil)
		wantSuccessDependencies := []string(nil)
		switch task.Entry.ID {
		case "destroy.machine-registration", "destroy.machine-infra", "destroy.infra-components", "destroy.provider-services":
			wantSuccessDependencies = []string{"destroy.controller-name-resolution-preflight"}
		case "destroy.controller-name-resolution-cleanup":
			wantDependencies = []string{"destroy.machine-registration", "destroy.machine-infra", "destroy.infra-components", "destroy.provider-services"}
		}
		if !reflect.DeepEqual(task.Entry.Dependencies, wantDependencies) {
			t.Fatalf("task[%d] skip-tolerant deps = %v, want %v", i, task.Entry.Dependencies, wantDependencies)
		}
		if !reflect.DeepEqual(task.Entry.SuccessDependencies, wantSuccessDependencies) {
			t.Fatalf("task[%d] success deps = %v, want %v", i, task.Entry.SuccessDependencies, wantSuccessDependencies)
		}
	}
	if tasks[0].Playbook == "" || tasks[0].Playbook == tasks[1].Playbook {
		t.Fatalf("tasks must carry distinct destroy playbooks: %q / %q", tasks[0].Playbook, tasks[1].Playbook)
	}
}

func TestPlanDestroyTasksMachineInfraUsesOneForkPerDeclaredHost(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("infra", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Entry.Kind != DestroyTaskKindMachineInfra {
			continue
		}
		if task.Limit != "bootwright_machine_task_hosts:bootwright_provider_hosts:bootwright_infra_hosts" {
			t.Fatalf("machine infra destroy limit = %q", task.Limit)
		}
		if task.Forks != 4 {
			t.Fatalf("machine infra destroy forks = %d, want 4 for three VM task hosts plus their provider host", task.Forks)
		}
		return
	}
	t.Fatal("infra destroy plan has no machine infrastructure task")
}

func TestPlanDestroyTasksMachineInfraNamesTheClustersItTearsDown(t *testing.T) {
	state := loadWorkflowFixtureState(t, "003-3nodes-libvirt")
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Entry.Kind != DestroyTaskKindMachineInfra {
			continue
		}
		if len(DestroyTaskClusterKeys(task.Entry)) == 0 {
			t.Fatalf("%q carries no cluster key; the row that actually destroys nodes would render nameless", task.Entry.ID)
		}
		return
	}
	t.Fatal("full destroy plan has no machine infrastructure task")
}

func TestPlanDestroyTasksClustersChain(t *testing.T) {
	tasks, err := PlanDestroyTasks("clusters", destroyStorageFanOutState(map[string][]string{"ceph-a": {"a1"}}), "limit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"destroy.cluster-runtime", "destroy.storage-clusters", "destroy.container-clusters", "destroy.storage-node-access"}
	if got := destroyTaskIDs(tasks); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("clusters chain = %v, want %v", got, wantIDs)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.cluster-runtime":     nil,
		"destroy.storage-clusters":    nil,
		"destroy.container-clusters":  {"destroy.cluster-runtime", "destroy.storage-clusters"},
		"destroy.storage-node-access": {"destroy.container-clusters"},
	})
	if got := destroyTaskByID(t, tasks, "destroy.cluster-runtime").Entry.Kind; got != DestroyTaskKindContainerClusterRuntime {
		t.Fatalf("cluster runtime teardown must carry its own kind so it is not folded into the records half, got %q", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.storage-node-access").Entry.Kind; got != DestroyTaskKindStorageNodeAccess {
		t.Fatalf("storage node access revoke must carry its own distinct kind, got %q", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.storage-node-access").Entry.SuccessDependencies; !reflect.DeepEqual(got, []string{DestroyStorageClustersTaskID}) {
		t.Fatalf("storage node access success deps = %v, want exact storage proof", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.container-clusters").Entry.SuccessDependencies; slices.Contains(got, DestroyStorageClustersTaskID) {
		t.Fatalf("cluster records are an independent ordering-only branch, success deps=%v", got)
	}
	for _, task := range tasks {
		if len(task.Entry.Dependencies) != 0 {
			t.Fatalf("the clusters chain plans no machine teardown, so nothing in it may be hard-gated: %q has %v", task.Entry.ID, task.Entry.Dependencies)
		}
	}
}

func TestPlanDestroyTasksSplitsRuntimeFromRecordsAtEveryFleetSize(t *testing.T) {
	for _, scope := range []string{"clusters", "all"} {
		tasks, err := PlanDestroyTasks(scope, v1alpha1.State{}, "limit", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		runtime := destroyTaskByID(t, tasks, "destroy.cluster-runtime")
		records := destroyTaskByID(t, tasks, "destroy.container-clusters")
		if runtime.Playbook == records.Playbook {
			t.Fatalf("%s: runtime and records teardown share playbook %q; the records half must not re-run the runtime half", scope, runtime.Playbook)
		}
		for _, pair := range []struct {
			id   string
			task ApplyTask
		}{{"destroy.cluster-runtime", runtime}, {"destroy.container-clusters", records}} {
			for _, pairVar := range pair.task.ExtraVarPairs {
				if strings.Contains(pairVar, "records_only") {
					t.Fatalf("%s: %s carries %q; records-only is no longer a per-fleet-size toggle", scope, pair.id, pairVar)
				}
			}
		}
	}
}

func TestPlanDestroyTasksAllChain(t *testing.T) {
	limit := ""
	extra := []string{"bootwright_infra_destroy_context_sweep=true"}
	tasks, err := PlanDestroyTasks("all", v1alpha1.State{}, limit, extra, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"destroy.controller-name-resolution-preflight",
		"destroy.cluster-runtime",
		"destroy.storage-clusters",
		"destroy.machine-registration",
		"destroy.storage-node-access",
		"destroy.machine-infra",
		"destroy.container-clusters",
		"destroy.infra-components",
		"destroy.provider-services",
		"destroy.controller-name-resolution-cleanup",
	}
	if got := destroyTaskIDs(tasks); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("full destroy plan = %v, want %v", got, wantIDs)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.controller-name-resolution-preflight": nil,
		"destroy.cluster-runtime":                      nil,
		"destroy.storage-clusters":                     nil,
		"destroy.machine-registration":                 nil,
		"destroy.storage-node-access":                  nil,
		"destroy.machine-infra": {
			"destroy.cluster-runtime",
			"destroy.storage-node-access",
			"destroy.machine-registration",
		},
		"destroy.container-clusters": {"destroy.cluster-runtime", "destroy.storage-clusters"},
		"destroy.infra-components": {
			"destroy.machine-infra",
			"destroy.storage-node-access",
			"destroy.machine-registration",
		},
		"destroy.provider-services":                  {"destroy.machine-infra", "destroy.container-clusters", "destroy.infra-components"},
		"destroy.controller-name-resolution-cleanup": nil,
	})
	for i, task := range tasks {
		if len(task.ExtraVarPairs) != 1 || task.ExtraVarPairs[0] != extra[0] {
			t.Fatalf("task[%d] extra-vars = %v, want %v", i, task.ExtraVarPairs, extra)
		}
		wantDependencies := []string(nil)
		wantSuccessDependencies := []string(nil)
		switch task.Entry.ID {
		case "destroy.cluster-runtime", "destroy.storage-clusters", "destroy.machine-registration", "destroy.storage-node-access", "destroy.infra-components", "destroy.provider-services":
			wantSuccessDependencies = []string{"destroy.controller-name-resolution-preflight"}
		case "destroy.container-clusters":
			wantDependencies = []string{"destroy.machine-infra"}
			wantSuccessDependencies = []string{"destroy.controller-name-resolution-preflight"}
		case "destroy.machine-infra":
			wantSuccessDependencies = []string{"destroy.controller-name-resolution-preflight"}
		case "destroy.controller-name-resolution-cleanup":
			wantDependencies = []string{
				"destroy.cluster-runtime",
				"destroy.storage-clusters",
				"destroy.machine-registration",
				"destroy.storage-node-access",
				"destroy.machine-infra",
				"destroy.container-clusters",
				"destroy.infra-components",
				"destroy.provider-services",
			}
		}
		if !reflect.DeepEqual(task.Entry.Dependencies, wantDependencies) {
			t.Fatalf("task[%d] skip-tolerant deps = %v, want %v", i, task.Entry.Dependencies, wantDependencies)
		}
		if !reflect.DeepEqual(task.Entry.SuccessDependencies, wantSuccessDependencies) {
			t.Fatalf("task[%d] success deps = %v, want %v", i, task.Entry.SuccessDependencies, wantSuccessDependencies)
		}
	}
	nodeAccess := slices.Index(wantIDs, "destroy.storage-node-access")
	registration := slices.Index(wantIDs, "destroy.machine-registration")
	storage := slices.Index(wantIDs, "destroy.storage-clusters")
	machineInfra := slices.Index(wantIDs, "destroy.machine-infra")
	runtime := slices.Index(wantIDs, "destroy.cluster-runtime")
	if !(storage < registration && registration < nodeAccess) {
		t.Fatalf("every step that connects to bootwright_storage_hosts with the run's statically rendered ansible_user must precede the node-access revoke: %v", wantIDs)
	}
	if nodeAccess > machineInfra {
		t.Fatalf("node-access revoke must precede machine teardown: once the substrate deletes the VMs, task_storage_node_access_destroy.yml ends every host on the unreachable probe and revocation silently no-ops while still reporting success; got %v", wantIDs)
	}
	if runtime > storage || runtime > machineInfra {
		t.Fatalf("cluster runtime teardown is the inverse of the terminal add-ons phase, so it is the graph root and must precede both storage and machine teardown: %v", wantIDs)
	}
	if machineInfra > slices.Index(wantIDs, "destroy.infra-components") {
		t.Fatalf("apply makes every machines-phase task depend on the fabric services, so the inverse tears the fabric down after the machines it serves: %v", wantIDs)
	}
	if got := destroyTaskByID(t, tasks, "destroy.container-clusters").Entry.Dependencies; !reflect.DeepEqual(got, []string{"destroy.machine-infra"}) {
		t.Fatalf("the records half must require successful machine teardown: kubeVirtHostClustersForRun materialises every KubeVirt host kubeconfig for each machine-infra task, so deleting one while any machine teardown is still runnable strands the guests; got %v", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.machine-registration").Entry.OrderingDependencies; slices.Contains(got, "destroy.infra-components") {
		t.Fatalf("RHSM deregistration runs through bootwright_proxy_env, so the proxy InfraComponent must still exist: %v", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.infra-components").Entry.OrderingDependencies; !slices.Contains(got, "destroy.machine-registration") {
		t.Fatalf("the proxy InfraComponent carries RHSM egress, so deregistration must complete before the fabric is torn down; got %v", got)
	}
	if got := destroyTaskByID(t, tasks, "destroy.provider-services").Entry.OrderingDependencies; !slices.Contains(got, "destroy.container-clusters") {
		t.Fatalf("provider services must remain behind the container-cluster teardown they supported; got %v", got)
	}
}

func TestControllerNameResolutionDestroyBracketHardGatesEveryInfraMutation(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		state v1alpha1.State
	}{
		{name: "infra empty", scope: "infra"},
		{name: "all empty", scope: "all"},
		{name: "all selected resources", scope: "all", state: loadWorkflowFixtureState(t, "001-sno-libvirt")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := PlanDestroyTasks(tc.scope, tc.state, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			preflight := destroyTaskByID(t, tasks, destroyControllerNameResolutionPreflightTaskID)
			cleanup := destroyTaskByID(t, tasks, destroyControllerNameResolutionCleanupTaskID)
			if preflight.Entry.Kind != DestroyTaskKindControllerNameResolution || cleanup.Entry.Kind != DestroyTaskKindControllerNameResolution {
				t.Fatalf("controller bracket kinds = %q/%q, want %q", preflight.Entry.Kind, cleanup.Entry.Kind, DestroyTaskKindControllerNameResolution)
			}
			if preflight.Playbook != roles.PlaybookTaskControllerNameResolutionDestroyPreflight || cleanup.Playbook != roles.PlaybookTaskControllerNameResolutionDestroyCleanup {
				t.Fatalf("controller bracket playbooks = %q/%q", preflight.Playbook, cleanup.Playbook)
			}
			var successDependencies []string
			var skipTolerantDependencies []string
			for _, task := range tasks {
				if task.Entry.ID == preflight.Entry.ID || task.Entry.ID == cleanup.Entry.ID {
					continue
				}
				if !slices.Contains(task.Entry.SuccessDependencies, preflight.Entry.ID) {
					t.Errorf("destroy mutation %s does not require successful controller ownership preflight: %v", task.Entry.ID, task.Entry.SuccessDependencies)
				}
				if DestroyTaskNeedsCompletionProof(task.Entry) && len(task.Entry.ResourceKeys) > 0 {
					successDependencies = append(successDependencies, task.Entry.ID)
					if !slices.Contains(cleanup.Entry.SuccessDependencies, task.Entry.ID) || slices.Contains(cleanup.Entry.Dependencies, task.Entry.ID) {
						t.Errorf("controller cleanup does not require success only from identity-bearing mutation %s: deps=%v success=%v", task.Entry.ID, cleanup.Entry.Dependencies, cleanup.Entry.SuccessDependencies)
					}
				} else {
					skipTolerantDependencies = append(skipTolerantDependencies, task.Entry.ID)
					if !slices.Contains(cleanup.Entry.Dependencies, task.Entry.ID) || slices.Contains(cleanup.Entry.SuccessDependencies, task.Entry.ID) {
						t.Errorf("controller cleanup does not accept a skipped empty mutation %s after waiting for it: deps=%v success=%v", task.Entry.ID, cleanup.Entry.Dependencies, cleanup.Entry.SuccessDependencies)
					}
				}
			}
			if len(successDependencies)+len(skipTolerantDependencies) == 0 {
				t.Fatal("destroy plan has no bracketed mutation; the dependency assertion would pass vacuously")
			}
			if !reflect.DeepEqual(cleanup.Entry.Dependencies, skipTolerantDependencies) {
				t.Fatalf("controller cleanup skip-tolerant dependencies = %v, want empty mutations in plan order %v", cleanup.Entry.Dependencies, skipTolerantDependencies)
			}
			if !reflect.DeepEqual(cleanup.Entry.SuccessDependencies, successDependencies) {
				t.Fatalf("controller cleanup success dependencies = %v, want identity-bearing mutations in plan order %v", cleanup.Entry.SuccessDependencies, successDependencies)
			}
		})
	}
	clusters, err := PlanDestroyTasks("clusters", v1alpha1.State{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range clusters {
		if task.Entry.Kind == DestroyTaskKindControllerNameResolution {
			t.Fatalf("clusters-only destroy planned controller resolver teardown %s", task.Entry.ID)
		}
	}
}

func TestManagedNameResolutionPlacementDestroyKeepsResolverIndependentConnectionUntilCleanup(t *testing.T) {
	state := loadControllerResolverState(t, "sno-libvirt-redfish")
	const resolverHost = "bastion"
	const sshAddress = "192.0.2.53"
	found := false
	for i := range state.Machines {
		if state.Machines[i].Metadata.Name != resolverHost {
			continue
		}
		for j := range state.Machines[i].Spec.Addresses {
			if state.Machines[i].Spec.Addresses[j].Name == state.Machines[i].Spec.Access.SSH.AddressRef.Name {
				state.Machines[i].Spec.Addresses[j].Address = sshAddress
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("managed resolver host %q has no SSH address to exercise", resolverHost)
	}
	providerHosts := render.HostGroupMembers(state)[render.GroupProviderHosts]
	if !slices.Contains(providerHosts, resolverHost) {
		t.Fatalf("provider hosts = %v, want managed resolver placement host %q", providerHosts, resolverHost)
	}
	inventory := renderinventory.Inventory(state, "")
	all := inventory["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	host := hosts[resolverHost].(map[string]any)
	if got := host["ansible_host"]; got != sshAddress || net.ParseIP(got.(string)) == nil {
		t.Fatalf("managed resolver placement host ansible_host = %v, want explicit SSH IP %s independent of the resolver it hosts", got, sshAddress)
	}
	if got := host["ansible_connection"]; got == "local" {
		t.Fatalf("managed resolver placement host unexpectedly uses controller-local connection: %v", host)
	}

	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatalf("plan destroy: %v", err)
	}
	infra := destroyTaskByID(t, tasks, destroyInfraComponentsTaskID)
	provider := destroyTaskByID(t, tasks, destroyProviderServicesTaskID)
	cleanup := destroyTaskByID(t, tasks, destroyControllerNameResolutionCleanupTaskID)
	if !slices.Contains(provider.Entry.OrderingDependencies, infra.Entry.ID) {
		t.Fatalf("provider teardown ordering dependencies = %v, want name-resolution teardown %s first", provider.Entry.OrderingDependencies, infra.Entry.ID)
	}
	if !slices.Contains(cleanup.Entry.SuccessDependencies, provider.Entry.ID) {
		t.Fatalf("controller cleanup success dependencies = %v, want resolver-independent provider teardown %s to carry exact completion proof first", cleanup.Entry.SuccessDependencies, provider.Entry.ID)
	}
	if tasks[len(tasks)-1].Entry.ID != cleanup.Entry.ID {
		t.Fatalf("last destroy task = %s, want controller cleanup %s", tasks[len(tasks)-1].Entry.ID, cleanup.Entry.ID)
	}
}

func TestDestroySkipOrphanSweepPreservesGraphWithoutControllerBracket(t *testing.T) {
	extra := []string{DestroySkipOrphanSweepExtraVar + "=true"}
	for _, scope := range []string{"infra", "all"} {
		t.Run(scope, func(t *testing.T) {
			normal, err := PlanDestroyTasks(scope, v1alpha1.State{}, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			suppressed, err := PlanDestroyTasks(scope, v1alpha1.State{}, "", extra, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantByID := map[string]ApplyTask{}
			var wantIDs []string
			for _, task := range normal {
				if task.Entry.Kind == DestroyTaskKindControllerNameResolution {
					continue
				}
				filtered := task.Entry.SuccessDependencies[:0:0]
				for _, dependency := range task.Entry.SuccessDependencies {
					if dependency != destroyControllerNameResolutionPreflightTaskID {
						filtered = append(filtered, dependency)
					}
				}
				task.Entry.SuccessDependencies = filtered
				wantByID[task.Entry.ID] = task
				wantIDs = append(wantIDs, task.Entry.ID)
			}
			if got := destroyTaskIDs(suppressed); !reflect.DeepEqual(got, wantIDs) {
				t.Fatalf("suppressed destroy graph = %v, want the normal graph without controller preflight/cleanup %v", got, wantIDs)
			}
			for _, task := range suppressed {
				want := wantByID[task.Entry.ID]
				if task.Entry.Kind != want.Entry.Kind || task.Playbook != want.Playbook || task.Limit != want.Limit || !slices.Equal(task.Entry.Dependencies, want.Entry.Dependencies) || !slices.Equal(task.Entry.SuccessDependencies, want.Entry.SuccessDependencies) || !slices.Equal(task.Entry.OrderingDependencies, want.Entry.OrderingDependencies) {
					t.Errorf("suppressed task %s changed outside bracket removal: got kind=%q playbook=%q limit=%q deps=%v success=%v ordering=%v; want kind=%q playbook=%q limit=%q deps=%v success=%v ordering=%v", task.Entry.ID, task.Entry.Kind, task.Playbook, task.Limit, task.Entry.Dependencies, task.Entry.SuccessDependencies, task.Entry.OrderingDependencies, want.Entry.Kind, want.Playbook, want.Limit, want.Entry.Dependencies, want.Entry.SuccessDependencies, want.Entry.OrderingDependencies)
				}
				if !reflect.DeepEqual(task.ExtraVarPairs, extra) {
					t.Errorf("suppressed task %s extra vars = %v, want %v", task.Entry.ID, task.ExtraVarPairs, extra)
				}
			}
		})
	}
}

func TestPlanDestroyTasksRelinksOrderingAcrossSkippedSteps(t *testing.T) {
	tasks, err := PlanDestroyTasks("all", v1alpha1.State{}, "", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.controller-name-resolution-preflight": nil,
		"destroy.cluster-runtime":                      nil,
		"destroy.machine-infra":                        {"destroy.cluster-runtime"},
		"destroy.container-clusters":                   {"destroy.cluster-runtime"},
		"destroy.infra-components":                     {"destroy.machine-infra"},
		"destroy.provider-services":                    {"destroy.machine-infra", "destroy.container-clusters", "destroy.infra-components"},
		"destroy.controller-name-resolution-cleanup":   nil,
	})

	clusters, err := PlanDestroyTasks("clusters", v1alpha1.State{}, "limit", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	assertDestroyOrderingEdges(t, clusters, map[string][]string{
		"destroy.cluster-runtime":    nil,
		"destroy.container-clusters": {"destroy.cluster-runtime"},
	})
}

func TestPlanDestroyTasksRejectsUnknownScope(t *testing.T) {
	if _, err := PlanDestroyTasks("bogus", v1alpha1.State{}, "", nil, nil); err == nil {
		t.Fatal("expected an error for an unsupported destroy scope")
	}
}

func TestPlanDestroyTasksOrdersKubeVirtTenantBeforeHost(t *testing.T) {
	state := destroyKubeVirtDependencyState("child", "host")
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Entry.Kind != DestroyTaskKindMachineInfra {
			continue
		}
		want := DestroyClusterOrderExtraVar + "=child,host"
		if !slices.Contains(task.ExtraVarPairs, want) {
			t.Fatalf("machine teardown extra vars = %v, want %q", task.ExtraVarPairs, want)
		}
		wantLevels := DestroyClusterLevelsExtraVar + "=child;host"
		if !slices.Contains(task.ExtraVarPairs, wantLevels) {
			t.Fatalf("machine teardown extra vars = %v, want %q", task.ExtraVarPairs, wantLevels)
		}
		return
	}
	t.Fatal("full destroy plan has no machine infrastructure task")
}

func destroyExtraVar(t *testing.T, task ApplyTask, name string) string {
	t.Helper()
	for _, pair := range task.ExtraVarPairs {
		if key, value, ok := strings.Cut(pair, "="); ok && key == name {
			return value
		}
	}
	t.Fatalf("machine teardown extra vars = %v, want %q", task.ExtraVarPairs, name)
	return ""
}

func TestPlanDestroyTasksGroupsIndependentClustersIntoOneLevel(t *testing.T) {
	lower := destroyKubeVirtDependencyState("tenant", "middle")
	upper := destroyKubeVirtDependencyState("middle", "base")
	state := lower
	state.Machines = append(state.Machines, upper.Machines...)
	state.InfraProviders = append(state.InfraProviders, upper.InfraProviders...)
	state.ContainerClusters = []v1alpha1.ContainerCluster{
		lower.ContainerClusters[0],
		upper.ContainerClusters[0],
		upper.ContainerClusters[1],
		{Metadata: v1alpha1.Metadata{Name: "solo"}},
	}
	state.StorageClusters = []v1alpha1.StorageCluster{{Metadata: v1alpha1.Metadata{Name: "ceph"}}}

	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task := destroyTaskByID(t, tasks, "destroy.machine-infra")

	levels := strings.Split(destroyExtraVar(t, task, DestroyClusterLevelsExtraVar), ";")
	wantLevels := []string{"ceph,solo,tenant", "middle", "base"}
	if !reflect.DeepEqual(levels, wantLevels) {
		t.Fatalf("machine teardown levels = %v, want %v; clusters with no KubeVirt host edge must tear down in one barrier instead of one pass each", levels, wantLevels)
	}

	levelOf := map[string]int{}
	for i, level := range levels {
		for _, name := range strings.Split(level, ",") {
			levelOf[name] = i
		}
	}
	for child, parent := range map[string]string{"tenant": "middle", "middle": "base"} {
		if levelOf[child] >= levelOf[parent] {
			t.Fatalf("guest cluster %q (level %d) must tear down in a strictly earlier level than its KubeVirt host %q (level %d)", child, levelOf[child], parent, levelOf[parent])
		}
	}

	wantOrder := DestroyClusterOrderExtraVar + "=ceph,solo,tenant,middle,base"
	if !slices.Contains(task.ExtraVarPairs, wantOrder) {
		t.Fatalf("the flat order var is consumed by the preparation play with a comma split, so it must stay wire-compatible; got %v, want %q", task.ExtraVarPairs, wantOrder)
	}
	if got := destroyExtraVar(t, task, DestroyClusterOrderExtraVar); strings.Contains(got, ";") {
		t.Fatalf("flat destroy order must not carry level separators: %q", got)
	}
}

func TestPlanDestroyTasksLevelsCoverManagedOSInstallGroups(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph-b"}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-a"}},
		},
	}
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	task := destroyTaskByID(t, tasks, "destroy.machine-infra")
	if got := destroyExtraVar(t, task, DestroyClusterLevelsExtraVar); got != "ceph-a,ceph-b" {
		t.Fatalf("machine teardown levels = %q; the managed-OS install groups are named after their storage cluster and must appear in the levels the substrate play loops, or their machines are never torn down", got)
	}
}

func TestPlanDestroyTasksRejectsKubeVirtHostCycle(t *testing.T) {
	state := destroyKubeVirtDependencyState("a", "b")
	second := destroyKubeVirtDependencyState("b", "a")
	state.Machines = append(state.Machines, second.Machines...)
	state.InfraProviders = append(state.InfraProviders, second.InfraProviders...)
	state.ContainerClusters = []v1alpha1.ContainerCluster{state.ContainerClusters[0], second.ContainerClusters[0]}
	if _, err := PlanDestroyTasks("all", state, "", nil, nil); err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("KubeVirt host cycle error = %v", err)
	}
}

func destroyKubeVirtDependencyState(child, host string) v1alpha1.State {
	machineName := child + "-m0"
	providerName := child + "-kubevirt"
	return v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{
			{
				Metadata: v1alpha1.Metadata{Name: child},
				Spec: v1alpha1.ContainerClusterSpec{
					Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: machineName}}},
				},
			},
			{Metadata: v1alpha1.Metadata{Name: host}},
		},
		Machines: []v1alpha1.Machine{
			{
				Metadata: v1alpha1.Metadata{Name: machineName},
				Spec: v1alpha1.MachineSpec{
					Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: providerName}},
				},
			},
		},
		InfraProviders: []v1alpha1.InfraProvider{
			{
				Metadata: v1alpha1.Metadata{Name: providerName},
				Spec: v1alpha1.InfraProviderSpec{
					Type: v1alpha1.ProvisionerKubeVirt,
					KubeVirt: &v1alpha1.InfraProviderKubeVirt{
						HostClusterRef: &v1alpha1.LocalObjectReference{Name: host},
					},
				},
			},
		},
	}
}

func TestPlanDestroyTasksAvoidsPlacementCycleAcrossFannedMachineTeardown(t *testing.T) {
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata: v1alpha1.Metadata{Name: "env"},
			Spec: v1alpha1.EnvironmentSpec{InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{Proxies: []v1alpha1.EnvironmentProxyComponent{{
				Name: "proxy", Management: v1alpha1.EnvironmentComponentManaged, ComponentRef: v1alpha1.LocalObjectReference{Name: "proxy"},
			}}}},
		}},
		ContainerClusters: []v1alpha1.ContainerCluster{
			{Metadata: v1alpha1.Metadata{Name: "child-a"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "child-a-m0"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "child-b"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "child-b-m0"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "host"}, Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{MachineRef: v1alpha1.LocalObjectReference{Name: "host-m0"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "base"}},
		},
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "child-a-m0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "child-a-kubevirt"}, ProfileRef: v1alpha1.LocalObjectReference{Name: "node"}}}},
			{Metadata: v1alpha1.Metadata{Name: "child-b-m0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "child-b-kubevirt"}, ProfileRef: v1alpha1.LocalObjectReference{Name: "node"}}}},
			{Metadata: v1alpha1.Metadata{Name: "host-m0"}, Spec: v1alpha1.MachineSpec{Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "host-kubevirt"}, ProfileRef: v1alpha1.LocalObjectReference{Name: "node"}}}},
		},
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "child-a-kubevirt"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt, KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: "host"}, MachineProfiles: []v1alpha1.MachineProfile{{Name: "node"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "child-b-kubevirt"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt, KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: "host"}, MachineProfiles: []v1alpha1.MachineProfile{{Name: "node"}}}}},
			{Metadata: v1alpha1.Metadata{Name: "host-kubevirt"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt, KubeVirt: &v1alpha1.InfraProviderKubeVirt{HostClusterRef: &v1alpha1.LocalObjectReference{Name: "base"}, MachineProfiles: []v1alpha1.MachineProfile{{Name: "node"}}}}},
		},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "proxy"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotProxy,
				Proxy: &v1alpha1.ProxyComponent{
					Implementation: v1alpha1.InfraComponentTypeSquid,
					MachineRef:     v1alpha1.LocalObjectReference{Name: "child-a-m0"},
				},
			},
		}},
	}

	for _, scope := range []string{"infra", "all"} {
		t.Run(scope, func(t *testing.T) {
			tasks, err := PlanDestroyTasks(scope, state, "", nil, nil)
			if err != nil {
				t.Fatalf("plan valid placement graph: %v", err)
			}
			infra := destroyTaskByID(t, tasks, destroyInfraComponentsTaskID)
			if slices.Contains(infra.Entry.OrderingDependencies, destroyMachineInfraRecordsTaskID) {
				t.Fatalf("infra components depend on the aggregate records task and recreate a cycle: %v", infra.Entry.OrderingDependencies)
			}
			if !slices.Contains(infra.Entry.OrderingDependencies, "destroy.machine-infra.child-b") {
				t.Fatalf("infra components must still follow independent machine teardown: %v", infra.Entry.OrderingDependencies)
			}
			for _, id := range []string{"destroy.machine-infra.child-a", "destroy.machine-infra.host"} {
				if !slices.Contains(destroyTaskByID(t, tasks, id).Entry.OrderingDependencies, destroyInfraComponentsTaskID) {
					t.Fatalf("placement machine task %s must follow infra-component teardown", id)
				}
			}
			host := destroyTaskByID(t, tasks, "destroy.machine-infra.host")
			for _, child := range []string{"destroy.machine-infra.child-a", "destroy.machine-infra.child-b"} {
				if !slices.Contains(host.Entry.Dependencies, child) {
					t.Fatalf("host teardown hard dependencies = %v, want child-before-host dependency %s", host.Entry.Dependencies, child)
				}
			}
		})
	}
}

func TestPlanDestroyTasksStorageWorkSetGate(t *testing.T) {
	state := v1alpha1.State{
		StorageClusters: []v1alpha1.StorageCluster{
			{Metadata: v1alpha1.Metadata{Name: "ceph-render-ref"}},
			{Metadata: v1alpha1.Metadata{Name: "ceph-selected"}},
		},
	}

	containerOnly, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range containerOnly {
		if task.Entry.Kind == DestroyTaskKindStorageCluster {
			t.Fatalf("container-only selection must plan no storage teardown step; got %+v", task.Entry)
		}
		if task.Entry.Kind == DestroyTaskKindStorageNodeAccess {
			t.Fatalf("container-only selection must plan no storage node access revoke step; got %+v", task.Entry)
		}
	}

	narrowed, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{"ceph-selected"})
	if err != nil {
		t.Fatal(err)
	}
	var storageTask, nodeAccessTask *ApplyTask
	for i := range narrowed {
		switch narrowed[i].Entry.Kind {
		case DestroyTaskKindStorageCluster:
			storageTask = &narrowed[i]
		case DestroyTaskKindStorageNodeAccess:
			nodeAccessTask = &narrowed[i]
		}
	}
	if storageTask == nil {
		t.Fatal("storage-narrowed selection must plan a storage teardown step")
	}
	if len(storageTask.Entry.ResourceKeys) != 1 || storageTask.Entry.ResourceKeys[0] != "ceph-selected" {
		t.Fatalf("storage step must cover only the selected root; got %v", storageTask.Entry.ResourceKeys)
	}
	if nodeAccessTask == nil {
		t.Fatal("storage-narrowed selection must plan a storage node access revoke step")
	}
	if len(nodeAccessTask.Entry.ResourceKeys) != 1 || nodeAccessTask.Entry.ResourceKeys[0] != "ceph-selected" {
		t.Fatalf("storage node access revoke step must cover only the selected root; got %v", nodeAccessTask.Entry.ResourceKeys)
	}
	for _, pair := range storageTask.ExtraVarPairs {
		if strings.HasPrefix(pair, DestroyStorageScopeExtraVar+"=") {
			t.Fatalf("planner must not emit the storage-scope allowlist (composed centrally); got %q", pair)
		}
	}

	unscoped, err := PlanDestroyTasks("clusters", state, "limit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var covered []string
	for _, task := range unscoped {
		if task.Entry.Kind != DestroyTaskKindStorageCluster {
			continue
		}
		covered = append(covered, task.Entry.ResourceKeys...)
	}
	sort.Strings(covered)
	if !reflect.DeepEqual(covered, []string{"ceph-render-ref", "ceph-selected"}) {
		t.Fatalf("unscoped destroy must tear down every storage cluster; covered %v", covered)
	}
}

func destroyStorageFanOutState(clusters map[string][]string) v1alpha1.State {
	state := v1alpha1.State{}
	names := make([]string, 0, len(clusters))
	for name := range clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		nodes := make([]v1alpha1.StorageCephNode, 0, len(clusters[name]))
		bootstrapNode := ""
		if len(clusters[name]) > 0 {
			bootstrapNode = clusters[name][0]
		}
		for _, machine := range clusters[name] {
			nodes = append(nodes, v1alpha1.StorageCephNode{
				Name:       machine,
				MachineRef: v1alpha1.LocalObjectReference{Name: machine},
			})
			if seen[machine] {
				continue
			}
			seen[machine] = true
			state.Machines = append(state.Machines, v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: machine}})
		}
		state.StorageClusters = append(state.StorageClusters, v1alpha1.StorageCluster{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.StorageClusterSpec{
				Type:       v1alpha1.StorageClusterTypeCeph,
				Management: v1alpha1.StorageClusterManagementManaged,
				Ceph: &v1alpha1.StorageClusterCephSpec{
					Cephadm:  v1alpha1.StorageCephadmSpec{Bootstrap: v1alpha1.StorageCephadmBootstrap{Node: bootstrapNode}},
					Topology: v1alpha1.StorageCephTopology{Nodes: nodes},
				},
			},
		})
	}
	return state
}

func TestPlanDestroyTasksKeepsOneTaskForASingleStorageCluster(t *testing.T) {
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0", "a1"}})
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	storage := destroyTaskByID(t, tasks, DestroyStorageClustersTaskID)
	if storage.Limit != render.GroupStorageHosts {
		t.Fatalf("single-cluster storage teardown limit = %q, want the proven whole-group limit %q", storage.Limit, render.GroupStorageHosts)
	}
	if !reflect.DeepEqual(storage.Entry.ResourceKeys, []string{"ceph-a"}) {
		t.Fatalf("single-cluster storage teardown keys = %v, want just the cluster; per-machine keys only serialise concurrent per-cluster tasks", storage.Entry.ResourceKeys)
	}
}

func TestPlanDestroyTasksNeverFansOutOntoAnUnrenderedInventoryGroup(t *testing.T) {
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0"}, "ceph-external": {"x0"}})
	for i := range state.StorageClusters {
		if state.StorageClusters[i].Metadata.Name == "ceph-external" {
			state.StorageClusters[i].Spec.Management = v1alpha1.StorageClusterManagementExternal
		}
	}
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	storage := destroyTaskByID(t, tasks, DestroyStorageClustersTaskID)
	if storage.Limit != render.GroupStorageHosts {
		t.Fatalf("storage teardown limit = %q; only one cluster renders an inventory group, and ansible aborts the whole run when --limit names a group the inventory never emitted", storage.Limit)
	}
	if !reflect.DeepEqual(storage.Entry.ResourceKeys, []string{"ceph-a"}) {
		t.Fatalf("storage teardown completion keys = %v, want only managed cluster ceph-a", storage.Entry.ResourceKeys)
	}
	for _, id := range []string{destroyMachineRegistrationTaskID, destroyStorageNodeAccessTaskID, destroyMachineInfraTaskID, destroyControllerNameResolutionCleanupTaskID} {
		task := destroyTaskByID(t, tasks, id)
		if !slices.Contains(task.Entry.SuccessDependencies, DestroyStorageClustersTaskID) {
			t.Errorf("managed storage in the collapsed task must keep the positive-proof edge from %s, got %v", id, task.Entry.SuccessDependencies)
		}
	}
	for _, task := range tasks {
		if strings.HasPrefix(task.Entry.ID, DestroyStorageClustersTaskID+".") {
			t.Fatalf("task %q limits itself to %q, but external storage clusters have no rendered host group", task.Entry.ID, task.Limit)
		}
	}
}

func TestPlanDestroyTasksDoesNotRequireManagedStorageProofFromExternalCluster(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{{
		Metadata: v1alpha1.Metadata{Name: "ceph-external"},
		Spec: v1alpha1.StorageClusterSpec{
			Type:       v1alpha1.StorageClusterTypeCeph,
			Management: v1alpha1.StorageClusterManagementExternal,
		},
	}}}
	for _, scope := range []string{"clusters", "all"} {
		t.Run(scope, func(t *testing.T) {
			tasks, err := PlanDestroyTasks(scope, state, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			storage := destroyTaskByID(t, tasks, DestroyStorageClustersTaskID)
			if len(storage.Entry.ResourceKeys) != 0 {
				t.Fatalf("external storage task completion keys = %v, want none", storage.Entry.ResourceKeys)
			}
			for _, task := range tasks {
				if slices.Contains(task.Entry.SuccessDependencies, DestroyStorageClustersTaskID) {
					t.Errorf("task %s requires success from an external storage no-host task: %v", task.Entry.ID, task.Entry.SuccessDependencies)
				}
			}
		})
	}
}

func TestPlanDestroyTasksFallsBackWholesaleWhenAnyClusterHasNoHostGroup(t *testing.T) {
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0"}, "ceph-b": {"b0"}, "ceph-empty": {}})
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range tasks {
		if task.Entry.Kind == DestroyTaskKindStorageCluster {
			ids = append(ids, task.Entry.ID)
		}
	}
	if !reflect.DeepEqual(ids, []string{DestroyStorageClustersTaskID}) {
		t.Fatalf("storage teardown tasks = %v; when any selected cluster lacks a rendered host group the plan must fall back to the single proven whole-group task rather than drop that cluster or limit onto a group that does not exist", ids)
	}
	if got := destroyTaskByID(t, tasks, DestroyStorageClustersTaskID).Entry.ResourceKeys; !reflect.DeepEqual(got, []string{"ceph-a", "ceph-b", "ceph-empty"}) {
		t.Fatalf("the fallback task must still claim every cluster it tears down, got %v", got)
	}
}

func TestPlanDestroyTasksFansOutIndependentStorageClusters(t *testing.T) {
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0", "a1"}, "ceph-b": {"b0"}})
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"destroy.controller-name-resolution-preflight",
		"destroy.cluster-runtime",
		"destroy.storage-clusters.ceph-a",
		"destroy.storage-clusters.ceph-b",
		"destroy.machine-registration.ceph-a",
		"destroy.machine-registration.ceph-b",
		"destroy.storage-node-access.ceph-a",
		"destroy.storage-node-access.ceph-b",
		"destroy.machine-infra",
		"destroy.container-clusters",
		"destroy.infra-components",
		"destroy.provider-services",
		"destroy.controller-name-resolution-cleanup",
	}
	if got := destroyTaskIDs(tasks); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("fanned destroy plan = %v, want %v", got, wantIDs)
	}
	assertDestroyOrderingEdges(t, tasks, map[string][]string{
		"destroy.controller-name-resolution-preflight": nil,
		"destroy.cluster-runtime":                      nil,
		"destroy.storage-clusters.ceph-a":              nil,
		"destroy.storage-clusters.ceph-b":              nil,
		"destroy.machine-registration.ceph-a":          nil,
		"destroy.machine-registration.ceph-b":          nil,
		"destroy.storage-node-access.ceph-a":           nil,
		"destroy.storage-node-access.ceph-b":           nil,
		"destroy.machine-infra": {
			"destroy.cluster-runtime",
			"destroy.storage-node-access.ceph-a",
			"destroy.storage-node-access.ceph-b",
			"destroy.machine-registration.ceph-a",
			"destroy.machine-registration.ceph-b",
		},
		"destroy.container-clusters": {
			"destroy.cluster-runtime",
			"destroy.storage-clusters.ceph-a",
			"destroy.storage-clusters.ceph-b",
		},
		"destroy.infra-components": {
			"destroy.machine-infra",
			"destroy.storage-node-access.ceph-a",
			"destroy.storage-node-access.ceph-b",
			"destroy.machine-registration.ceph-a",
			"destroy.machine-registration.ceph-b",
		},
		"destroy.provider-services":                  {"destroy.machine-infra", "destroy.container-clusters", "destroy.infra-components"},
		"destroy.controller-name-resolution-cleanup": nil,
	})
	for _, cluster := range []string{"ceph-a", "ceph-b"} {
		other := map[string]string{"ceph-a": "ceph-b", "ceph-b": "ceph-a"}[cluster]
		got := destroyTaskByID(t, tasks, "destroy.storage-node-access."+cluster).Entry.SuccessDependencies
		if slices.Contains(got, "destroy.machine-registration."+other) {
			t.Fatalf("per-cluster storage proof edges must not cross clusters, or one cluster's failure blocks an unrelated one: %v", got)
		}
	}
	if got := destroyTaskByID(t, tasks, "destroy.container-clusters").Entry.Dependencies; !reflect.DeepEqual(got, []string{"destroy.machine-infra"}) {
		t.Fatalf("fanning storage out must keep the container-cluster hard dependency on machine teardown, got %v", got)
	}
	for _, id := range []string{"destroy.storage-clusters.ceph-a", "destroy.storage-node-access.ceph-a"} {
		task := destroyTaskByID(t, tasks, id)
		if task.Limit != render.StorageClusterGroupName("ceph-a") {
			t.Fatalf("task %q limit = %q, want its own cluster group so the play only reaches that cluster's nodes", id, task.Limit)
		}
		want := []string{"ceph-a", DestroyMachineResourceKeyPrefix + "a0", DestroyMachineResourceKeyPrefix + "a1"}
		if !reflect.DeepEqual(task.Entry.ResourceKeys, want) {
			t.Fatalf("task %q resource keys = %v, want %v", id, task.Entry.ResourceKeys, want)
		}
	}
}

func TestPlanDestroyTasksSerialisesStorageClustersSharingANode(t *testing.T) {
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0", "shared"}, "ceph-b": {"shared", "b1"}})
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := destroyTaskByID(t, tasks, "destroy.storage-clusters.ceph-a")
	b := destroyTaskByID(t, tasks, "destroy.storage-clusters.ceph-b")
	shared := DestroyMachineResourceKeyPrefix + "shared"
	if !slices.Contains(a.Entry.ResourceKeys, shared) || !slices.Contains(b.Entry.ResourceKeys, shared) {
		t.Fatalf("both teardowns of the shared node must claim %q or two rm-cluster runs race on one host; got %v and %v", shared, a.Entry.ResourceKeys, b.Entry.ResourceKeys)
	}
	if busy := busyTaskResourceKey(b, map[string]int{shared: 1}); busy != shared {
		t.Fatalf("scheduler must hold %q back while the sibling cluster's teardown holds the shared node, got %q", shared, busy)
	}
}

func TestPlanDestroyTasksFansOutOnlyTheSelectedStorageClusters(t *testing.T) {
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0"}, "ceph-b": {"b0"}, "ceph-c": {"c0"}})
	tasks, err := PlanDestroyTasks("clusters", state, "limit", nil, []string{"ceph-a", "ceph-c"})
	if err != nil {
		t.Fatal(err)
	}
	var storage []string
	for _, task := range tasks {
		if task.Entry.Kind == DestroyTaskKindStorageCluster {
			storage = append(storage, task.Entry.ID)
		}
	}
	want := []string{"destroy.storage-clusters.ceph-a", "destroy.storage-clusters.ceph-c"}
	if !reflect.DeepEqual(storage, want) {
		t.Fatalf("fan-out must stay inside the selected work set, got %v want %v", storage, want)
	}
}

func TestSucceededDestroyTaskKindsScopesSuccessToItsOwnCluster(t *testing.T) {
	outcome := SucceededDestroyTaskKinds(RunLedger{Tasks: []TaskLedgerEntry{
		{
			ID:           "destroy.storage-clusters.ceph-a",
			Kind:         DestroyTaskKindStorageCluster,
			ResourceKeys: []string{"ceph-a", DestroyMachineResourceKeyPrefix + "a0"},
			Status:       TaskStatusOK,
		},
		{
			ID:           "destroy.storage-clusters.ceph-b",
			Kind:         DestroyTaskKindStorageCluster,
			ResourceKeys: []string{"ceph-b"},
			Status:       TaskStatusFailed,
		},
	}})
	if !outcome.Covers(DestroyTaskKindStorageCluster, "ceph-a") {
		t.Fatal("the cluster whose teardown succeeded must be covered")
	}
	if outcome.Covers(DestroyTaskKindStorageCluster, "ceph-b") {
		t.Fatal("ceph-b's teardown failed: rolling success up by task kind authorizes a cluster that was never wiped")
	}
	if outcome[DestroyTaskKindStorageCluster] {
		t.Fatal("a kind is only fleet-wide successful when every task of that kind succeeded")
	}
	if outcome.Covers(DestroyTaskKindStorageCluster, DestroyMachineResourceKeyPrefix+"a0") {
		t.Fatal("per-machine serialisation keys are not cluster names and must never authorize a cluster")
	}
	if !outcome.Attempted(DestroyTaskKindStorageCluster, "ceph-b") {
		t.Fatal("ceph-b's teardown ran, so callers must be able to tell an attempted-and-failed cluster from one never in scope")
	}
	if outcome.Attempted(DestroyTaskKindMachineInfra, "ceph-a") {
		t.Fatal("no machine teardown ran in this ledger")
	}
}

func TestControllerNameResolutionDestroyOutcomeRequiresBothBracketTasks(t *testing.T) {
	cases := []struct {
		name            string
		preflightStatus TaskStatus
		cleanupStatus   TaskStatus
		wantCovered     bool
	}{
		{name: "both succeeded", preflightStatus: TaskStatusOK, cleanupStatus: TaskStatusOK, wantCovered: true},
		{name: "preflight failed", preflightStatus: TaskStatusFailed, cleanupStatus: TaskStatusBlocked},
		{name: "cleanup failed", preflightStatus: TaskStatusOK, cleanupStatus: TaskStatusFailed},
		{name: "cleanup skipped", preflightStatus: TaskStatusOK, cleanupStatus: TaskStatusSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome := SucceededDestroyTaskKinds(RunLedger{Tasks: []TaskLedgerEntry{
				{ID: destroyControllerNameResolutionPreflightTaskID, Kind: DestroyTaskKindControllerNameResolution, Status: tc.preflightStatus},
				{ID: destroyControllerNameResolutionCleanupTaskID, Kind: DestroyTaskKindControllerNameResolution, Status: tc.cleanupStatus},
			}})
			if !outcome.Attempted(DestroyTaskKindControllerNameResolution, "") {
				t.Fatal("controller name-resolution destroy outcome did not record an attempted bracket")
			}
			if got := outcome.Covers(DestroyTaskKindControllerNameResolution, ""); got != tc.wantCovered {
				t.Fatalf("controller name-resolution destroy covered = %t, want %t for preflight=%s cleanup=%s", got, tc.wantCovered, tc.preflightStatus, tc.cleanupStatus)
			}
		})
	}
}

func TestDestroyTaskNeedsCompletionProofUsesRegisteredDestroyKinds(t *testing.T) {
	cases := []struct {
		name string
		task TaskLedgerEntry
		want bool
	}{
		{name: "empty"},
		{name: "full scope provider kind", task: TaskLedgerEntry{Kind: DestroyTaskKindProviderServices}, want: true},
		{name: "unknown kind", task: TaskLedgerEntry{Kind: "futureDestroyKind"}},
		{name: "unknown kind with cluster resource", task: TaskLedgerEntry{Kind: "futureDestroyKind", ResourceKeys: []string{"cluster-a"}}},
		{name: "registered container kind without selected identity", task: TaskLedgerEntry{Kind: DestroyTaskKindContainerCluster}, want: true},
		{name: "registered machine kind", task: TaskLedgerEntry{Kind: DestroyTaskKindMachineInfra}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DestroyTaskNeedsCompletionProof(tc.task); got != tc.want {
				t.Fatalf("DestroyTaskNeedsCompletionProof(%+v) = %t, want %t", tc.task, got, tc.want)
			}
		})
	}
}

func TestSucceededDestroyTaskKindsMarksKindsWhoseTasksAllSucceeded(t *testing.T) {
	outcome := SucceededDestroyTaskKinds(RunLedger{Tasks: []TaskLedgerEntry{
		{ID: "destroy.storage-clusters.ceph-a", Kind: DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph-a"}, Status: TaskStatusOK},
		{ID: "destroy.storage-clusters.ceph-b", Kind: DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph-b"}, Status: TaskStatusOK},
		{ID: "destroy.machine-infra", Kind: DestroyTaskKindMachineInfra, Status: TaskStatusFailed},
	}})
	if !outcome[DestroyTaskKindStorageCluster] {
		t.Fatal("every storage teardown task succeeded, so the kind is covered fleet-wide")
	}
	if outcome.Covers(DestroyTaskKindMachineInfra, "ceph-a") {
		t.Fatal("the machine teardown failed and must not be covered for any cluster")
	}
}

func TestDestroyOutcomeNilMeansEverythingSucceeded(t *testing.T) {
	var outcome DestroyOutcome
	if !outcome.Covers(DestroyTaskKindStorageCluster, "ceph-a") {
		t.Fatal("a nil outcome is the fully successful run and covers every cluster")
	}
	if outcome.Attempted(DestroyTaskKindStorageCluster, "ceph-a") {
		t.Fatal("a nil outcome carries no evidence that a step was attempted")
	}
}

func TestDestroyOutcomeFullySucceededRequiresEveryAttemptedTask(t *testing.T) {
	complete := SucceededDestroyTaskKinds(RunLedger{Tasks: []TaskLedgerEntry{
		{ID: "destroy.storage", Kind: DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph"}, Status: TaskStatusOK},
		{ID: "destroy.machine", Kind: DestroyTaskKindMachineInfra, ResourceKeys: []string{"ceph"}, Status: TaskStatusOK},
	}})
	if !DestroyOutcomeFullySucceeded(complete) {
		t.Fatal("an outcome derived from an all-OK ledger must prove full success")
	}
	incomplete := SucceededDestroyTaskKinds(RunLedger{Tasks: []TaskLedgerEntry{
		{ID: "destroy.storage", Kind: DestroyTaskKindStorageCluster, ResourceKeys: []string{"ceph"}, Status: TaskStatusOK},
		{ID: "destroy.machine", Kind: DestroyTaskKindMachineInfra, ResourceKeys: []string{"ceph"}, Status: TaskStatusSkipped},
	}})
	if DestroyOutcomeFullySucceeded(incomplete) {
		t.Fatal("a skipped attempted task must withhold full-success cleanup")
	}
	if DestroyOutcomeFullySucceeded(DestroyOutcome{DestroyTaskKindStorageCluster: true}) {
		t.Fatal("a hand-built partial outcome with no attempted evidence must not authorize a full-context sweep")
	}
}

func TestDestroyChainSetsForksForEveryStep(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	members := render.HostGroupMembers(state)
	hostCount := func(groups ...string) int {
		set := map[string]bool{}
		for _, group := range groups {
			for _, host := range members[group] {
				set[host] = true
			}
		}
		return len(set)
	}
	bounded := func(count int) int {
		if count < 1 {
			return 1
		}
		if count > destroyMaxForks {
			return destroyMaxForks
		}
		return count
	}
	inventory := hostCount(
		render.GroupStorageHosts, render.GroupInfraComponentHosts, render.GroupProviderHosts,
		render.GroupOCPHosts, render.GroupInfraHosts, render.GroupMachineTaskHosts,
		render.GroupBootHosts, render.GroupControllerHosts,
	)
	storageHosts := hostCount(render.GroupStorageHosts)
	if storageHosts < 2 || storageHosts >= inventory {
		t.Fatalf("fixture must have several storage hosts and a strictly larger inventory to prove the fork limit is per step; storage=%d inventory=%d", storageHosts, inventory)
	}
	want := map[string]int{
		"destroy.controller-name-resolution-preflight": bounded(hostCount(render.GroupControllerHosts)),
		"destroy.controller-name-resolution-cleanup":   bounded(hostCount(render.GroupControllerHosts)),
		"destroy.storage-clusters":                     bounded(storageHosts),
		"destroy.machine-registration":                 bounded(storageHosts),
		"destroy.storage-node-access":                  bounded(storageHosts),
		"destroy.infra-components":                     bounded(hostCount(render.GroupInfraComponentHosts)),
		"destroy.provider-services":                    bounded(hostCount(render.GroupProviderHosts)),
		"destroy.machine-infra":                        bounded(hostCount(render.GroupMachineTaskHosts, render.GroupProviderHosts, render.GroupInfraHosts)),
	}
	ocpForks := bounded(hostCount(render.GroupOCPHosts))
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Forks < 1 {
			t.Fatalf("task %q runs at the Ansible built-in default of 5 forks because the planner left Forks unset", task.Entry.ID)
		}
		if task.Forks > destroyMaxForks {
			t.Fatalf("task %q forks = %d, want at most %d", task.Entry.ID, task.Forks, destroyMaxForks)
		}
		switch {
		case task.Entry.Kind == DestroyTaskKindContainerCluster || task.Entry.Kind == DestroyTaskKindContainerClusterRuntime:
			if task.Forks != ocpForks {
				t.Fatalf("task %q forks = %d, want %d (one worker per OCP host)", task.Entry.ID, task.Forks, ocpForks)
			}
			continue
		case task.Entry.ID == "destroy.machine-infra-records":
			if got, expected := task.Forks, bounded(hostCount(render.GroupProviderHosts)); got != expected {
				t.Fatalf("records sweep forks = %d, want %d; it targets only the provider and infra hosts, never the machine task hosts", got, expected)
			}
			continue
		case strings.HasPrefix(task.Entry.ID, "destroy.machine-infra."):
			cluster := strings.TrimPrefix(task.Entry.ID, "destroy.machine-infra.")
			if got, expected := task.Forks, bounded(hostCount(destroyMachineInfraClusterGroup(state, cluster))); got != expected {
				t.Fatalf("task %q forks = %d, want %d (one worker per machine task host of its own cluster)", task.Entry.ID, got, expected)
			}
			continue
		}
		expected, ok := want[task.Entry.ID]
		if !ok {
			t.Fatalf("task %q has no expected fork count; every planned step must claim one", task.Entry.ID)
		}
		if task.Forks != expected {
			t.Fatalf("task %q forks = %d, want %d (one worker per host the step's own play targets)", task.Entry.ID, task.Forks, expected)
		}
	}
	for _, id := range []string{"destroy.storage-clusters", "destroy.machine-registration", "destroy.storage-node-access"} {
		if got := destroyTaskByID(t, tasks, id).Forks; got >= inventory {
			t.Fatalf("task %q forks = %d, want the %d storage hosts rather than the whole %d-host inventory", id, got, storageHosts, inventory)
		}
	}
}

func TestPlanDestroyTasksBlocksMachineTeardownOnFailedStorageTeardown(t *testing.T) {
	provided := false
	state := destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0", "a1"}, "ceph-b": {"b0"}})
	for i := range state.Machines {
		state.Machines[i].Spec.OS.Provided = &provided
		state.Machines[i].Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{Name: "profile"}
	}
	tasks, err := PlanDestroyTasks("all", state, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range []string{"ceph-a", "ceph-b"} {
		task := destroyTaskByID(t, tasks, "destroy.machine-infra."+cluster)
		own := "destroy.storage-clusters." + cluster
		if !slices.Contains(task.Entry.SuccessDependencies, own) {
			t.Errorf("%q releases the machines of storage cluster %q and must require successful proof from %q: a skip-tolerant or ordering-only edge lets an incomplete storage teardown release the machines anyway; got success deps=%v", task.Entry.ID, cluster, own, task.Entry.SuccessDependencies)
		}
		other := map[string]string{"ceph-a": "ceph-b", "ceph-b": "ceph-a"}[cluster]
		if slices.Contains(task.Entry.SuccessDependencies, "destroy.storage-clusters."+other) {
			t.Errorf("%q must not be success-gated by an unrelated cluster's storage teardown, got success deps=%v", task.Entry.ID, task.Entry.SuccessDependencies)
		}
		registration := destroyTaskByID(t, tasks, "destroy.machine-registration."+cluster)
		if !reflect.DeepEqual(registration.Entry.SuccessDependencies, []string{own, destroyControllerNameResolutionPreflightTaskID}) {
			t.Errorf("%q success deps=%v, want its own storage proof and controller preflight", registration.Entry.ID, registration.Entry.SuccessDependencies)
		}
		access := destroyTaskByID(t, tasks, "destroy.storage-node-access."+cluster)
		if !reflect.DeepEqual(access.Entry.SuccessDependencies, []string{own, registration.Entry.ID, destroyControllerNameResolutionPreflightTaskID}) {
			t.Errorf("%q success deps=%v, want its own storage proof, registration, and controller preflight", access.Entry.ID, access.Entry.SuccessDependencies)
		}
	}

	collapsed, err := PlanDestroyTasks("all", destroyStorageFanOutState(map[string][]string{"ceph-a": {"a0"}}), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	machineInfra := destroyTaskByID(t, collapsed, "destroy.machine-infra")
	if !slices.Contains(machineInfra.Entry.SuccessDependencies, DestroyStorageClustersTaskID) {
		t.Errorf("the collapsed machine teardown covers the storage cluster's machines and must require successful storage proof, got success deps=%v", machineInfra.Entry.SuccessDependencies)
	}
	registration := destroyTaskByID(t, collapsed, destroyMachineRegistrationTaskID)
	if !slices.Contains(registration.Entry.SuccessDependencies, DestroyStorageClustersTaskID) {
		t.Errorf("collapsed registration can erase retry state without storage proof, success deps=%v", registration.Entry.SuccessDependencies)
	}
	access := destroyTaskByID(t, collapsed, destroyStorageNodeAccessTaskID)
	if !slices.Contains(access.Entry.SuccessDependencies, DestroyStorageClustersTaskID) || !slices.Contains(access.Entry.SuccessDependencies, destroyMachineRegistrationTaskID) {
		t.Errorf("collapsed access can revoke retry identity before storage and registration complete, success deps=%v", access.Entry.SuccessDependencies)
	}
	cleanup := destroyTaskByID(t, collapsed, destroyControllerNameResolutionCleanupTaskID)
	if !slices.Contains(cleanup.Entry.SuccessDependencies, DestroyStorageClustersTaskID) {
		t.Errorf("controller resolver cleanup can remove a route required by the storage retry, success deps=%v", cleanup.Entry.SuccessDependencies)
	}
}

func TestDestroyChainClampsForksOnWideGroups(t *testing.T) {
	hosts := make([]string, 0, destroyMaxForks*2)
	for i := 0; i < destroyMaxForks*2; i++ {
		hosts = append(hosts, "node"+strings.Repeat("x", i%3)+string(rune('a'+i)))
	}
	step := destroyStep{id: "destroy.wide", forksLimit: strings.Join(hosts, ":")}
	if got := destroyStepForks(v1alpha1.State{}, step, ""); got != destroyMaxForks {
		t.Fatalf("wide destroy step forks = %d, want the %d clamp so a large environment cannot fork-bomb the controller", got, destroyMaxForks)
	}
	empty := destroyStep{id: "destroy.empty", forksLimit: render.GroupStorageHosts}
	if got := destroyStepForks(v1alpha1.State{}, empty, ""); got != 1 {
		t.Fatalf("empty group forks = %d, want 1", got)
	}
}
