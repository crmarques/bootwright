package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets/material"
	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
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
	capability := NewMedia(runner, material.AuthorizedKey)
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
// still inside one of its phases. The media block's fixed budget stays within
// the ceiling fixed budgets are held to; the installation's grows with its
// nodes and is bounded by no ceiling, so its runs state exactly its request's.
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
			if deadline := installRunDeadline(t, install, operation); deadline <= phases || deadline != install.Deadline() {
				t.Errorf("%s install %s runs under %s, which must exceed the %s its phases are budgeted and be the %s its request states",
					name, operation, deadline, phases, install.Deadline())
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
	install.Budgets = InstallBudgets{BootSeconds: 4111, BootstrapSeconds: 6007, InstallSeconds: 8123, FanOut: 4, RoundMarginSeconds: 660}
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
		want := time.Duration(seconds)*time.Second + installMargin + nodeMargin
		for _, operation := range runs {
			if got := installRunDeadline(t, frozen, operation); got != want {
				t.Errorf("install %s with %s moved runs under %s, want %s for %+v", operation, name, got, want, frozen.Budgets)
			}
		}
	}
	one := install
	one.Nodes = one.Nodes[:1]
	if got, want := installRunDeadline(t, one, "apply"), installRunDeadline(t, install, "apply"); got != want {
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
	for _, arm := range substrate.Realized() {
		bounds, ok := substrate.MachineBoundsFor(arm)
		if !ok {
			t.Fatalf("no bounds for %s", arm)
		}
		if bounds.NodeMargin < 2*bounds.StateRead+bounds.MediaEject+substrate.ControllerBootSelectionBound {
			t.Fatalf("%s round margin omits a node phase", arm)
		}
	}
	for file, operation := range map[string]string{"node_state.yml": "state_read", "release.yml": "media_eject"} {
		found := false
		for _, task := range roleTasks(t, installRole, file) {
			data, _ := yaml.Marshal(task)
			if strings.Contains(string(data), "containercluster_install_agent_port_operation: "+operation) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s no longer invokes %s; revisit the round margin", file, operation)
		}
	}
}

// The boot budget and deadline grow only when a new four-node round starts.
func TestTheBootBudgetAndTheDeadlineGrowWithTheNodes(t *testing.T) {
	for _, test := range []struct {
		nodes    int
		boot     int
		deadline time.Duration
	}{
		{1, 900, 3*time.Hour + 56*time.Minute},
		{4, 900, 3*time.Hour + 56*time.Minute},
		{5, 1200, 4*time.Hour + 12*time.Minute},
		{28, 4200, 5*time.Hour + 57*time.Minute},
		{29, 4800, 6*time.Hour + 18*time.Minute},
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
}

// largeCatalog is a cluster of count virtual nodes on a platform that installs
// several, all on the one libvirt provider.
func largeCatalog(count int) api.Catalog {
	objects := base()
	var nodes []api.Value
	for index := range count {
		machine := fmt.Sprintf("ocp-%02d", index+1)
		objects = append(objects, guest(machine, fmt.Sprintf("198.51.100.%d", 100+index)))
		nodes = append(nodes, node(fmt.Sprintf("master-%d", index), "controlPlane", machine,
			fmt.Sprintf("master-%d.ocp.lab.example.test", index)))
	}
	return api.NewCatalog(append(objects, cluster("ocp",
		installSelection(
			endpoints("198.51.100.10", "198.51.100.10", "198.51.100.11", "installer"),
			field("platform", api.MapValue(field("baremetal", api.MapValue(text("provisioningNetwork", "disabled"))))),
		), nodes...)))
}

// largePhysicalCatalog is a cluster of count physical nodes on the one
// bare-metal provider.
func largePhysicalCatalog(count int) api.Catalog {
	objects := append(base(), metalProvider())
	var nodes []api.Value
	for index := range count {
		machine := fmt.Sprintf("metal-%02d", index+1)
		objects = append(objects, server(machine, fmt.Sprintf("198.51.100.%d", 100+index), fmt.Sprintf("aa:bb:cc:dd:ee:%02x", index+1)))
		nodes = append(nodes, node(fmt.Sprintf("master-%d", index), "controlPlane", machine,
			fmt.Sprintf("master-%d.metal.lab.example.test", index)))
	}
	return api.NewCatalog(append(objects, cluster("metal",
		installSelection(
			endpoints("198.51.100.20", "198.51.100.20", "198.51.100.21", "external"),
			field("platform", api.MapValue(field("baremetal", api.MapValue()))),
		), nodes...)))
}

// A cluster's size is bounded by no deadline (D235 (3)): each round of nodes
// adds to its installation's deadline, and a cluster whose deadline passes the
// ceiling the fixed-budget families keep plans on every arm, its request
// stating exactly the deadline its rounds derive, which its run is handed.
func TestAClusterOfAnySizePlansUnderTheDeadlineItsRoundsDerive(t *testing.T) {
	for name, test := range map[string]struct {
		catalog api.Catalog
		nodes   int
		arm     substrate.Arm
	}{
		"emulated": {largeCatalog(29), 29, substrate.ArmLibvirt},
		"kubevirt": {kubevirtCatalog(3, 6, true), 9, substrate.ArmKubeVirt},
		"physical": {largePhysicalCatalog(33), 33, substrate.ArmBaremetal},
	} {
		t.Run(name, func(t *testing.T) {
			if refused := Refusals(test.catalog); len(refused) != 0 {
				t.Fatalf("a cluster of %d nodes refuses: %+v", test.nodes, refused)
			}
			_, install, _ := onlyRequests(t, test.catalog)
			want := installDeadline(installBudgets(test.nodes, nil, test.arm), test.nodes)
			if len(install.Nodes) != test.nodes || install.Deadline() != want || want <= lifecycle.MaxDeadline {
				t.Fatalf("a cluster of %d nodes states %s for %d nodes, want %s past %s",
					test.nodes, install.Deadline(), len(install.Nodes), want, lifecycle.MaxDeadline)
			}
		})
	}
	_, install, _ := onlyRequests(t, largeCatalog(29))
	for _, operation := range runs {
		if got := installRunDeadline(t, install, operation); got != install.Deadline() {
			t.Errorf("install %s of 29 nodes runs under %s, want the %s its request states", operation, got, install.Deadline())
		}
	}
}
