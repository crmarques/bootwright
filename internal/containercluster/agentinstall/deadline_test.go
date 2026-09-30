package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

// Each node adds to the installation's deadline the bound of every call the
// installation makes to its controller outside the boot budget: its media read
// in each of the two state reads (state.yml), and the eject and the disk
// selection that release its media (release.yml, through the substrate's
// boot_disk entry point, which it tells to power nothing on). A margin shorter
// than those calls lets the runner kill an installation still inside them, so
// the figures the specification states are the ones derived here.
func TestEachNodeAddsTheBoundOfEveryControllerCallOutsideTheBootBudget(t *testing.T) {
	calls := 2*substrate.ControllerMediaReadBound + substrate.ControllerEjectBound + substrate.ControllerBootSelectionBound
	if nodeMargin < calls {
		t.Fatalf("each node adds %s, less than the %s its controller calls may take", nodeMargin, calls)
	}
	if nodeMargin != 11*time.Minute {
		t.Errorf("each node adds %s, want the 11m0s the specification states", nodeMargin)
	}
	if got, want := installDeadline(installBudgets, 1), 3*time.Hour+56*time.Minute; got != want {
		t.Errorf("a single node installs under %s, want the %s the specification states", got, want)
	}
	if got := int((lifecycle.MaxDeadline - installDeadline(installBudgets, 0)) / nodeMargin); got != 12 {
		t.Errorf("%d nodes fit within the %s ceiling, want the 12 the specification states", got, lifecycle.MaxDeadline)
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
	largest := 0
	for ; largest < most && installDeadline(installBudgets, largest+1) <= lifecycle.MaxDeadline; largest++ {
		if installDeadline(installBudgets, largest+1) <= installDeadline(installBudgets, largest) {
			t.Fatalf("a cluster of %d nodes has a deadline of %s, no longer than one of %d nodes: each node must add to it",
				largest+1, installDeadline(installBudgets, largest+1), largest)
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
	deadline := installDeadline(installBudgets, largest+1)
	for _, want := range []string{fmt.Sprint(largest + 1), deadline.String(), lifecycle.MaxDeadline.String()} {
		if !strings.Contains(reported[0].Message, want) {
			t.Errorf("refusal %q does not name %s", reported[0].Message, want)
		}
	}
	if want := fmt.Sprintf("declare at most %d nodes on ContainerCluster/ocp", largest); reported[0].Remediation != want {
		t.Errorf("remediation = %q, want %q", reported[0].Remediation, want)
	}
}
