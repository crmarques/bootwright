package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type keptReach struct {
	machines []string
	clusters []string
	services []*testCapability
}

func reachMachine(name string, stage reconciliation.Stage, dependencies ...string) reconciliation.BlockDefinition {
	machine := keptBlock(name, "Machine", "node-machine-v1", stage, dependencies...)
	machine.Object = name
	return machine
}

func (c keptCluster) bind(kind, implementation string, definitions ...reconciliation.BlockDefinition) *testCapability {
	capability := &testCapability{definitions: definitions}
	c.h.service.capabilities.(keptResolver)[CapabilityBinding{Kind: kind, Implementation: implementation}] = capability
	return capability
}

func (c keptCluster) dependOn(id string, dependencies ...string) {
	for _, definitions := range [][]reconciliation.BlockDefinition{c.install.definitions, c.media.definitions} {
		for index := range definitions {
			if definitions[index].ID == id {
				definitions[index].Dependencies = append(definitions[index].Dependencies, dependencies...)
				slices.Sort(definitions[index].Dependencies)
			}
		}
	}
}

func (c keptCluster) nodesOn(dependency string) {
	for index := range c.machine.definitions {
		if strings.HasPrefix(c.machine.definitions[index].ID, "node-") {
			c.machine.definitions[index].Dependencies = []string{dependency}
		}
	}
}

func serviceReach(kind string) func(keptCluster) keptReach {
	return func(c keptCluster) keptReach {
		c.machine.definitions = append(c.machine.definitions, reachMachine("service-host", reconciliation.StageInfrastructureServices))
		service := c.bind(kind, "service-v1", keptBlock("service", kind, "service-v1", reconciliation.StageInfrastructureServices, "service-host"))
		c.dependOn("install", "service")
		return keptReach{machines: []string{"service-host"}, services: []*testCapability{service}}
	}
}

// keptReaches are the ways an installation on kept nodes reaches a Machine it
// does not run on: the managed hosts of the DNS, NTP and Proxy services it
// requires, the host of the artifact server its media publishes to, the node
// Machines of the cluster hosting its KubeVirt nodes, and the host of the
// libvirt provider holding its nested nodes.
var keptReaches = map[string]func(keptCluster) keptReach{
	"a managed DNS server's host": serviceReach("DNSServer"),
	"a managed NTP server's host": serviceReach("NTPServer"),
	"a managed proxy's host":      serviceReach("Proxy"),
	"an artifact server's host": func(c keptCluster) keptReach {
		c.machine.definitions = append(c.machine.definitions, reachMachine("server", reconciliation.StageInfrastructureServices))
		server := c.bind("ArtifactServer", "artifact-server-nginx-v1", keptBlock("artifacts", "ArtifactServer", "artifact-server-nginx-v1", reconciliation.StageInfrastructureServices, "server"))
		c.dependOn("media", "artifacts")
		return keptReach{machines: []string{"server"}, services: []*testCapability{server}}
	},
	"a KubeVirt node's host-cluster Machines": func(c keptCluster) keptReach {
		c.machine.definitions = append(c.machine.definitions,
			reachMachine("hub-1", reconciliation.StageMachines, "host"), reachMachine("hub-2", reconciliation.StageMachines, "host"))
		hub := keptBlock("hub", "OpenShiftCluster", "hub-install-v1", reconciliation.StageClusters, "hub-1", "hub-2")
		hub.Object = "hub"
		installed := c.bind("OpenShiftCluster", "hub-install-v1", hub)
		provider := c.bind("InfraProvider", "kubevirt-v1", keptBlock("kubevirt", "InfraProvider", "kubevirt-v1", reconciliation.StageSubstrates, "hub"))
		c.nodesOn("kubevirt")
		c.media.definitions[0].ClusterOwners = []string{"edge"}
		c.addon.definitions[0].Dependencies, c.addon.definitions[0].Requires = nil, []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: "edge"}}
		return keptReach{machines: []string{"hub-1", "hub-2"}, clusters: []string{"edge"}, services: []*testCapability{installed, provider}}
	},
	"a nested libvirt guest's host Machine": func(c keptCluster) keptReach {
		c.machine.definitions = append(c.machine.definitions, reachMachine("outer", reconciliation.StageMachines, "host"))
		provider := c.bind("InfraProvider", "guest-host-v1", keptBlock("guests", "InfraProvider", "guest-host-v1", reconciliation.StageSubstrates, "outer"))
		c.nodesOn("guests")
		return keptReach{machines: []string{"outer"}, services: []*testCapability{provider}}
	},
}

// B1875: a step runs on the Machines it names, its own and those of the
// cluster steps it depends on, never on the Machines a service, a substrate or
// a Machine it depends on runs on. So for each reach a staged destroy at
// clusters takes back the installation on its kept nodes, proves only those
// nodes off while the reached Machines run, takes back and touches no service,
// and the restore proves only the nodes off again.
func TestAStagedDestroyTakesBackAnInstallationWhateverItsServicesRunOn(t *testing.T) {
	t.Parallel()
	for name, arrange := range keptReaches {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster := keptClusterHarness(t, false, "")
			reach := arrange(cluster)
			h := cluster.h
			completeApply(t, h)
			for _, machine := range reach.machines {
				cluster.power(machine, true)
			}
			cluster.resetPower()
			if _, err := clusterDestroy(h, reconciliation.StageClusters, reach.clusters...); err != nil {
				t.Fatalf("destroy --stage clusters: %+v", diagnostics.Of(err))
			}
			removal, plan := currentRecord(t, h)
			if !trimmed(removal) || !slices.Equal(planIDs(plan), []string{"addon", "install", "media"}) ||
				!slices.Equal(removal.Stage.TakenBack, []string{"addon", "install", "media"}) || !slices.Equal(removal.Stage.KeptPower, keptPower("node-1", "node-2")) {
				t.Fatalf("the staged destroy recorded %+v over %v", removal, planIDs(plan))
			}
			if !slices.Equal(keptHostsOf(plan, "install"), []string{"node-1", "node-2"}) || !slices.Equal(keptHostsOf(plan, "media"), []string{"node-1"}) {
				t.Fatalf("the removal froze the kept hosts %v and %v", keptHostsOf(plan, "install"), keptHostsOf(plan, "media"))
			}
			if got := cluster.powered(); !slices.Contains(got, "node-1") || !slices.Contains(got, "node-2") || slices.ContainsFunc(got, func(machine string) bool { return slices.Contains(reach.machines, machine) }) {
				t.Fatalf("the staged destroy read the power of %v", got)
			}
			for _, service := range reach.services {
				if len(service.destroys) != 0 {
					t.Fatalf("the staged destroy removed %v", service.destroys)
				}
			}
			if destroyed := slices.Concat(cluster.install.destroys, cluster.machine.destroys); !slices.Equal(destroyed, []string{"install"}) {
				t.Fatalf("the staged destroy removed %v", destroyed)
			}

			cluster.resetPower()
			restoreWith(t, h)
			if got := cluster.powered(); !slices.Equal(slices.Sorted(slices.Values(got)), []string{"node-1", "node-2"}) {
				t.Fatalf("the restore read the power of %v, want only the kept nodes its installation runs on", got)
			}
		})
	}
}

// A step still runs on the Machines of a cluster step it depends on: an
// add-on of a kept installation refuses an add-ons floor naming the nodes,
// and not the host of the DNS server the installation uses.
func TestAnAddonOfAKeptInstallationRunsOnItsNodes(t *testing.T) {
	t.Parallel()
	cluster := keptClusterHarness(t, false, "")
	serviceReach("DNSServer")(cluster)
	completeApply(t, cluster.h)
	before := untouchedOf(cluster.h)
	_, err := cluster.h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step addon runs on Machine/node-1, Machine/node-2:") {
		t.Fatalf("destroy --stage add-ons over an add-on of a kept installation returned %v", reported)
	}
	before.require(t, cluster.h)
}

// A Machine the installation requires directly, here a Machine step of a
// third Machine, is one it runs on: the staged destroy freezes it beside the
// nodes as a kept host and power target, and refuses destroy.in-use while it
// runs.
func TestAnInstallationRunsOnAMachineItRequiresDirectly(t *testing.T) {
	t.Parallel()
	for name, running := range map[string]bool{"off": false, "running": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cluster := keptClusterHarness(t, false, "")
			cluster.machine.definitions = append(cluster.machine.definitions, reachMachine("third", reconciliation.StageMachines, "host"))
			witness := keptBlock("witness", "Machine", "witness-v1", reconciliation.StageMachines, "third")
			witness.Object = "third"
			cluster.bind("Machine", "witness-v1", witness)
			cluster.dependOn("install", "witness")
			completeApply(t, cluster.h)
			cluster.power("third", running)
			_, err := clusterDestroy(cluster.h, reconciliation.StageClusters)
			if running {
				if reported := diagnostics.Of(err); firstCode(err) != destroyInUse.Code || !strings.Contains(reported[0].Message, "Machine/third (powered on)") {
					t.Fatalf("destroy --stage clusters over a running required Machine returned %v", reported)
				}
				return
			}
			if err != nil {
				t.Fatalf("destroy --stage clusters: %+v", diagnostics.Of(err))
			}
			removal, plan := currentRecord(t, cluster.h)
			if !slices.Equal(keptHostsOf(plan, "install"), []string{"node-1", "node-2", "third"}) || !slices.Equal(removal.Stage.KeptPower, keptPower("node-1", "node-2", "third")) {
				t.Fatalf("the removal froze %v and the power targets %+v", keptHostsOf(plan, "install"), removal.Stage.KeptPower)
			}
		})
	}
}

// A host or node binding that does not decode refuses with its error before
// anything registers, at the staged destroy and at the restore's power
// targets, rather than reading as no Machine.
func TestAnUndecodableBindingRefusesRatherThanNamingNoMachine(t *testing.T) {
	t.Parallel()
	undecodable := errors.New("the node binding does not decode")
	cluster := keptClusterHarness(t, false, "")
	completeApply(t, cluster.h)
	cluster.install.nodesErr = undecodable
	before := untouchedOf(cluster.h)
	if _, err := clusterDestroy(cluster.h, reconciliation.StageClusters); !errors.Is(err, undecodable) {
		t.Fatalf("destroy --stage clusters over an undecodable node binding returned %v", err)
	}
	before.require(t, cluster.h)
	cluster.install.nodesErr = nil
	if _, err := clusterDestroy(cluster.h, reconciliation.StageClusters); err != nil {
		t.Fatalf("destroy --stage clusters: %+v", diagnostics.Of(err))
	}
	cluster.install.nodesErr = undecodable
	cluster.resetPower()
	if _, err := cluster.h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); !errors.Is(err, undecodable) || len(cluster.powered()) != 0 {
		t.Fatalf("the restore over an undecodable node binding returned %v after reading the power of %v", err, cluster.powered())
	}
}
