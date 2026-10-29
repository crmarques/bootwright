//go:build linux && amd64

package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const witnessAccess = "  access:\n    ssh:\n      addressRef: ip\n      user: bootwright\n      knownHostsRef: witness-known-hosts\n\n      auth:\n        operatorIdentity: {}\n"

// tiebreakerSources is the three-node libvirt example stretched over two data
// sites of two installed hosts each, with a provided MON-only witness at a
// third site whose Machine declares access.
func tiebreakerSources(t *testing.T, access string) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, "libvirt-3-nodes-ceph")
	root := sources.Roots[0]
	edit := func(file, old, replacement string) {
		path := filepath.Join(root, file)
		index := slices.IndexFunc(sources.Files, func(f desiredstate.SourceFile) bool { return f.Path() == path })
		if index < 0 {
			t.Fatalf("the example has no %s", file)
		}
		sources.Files[index] = desiredstate.NewSourceFile(path, []byte(mustReplaceOnce(t, file, string(sources.Files[index].Bytes()), old, replacement)))
	}
	read := func(file string) string {
		index := slices.IndexFunc(sources.Files, func(f desiredstate.SourceFile) bool { return f.Path() == filepath.Join(root, file) })
		return string(sources.Files[index].Bytes())
	}
	add := func(file, body string) {
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(root, file), []byte(body)))
	}
	sources.Files = slices.DeleteFunc(sources.Files, func(f desiredstate.SourceFile) bool {
		return f.Path() == filepath.Join(root, "storage/placement-policy.yaml")
	})
	edit("environment.yaml", "  storageClusters:\n", "  sites:\n    - name: site-a\n    - name: site-b\n    - name: site-c\n\n  storageClusters:\n")
	rhel04 := strings.ReplaceAll(strings.ReplaceAll(read("infra/machines/rhel-03.yaml"), "rhel-03", "rhel-04"), "198.51.100.13/24", "198.51.100.14/24")
	add("infra/machines/rhel-04.yaml", rhel04)
	for machine, site := range map[string]string{"rhel-01": "site-a", "rhel-02": "site-a", "rhel-03": "site-b", "rhel-04": "site-b"} {
		edit("infra/machines/"+machine+".yaml", "spec:\n  substrate:\n", "spec:\n  placement:\n    site: "+site+"\n\n  substrate:\n")
	}
	add("infra/machines/witness.yaml", "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata:\n  name: witness\n\nspec:\n  placement:\n    site: site-c\n\n  os:\n    provided: true\n\n"+
		"  network:\n    addresses:\n      - name: fqdn\n        address: witness.lab.example.test\n\n      - name: ip\n        address: 198.51.100.20\n\n"+access)
	add("secret-descriptors/witness-known-hosts.yaml", "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: witness-known-hosts\n\nspec:\n  type: opaque\n\n  source:\n    contextStore: {}\n")
	for _, pool := range []string{"pool", "cephfs-data-pool", "cephfs-metadata-pool"} {
		edit("storage/"+pool+".yaml", "    size: 2\n", "    size: 4\n")
	}
	edit("storage/cluster.yaml", "      placement:\n        count: 3\n        label: ceph-core\n",
		"      placement:\n        hosts:\n          - rhel-01.lab.example.test\n          - rhel-02.lab.example.test\n          - rhel-03.lab.example.test\n          - rhel-04.lab.example.test\n          - witness.lab.example.test\n")
	edit("storage/cluster.yaml", "        count: 2\n        label: ceph-core\n", "        label: ceph-core\n")
	edit("storage/cluster.yaml", "        count: 1\n        label: ceph-core\n", "        label: ceph-core\n")
	edit("storage/cluster.yaml", "        machineRef: rhel-03\n\n        labels:\n          - ceph-core\n",
		"        machineRef: rhel-03\n\n        labels:\n          - ceph-core\n\n"+
			"      - name: rhel-04\n        fqdn: rhel-04.lab.example.test\n        machineRef: rhel-04\n\n        labels:\n          - ceph-core\n\n"+
			"      - name: witness\n        fqdn: witness.lab.example.test\n        machineRef: witness\n\n        labels:\n          - ceph-mon\n\n"+
			"    stretch:\n      failureDomain: datacenter\n\n      tiebreaker:\n        node: witness\n")
	return sources
}

func tiebreakerPlan(t *testing.T, sources desiredstate.Sources) ([]reconciliation.BlockDefinition, error) {
	t.Helper()
	state, _, err := wireCompiler().Compile(context.Background(), sources)
	if err != nil {
		t.Fatalf("validation refuses a stretch cluster with a provided tiebreaker: %+v", diagnostics.Of(err))
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: "stretch"}}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	return definitions, nil
}

// The Environment and Ceph admission run together over one complete catalog:
// a provided tiebreaker that names only the cluster SSH user and its pinned
// host key validates, and the plan binds it with the Environment's fleet key.
func TestAProvidedTiebreakerValidatesAndPlansWithTheFleetIdentity(t *testing.T) {
	definitions, err := tiebreakerPlan(t, tiebreakerSources(t, witnessAccess))
	if err != nil {
		t.Fatalf("the plan refuses: %+v", diagnostics.Of(err))
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(definitions, func(d reconciliation.BlockDefinition) bool { return d.ID == ceph.HostBlockID("ceph-lab", "witness") })
	if index < 0 {
		t.Fatal("no host block for the provided tiebreaker")
	}
	request, err := ceph.DecodeHostRequest(definitions[index].Request)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range request.Cluster.Hosts {
		if host.SSHKeyRef != "bootwright-machine-key" {
			t.Errorf("Ceph host %s is dialed with %q, not the fleet key", host.Machine, host.SSHKeyRef)
		}
		if host.Machine == "witness" && (!host.Provided || host.User != "bootwright" || host.KnownHostsRef != "witness-known-hosts" || host.Address != "198.51.100.20") {
			t.Errorf("tiebreaker host = %+v", host)
		}
	}
}

func TestATiebreakerAuthoringTheFleetKeyIsRefusedWithTheRemedy(t *testing.T) {
	access := strings.Replace(witnessAccess, "operatorIdentity: {}", "privateKeyRef: bootwright-machine-key", 1)
	_, _, err := wireCompiler().Compile(context.Background(), tiebreakerSources(t, access))
	found := false
	for _, d := range diagnostics.Of(err) {
		if d.Code == "api.invariant" && d.Field == "$.spec.remoteMachinesAccessKey.keyRef" {
			found = strings.Contains(d.Message, "Machine/witness") && strings.Contains(d.Remediation, "give Machine/witness its own sshKeyPair Secret or an operator identity") &&
				strings.Contains(d.Remediation, "stretch tiebreaker, it never names that key: authorize the public half of Secret bootwright-machine-key")
		}
	}
	if !found {
		t.Fatalf("the fleet key as the tiebreaker's access key was not refused with the tiebreaker remedy: %+v", diagnostics.Of(err))
	}
}

func TestAProvidedTiebreakerRefusesAtPlanWithoutItsHostKeyPinOrTheClusterUser(t *testing.T) {
	for name, access := range map[string]string{
		"no knownHostsRef": strings.Replace(witnessAccess, "      knownHostsRef: witness-known-hosts\n", "", 1),
		"another user":     strings.Replace(witnessAccess, "user: bootwright", "user: operator", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tiebreakerPlan(t, tiebreakerSources(t, access))
			d := diagnostics.Of(err)
			if len(d) != 1 || d[0].Code != "lifecycle.unsupported" || !strings.Contains(d[0].Message, "Machine/witness") ||
				!strings.Contains(d[0].Remediation, "user bootwright") || !strings.Contains(d[0].Remediation, "knownHostsRef") {
				t.Fatalf("plan = %+v", d)
			}
		})
	}
}
