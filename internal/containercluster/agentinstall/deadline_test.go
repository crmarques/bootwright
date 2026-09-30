package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// operations are every entry point a cluster block's role offers, and runs
// every call that runs one, each under the one deadline its frozen request
// derives: a removal's resolution runs the observe entry point.
var (
	operations = []string{"apply", "observe", "destroy"}
	runs       = []string{"apply", "observe", "observe-removal", "destroy"}
)

// mediaRunDeadline is the deadline one operation's run states for a frozen
// media request.
func mediaRunDeadline(t *testing.T, request MediaRequest, operation string) time.Duration {
	t.Helper()
	execution, _ := mediaExecution(t, testDigest)
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	execution.Block.Request = canonical
	// The run itself is not under test: only the request it was handed.
	runner := &fakeRunner{err: errors.New("stopped")}
	capability := NewMedia(runner)
	switch operation {
	case "apply":
		_, _ = capability.Apply(context.Background(), execution)
	case "destroy":
		_, _ = capability.Destroy(context.Background(), execution)
	case "observe":
		_, _ = capability.Observe(context.Background(), execution)
	case "observe-removal":
		_, _ = capability.ObserveRemoval(context.Background(), execution)
	default:
		t.Fatalf("no run measures the %s operation", operation)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("media %s ran the adapter %d times", operation, len(runner.requests))
	}
	return runner.requests[0].Deadline
}

// installRunDeadline is the deadline one operation's run states for a frozen
// install request.
func installRunDeadline(t *testing.T, request InstallRequest, operation string) time.Duration {
	t.Helper()
	execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	execution.Block.Request = canonical
	for _, reference := range request.SecretReferences() {
		if _, ok := execution.Material[reference]; !ok {
			t.Fatalf("the execution binds no %s", reference)
		}
	}
	runner := &fakeRunner{err: errors.New("stopped")}
	capability := NewInstall(runner)
	switch operation {
	case "apply":
		_, _ = capability.Apply(context.Background(), execution)
	case "destroy":
		_, _ = capability.Destroy(context.Background(), execution)
	case "observe":
		_, _ = capability.Observe(context.Background(), execution)
	case "observe-removal":
		_, _ = capability.ObserveRemoval(context.Background(), execution)
	default:
		t.Fatalf("no run measures the %s operation", operation)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("install %s ran the adapter %d times", operation, len(runner.requests))
	}
	return runner.requests[0].Deadline
}

// Every run of both cluster blocks is bounded by a deadline derived from the
// budgets its frozen request carries. It covers every phase those budgets
// bound with room for the work around them, so the runner never kills a block
// still inside one of its phases, and it stays within the ceiling the runner
// holds every request to, so the runner never cuts it short.
func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	for name, catalog := range map[string]api.Catalog{
		"lab-sno": singleNodeCatalog(), "compact": compactCatalog(), "external": externalCatalog(),
	} {
		media, install, _ := onlyRequests(t, catalog)
		if media.Budgets.BuildSeconds <= 0 {
			t.Fatalf("%s freezes a build budget of %d seconds, which bounds nothing", name, media.Budgets.BuildSeconds)
		}
		build := time.Duration(media.Budgets.BuildSeconds) * time.Second
		phases := time.Duration(0)
		for phase, budget := range map[string]int{
			"boot": install.Budgets.BootSeconds, "bootstrap": install.Budgets.BootstrapSeconds,
			"install": install.Budgets.InstallSeconds,
		} {
			if budget <= 0 {
				t.Fatalf("%s freezes a %s budget of %d seconds, which bounds nothing", name, phase, budget)
			}
			phases += time.Duration(budget) * time.Second
		}
		for _, operation := range runs {
			if deadline := mediaRunDeadline(t, media, operation); deadline <= build || deadline > lifecycle.MaxDeadline {
				t.Errorf("%s media %s runs under %s, which must exceed the %s build budget and stay within the %s ceiling",
					name, operation, deadline, build, lifecycle.MaxDeadline)
			}
			if deadline := installRunDeadline(t, install, operation); deadline <= phases || deadline > lifecycle.MaxDeadline {
				t.Errorf("%s install %s runs under %s, which must exceed the %s its phases are budgeted and stay within the %s ceiling",
					name, operation, deadline, phases, lifecycle.MaxDeadline)
			}
		}
	}
}

// The deadline follows every budget the request froze and every node it
// boots, not this build's own. The budgets differ from this build's and from
// each other, and each is longer than the margins, so a term the derivation
// dropped, or took from this build instead, cannot hide inside a margin; and
// each field moved alone must move the deadline by exactly what it adds.
func TestTheDeadlineFollowsTheBudgetsTheRequestFroze(t *testing.T) {
	media, install, _ := onlyRequests(t, compactCatalog())
	media.Budgets = MediaBudgets{BuildSeconds: 7919}
	for name, field := range map[string]func(*MediaBudgets) *int{
		"nothing":      nil,
		"buildSeconds": func(b *MediaBudgets) *int { return &b.BuildSeconds },
	} {
		frozen := media
		if field != nil {
			*field(&frozen.Budgets) += 7
		}
		want := time.Duration(frozen.Budgets.BuildSeconds)*time.Second + mediaMargin
		for _, operation := range runs {
			if got := mediaRunDeadline(t, frozen, operation); got != want {
				t.Errorf("media %s with %s moved runs under %s, want %s for %+v", operation, name, got, want, frozen.Budgets)
			}
		}
	}
	install.Budgets = InstallBudgets{BootSeconds: 4111, BootstrapSeconds: 6007, InstallSeconds: 8123}
	for name, field := range map[string]func(*InstallBudgets) *int{
		"nothing":          nil,
		"bootSeconds":      func(b *InstallBudgets) *int { return &b.BootSeconds },
		"bootstrapSeconds": func(b *InstallBudgets) *int { return &b.BootstrapSeconds },
		"installSeconds":   func(b *InstallBudgets) *int { return &b.InstallSeconds },
	} {
		frozen := install
		if field != nil {
			*field(&frozen.Budgets) += 7
		}
		seconds := frozen.Budgets.BootSeconds + frozen.Budgets.BootstrapSeconds + frozen.Budgets.InstallSeconds
		want := time.Duration(seconds)*time.Second + installMargin + 3*nodeMargin
		for _, operation := range runs {
			if got := installRunDeadline(t, frozen, operation); got != want {
				t.Errorf("install %s with %s moved runs under %s, want %s for %+v", operation, name, got, want, frozen.Budgets)
			}
		}
	}
	one := install
	one.Nodes = one.Nodes[:1]
	if got, want := installRunDeadline(t, one, "apply"), installRunDeadline(t, install, "apply")-2*nodeMargin; got != want {
		t.Errorf("an installation of one node runs under %s, want %s: each node adds %s", got, want, nodeMargin)
	}
}

// collectionRoles is where the embedded collection keeps every role, and
// installRole the one each install entry point runs.
const (
	collectionRoles = "collections/ansible_collections/bootwright/core/roles/"
	installRole     = "containercluster_install_agent"
)

// roleTasks is one task file of a role in the embedded collection.
func roleTasks(t *testing.T, role, file string) []map[string]any {
	t.Helper()
	path := collectionRoles + role + "/tasks/" + file
	data, ok := ansible.Assets()[path]
	if !ok {
		t.Fatalf("the embedded collection carries no %s", path)
	}
	var tasks []map[string]any
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return tasks
}

// controllerCall is one call a task makes to a node's management controller,
// and the bound the substrate states for it.
type controllerCall struct {
	name  string
	bound time.Duration
}

// controllerCallOf is the controller call one task makes, if it makes one. A
// call whose bound the substrate does not state, because it polls more than
// the default, repeats itself or settles the device's trust, fails the test.
func controllerCallOf(t *testing.T, where string, task map[string]any) (controllerCall, bool) {
	t.Helper()
	call, found := controllerCall{}, false
	if arguments, ok := task["bootwright.core.redfish_system_read"].(map[string]any); ok {
		call, found = controllerCall{"power read", substrate.ControllerPowerReadBound}, true
		if arguments["media"] == true {
			call = controllerCall{"media read", substrate.ControllerMediaReadBound}
		}
	}
	if arguments, ok := task["bootwright.core.redfish_boot"].(map[string]any); ok {
		operation := fmt.Sprint(arguments["operation"])
		bound, known := map[string]time.Duration{
			"insert": substrate.ControllerInsertBound, "eject": substrate.ControllerEjectBound,
			"boot": substrate.ControllerBootSelectionBound, "power-on": substrate.ControllerPowerBound,
			"power-off": substrate.ControllerPowerBound, "shutdown": substrate.ControllerPowerBound,
		}[operation]
		for _, unbounded := range []string{"attempts", "remove_certificate", "restore_verification"} {
			if _, set := arguments[unbounded]; set {
				known = false
			}
		}
		if !known {
			t.Fatalf("%s: redfish_boot %s %v takes no bound the substrate states", where, operation, arguments)
		}
		call, found = controllerCall{operation, bound}, true
	}
	for key := range task {
		if !found && strings.HasPrefix(key, "bootwright.core.redfish") {
			t.Fatalf("%s: %s takes no bound the substrate states", where, key)
		}
	}
	for _, repeats := range []string{"until", "retries"} {
		if _, repeated := task[repeats]; found && repeated {
			t.Fatalf("%s: a repeated %s takes no bound the substrate states", where, call.name)
		}
	}
	return call, found
}

// callScope is what a task inherits from the tasks that run it: whether it runs
// once for each node, whether a loop other than the one over the nodes repeats
// it, the substrate its dispatch binds, and the variables it is handed. A task
// a repeating loop reaches is repeated whatever loops run inside it.
type callScope struct {
	perNode   bool
	repeated  bool
	substrate string
	vars      map[string]any
}

var (
	substrateCondition = regexp.MustCompile(`^containercluster_install_agent_node\.substrate == '([a-z]+)'$`)
	switchCondition    = regexp.MustCompile(`^([a-z_]+) \| bool$`)
)

// nodeLoop is the one loop that runs a task once for each node.
const nodeLoop = "{{ bootwright_cluster_install_request.nodes }}"

// builtinAction is the builtin action a task key names, however it is spelled:
// ansible-core reads a bare name and its ansible.builtin and ansible.legacy
// names as one action (utils/fqcn.py, add_internal_fqcns).
func builtinAction(key string) string {
	for _, prefix := range []string{"ansible.builtin.", "ansible.legacy."} {
		if action, ok := strings.CutPrefix(key, prefix); ok {
			return action
		}
	}
	return key
}

// includedFile is the task file an import_tasks or include_tasks names, given
// alone or as the file among its arguments (playbook/task_include.py reads
// both). Any other form fails the test, because the walk cannot follow it.
func includedFile(t *testing.T, where string, arguments any) string {
	t.Helper()
	if file, ok := arguments.(string); ok {
		return file
	}
	if named, ok := arguments.(map[string]any); ok {
		if file, ok := named["file"].(string); ok {
			return file
		}
	}
	t.Fatalf("%s: includes %v, which this walk cannot read", where, arguments)
	return ""
}

// nodeCalls walks tasks as a run reaches them and records, under the substrate
// that binds it, every controller call made once for each node. The boot phase
// is not walked, because the boot budget bounds every call in it (boot.yml).
// A condition the walk cannot read counts as true, so no call is dropped for a
// reason it did not prove, and an include it cannot follow fails the test. A
// call made other than once per node, including one a node loop makes inside
// another loop, fails the test too, because the node margin cannot count it.
func nodeCalls(t *testing.T, role, file string, tasks []map[string]any, scope callScope, calls map[string][]controllerCall) {
	t.Helper()
	for _, task := range tasks {
		where := fmt.Sprintf("%s/tasks/%s: %v", role, file, task["name"])
		inner, skipped := scope, false
		inner.vars = map[string]any{}
		maps.Copy(inner.vars, scope.vars)
		if vars, ok := task["vars"].(map[string]any); ok {
			maps.Copy(inner.vars, vars)
		}
		conditions, _ := task["when"].([]any)
		if condition, ok := task["when"].(string); ok {
			conditions = []any{condition}
		}
		for _, condition := range conditions {
			text := strings.TrimSpace(fmt.Sprint(condition))
			if match := substrateCondition.FindStringSubmatch(text); match != nil {
				skipped = skipped || (inner.substrate != "" && inner.substrate != match[1])
				inner.substrate = match[1]
			}
			if match := switchCondition.FindStringSubmatch(text); match != nil && inner.vars[match[1]] == false {
				skipped = true
			}
		}
		for _, key := range slices.Sorted(maps.Keys(task)) {
			if key != "loop" && !strings.HasPrefix(key, "with_") {
				continue
			}
			loop := task[key]
			if inner.perNode || inner.repeated || key != "loop" || strings.TrimSpace(fmt.Sprint(loop)) != nodeLoop {
				inner.perNode, inner.repeated = false, true
				where += fmt.Sprintf(" (%s %v)", key, loop)
			} else {
				inner.perNode = true
			}
		}
		if skipped {
			continue
		}
		if call, found := controllerCallOf(t, where, task); found {
			if !inner.perNode {
				t.Fatalf("%s: a %s made other than once for each node is one the node margin cannot count", where, call.name)
			}
			calls[inner.substrate] = append(calls[inner.substrate], call)
		}
		for _, key := range slices.Sorted(maps.Keys(task)) {
			switch builtinAction(key) {
			case "import_tasks", "include_tasks":
				if included := includedFile(t, where, task[key]); !(role == installRole && included == "boot.yml") {
					nodeCalls(t, role, included, roleTasks(t, role, included), inner, calls)
				}
			case "import_role", "include_role":
				arguments, _ := task[key].(map[string]any)
				included, collected := strings.CutPrefix(fmt.Sprint(arguments["name"]), "bootwright.core.")
				entry, named := arguments["tasks_from"].(string)
				if !collected || !named {
					t.Fatalf("%s: includes %v, which this walk cannot read", where, task[key])
				}
				nodeCalls(t, included, entry+".yml", roleTasks(t, included, entry+".yml"), inner, calls)
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			nested, _ := task[key].([]any)
			var blockTasks []map[string]any
			for _, entry := range nested {
				if blockTask, ok := entry.(map[string]any); ok {
					blockTasks = append(blockTasks, blockTask)
				}
			}
			nodeCalls(t, role, file, blockTasks, inner, calls)
		}
	}
}

// Each node adds to the installation's deadline exactly the bound of every
// call the role makes to that node's controller outside the boot phase, read
// from the embedded collection: an apply's media read in each of its two
// state reads (state.yml), and the eject and the disk selection that release
// the node's media (release.yml, through its substrate's boot_disk entry point,
// which it tells to power nothing on). The margin covers whichever entry point
// and substrate calls the most, so a call added to any of them, or dropped,
// fails here until the margin moves with it; a poll's own requests count as
// answered at once, as the bounds state.
func TestTheNodeMarginIsTheBoundOfEveryControllerCallTheRoleMakesForANode(t *testing.T) {
	largest, costliest := time.Duration(0), ""
	for _, operation := range operations {
		calls := map[string][]controllerCall{}
		file := operation + ".yml"
		nodeCalls(t, installRole, file, roleTasks(t, installRole, file), callScope{}, calls)
		for bound := range calls {
			made := calls[""]
			if bound != "" {
				made = slices.Concat(made, calls[bound])
			}
			total := time.Duration(0)
			for _, call := range made {
				total += call.bound
			}
			if total > largest {
				largest, costliest = total, fmt.Sprintf("%s on %q: %v", operation, bound, made)
			}
		}
	}
	if nodeMargin != largest {
		t.Fatalf("each node adds %s, but its controller calls outside the boot phase may take %s (%s)", nodeMargin, largest, costliest)
	}
	if nodeMargin != 11*time.Minute {
		t.Errorf("each node adds %s, want the 11m0s the specification states", nodeMargin)
	}
}

// The boot budget grows with the nodes a cluster boots, 300 seconds each and
// never less than 900, so a cluster of up to three nodes freezes the 900 it
// always did, and each node also adds its margin to the deadline. Nine nodes
// fit within the runner's ceiling, and ten refuse before registration, naming
// the deadline they would need and how many nodes fit. These are the figures
// the specification states.
func TestTheBootBudgetAndTheDeadlineGrowWithTheNodes(t *testing.T) {
	for _, test := range []struct {
		nodes    int
		boot     int
		deadline time.Duration
	}{
		{1, 900, 3*time.Hour + 56*time.Minute},
		{3, 900, 4*time.Hour + 18*time.Minute},
		{4, 1200, 4*time.Hour + 34*time.Minute},
		{9, 2700, 5*time.Hour + 54*time.Minute},
	} {
		_, install, _ := onlyRequests(t, largeCatalog(test.nodes))
		if len(install.Nodes) != test.nodes || install.Budgets.BootSeconds != test.boot {
			t.Errorf("a cluster of %d nodes freezes a boot budget of %d seconds for %d nodes, want %d",
				test.nodes, install.Budgets.BootSeconds, len(install.Nodes), test.boot)
		}
		if got := install.Deadline(); got != test.deadline {
			t.Errorf("a cluster of %d nodes installs under %s, want %s", test.nodes, got, test.deadline)
		}
	}
	_, _, _, err := Requests(largeCatalog(10), "controller", testContext)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" {
		t.Fatalf("a cluster of 10 nodes refuses with %#v", reported)
	}
	if want := "installing 10 nodes needs a run deadline of 6h10m0s, past the 6h0m0s every adapter run is held to"; reported[0].Message != want {
		t.Errorf("refusal = %q, want %q", reported[0].Message, want)
	}
	if want := "declare at most 9 nodes on ContainerCluster/ocp"; reported[0].Remediation != want {
		t.Errorf("remediation = %q, want %q", reported[0].Remediation, want)
	}
}

// largeCatalog is a cluster of count virtual nodes on a platform that installs
// several, all on the one libvirt provider.
func largeCatalog(count int) api.Catalog {
	objects := base()
	var nodes []api.Value
	for index := range count {
		machine := fmt.Sprintf("ocp-%02d", index+1)
		objects = append(objects, guest(machine, fmt.Sprintf("198.51.100.%d/24", 100+index)))
		nodes = append(nodes, node(fmt.Sprintf("master-%d", index), "master", machine,
			fmt.Sprintf("master-%d.ocp.lab.example.test", index)))
	}
	return api.NewCatalog(append(objects, cluster("ocp",
		installSelection(
			endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "openshift"),
			field("platform", api.MapValue(text("type", "baremetal"),
				field("baremetal", api.MapValue(text("provisioningNetwork", "disabled"))))),
		), nodes...)))
}

// Each node adds to the installation's deadline, and the runner cuts every run
// short at its ceiling rather than honoring a longer deadline. The largest
// cluster whose deadline fits is planned, and one node more refuses before
// registration, naming the deadline, the ceiling and how many nodes fit,
// rather than being killed part way through its installation.
func TestAClusterWhoseDeadlinePassesTheCeilingRefusesBeforeRegistration(t *testing.T) {
	// The count is bounded, so a deadline that stops growing with its nodes
	// fails here rather than holding the suite until go test's own timeout.
	const most = 1000
	deadlineOf := func(nodes int) time.Duration { return installDeadline(installBudgets(nodes), nodes) }
	largest := 0
	for ; largest < most && deadlineOf(largest+1) <= lifecycle.MaxDeadline; largest++ {
		if deadlineOf(largest+1) <= deadlineOf(largest) {
			t.Fatalf("a cluster of %d nodes has a deadline of %s, no longer than one of %d nodes: each node must add to it",
				largest+1, deadlineOf(largest+1), largest)
		}
	}
	if largest == most {
		t.Fatalf("%d nodes still fit within the %s ceiling: each node must add its margin to the deadline", most, lifecycle.MaxDeadline)
	}
	if largest < 3 {
		t.Fatalf("only %d nodes fit within the ceiling, fewer than the compact cluster declares", largest)
	}
	_, install, _ := onlyRequests(t, largeCatalog(largest))
	if deadline := install.Deadline(); deadline > lifecycle.MaxDeadline {
		t.Fatalf("a cluster of %d nodes was planned with a deadline of %s, past the %s ceiling", largest, deadline, lifecycle.MaxDeadline)
	}
	refused := largeCatalog(largest + 1)
	if unsupported := Unsupported(refused); len(unsupported) != 1 || unsupported[0] != "ContainerCluster/ocp" {
		t.Fatalf("unsupported = %v, want the cluster whose deadline passes the ceiling", unsupported)
	}
	_, _, _, err := Requests(refused, "controller", testContext)
	if err == nil {
		t.Fatalf("a cluster of %d nodes was planned past the ceiling", largest+1)
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" {
		t.Fatalf("refusal = %#v", reported)
	}
	deadline := deadlineOf(largest + 1)
	for _, want := range []string{fmt.Sprint(largest + 1), deadline.String(), lifecycle.MaxDeadline.String()} {
		if !strings.Contains(reported[0].Message, want) {
			t.Errorf("refusal %q does not name %s", reported[0].Message, want)
		}
	}
	if want := fmt.Sprintf("declare at most %d nodes on ContainerCluster/ocp", largest); reported[0].Remediation != want {
		t.Errorf("remediation = %q, want %q", reported[0].Remediation, want)
	}
}
