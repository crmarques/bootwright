//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
)

func TestExampleCephNodesRegisterUnderTheirMachineFQDN(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-3-nodes-ceph"))
	effective := state.Effective()
	cluster := requireObject(t, effective, api.CephCluster, "ceph-lab")
	nodes := cluster.Spec().Get("topology", "nodes").Items()
	if len(nodes) != 3 {
		t.Fatalf("nodes %d", len(nodes))
	}
	for _, node := range nodes {
		host := requireObject(t, effective, api.Machine, node.Get("machineRef").Text())
		fqdn, declared := machine.Address(host, "fqdn")
		if !declared || !machine.Installed(host) || node.Get("fqdn").Text() != fqdn {
			t.Fatalf("node %s registers as %q over installed Machine %s with fqdn %q", node.Get("name").Text(), node.Get("fqdn").Text(), host.Name(), fqdn)
		}
	}
}

func TestExampleCephNodeRefusesARestatedIdentity(t *testing.T) {
	for _, test := range []struct {
		name, line, field string
		mentions          []string
	}{
		{"composed fqdn", "        fqdn: rhel-01.ceph-lab.lab.example.test\n", "$.spec.topology.nodes[0].fqdn", []string{"rhel-01.ceph-lab.lab.example.test", "rhel-01.lab.example.test"}},
		{"authored site", "        site: lab\n", "$.spec.topology.nodes[0].site", []string{"Machine/rhel-01"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			const anchor = "      - name: rhel-01\n"
			sources := editedExample(t, "libvirt-3-nodes-ceph", exampleEdit{"storage/cluster.yaml", anchor, anchor + test.line})
			_, _, err := wireCompiler().Compile(context.Background(), sources)
			refused := diagnostics.Of(err)
			if len(refused) != 1 || refused[0].Code != "api.invariant" || refused[0].Field != test.field {
				t.Fatalf("refusals %+v", refused)
			}
			for _, mention := range test.mentions {
				if !strings.Contains(refused[0].Message+" "+refused[0].Remediation, mention) {
					t.Fatalf("refusal %+v does not name %s", refused[0], mention)
				}
			}
		})
	}
}

func bindingTypes(t *testing.T, cluster api.Object) []string {
	t.Helper()
	types := []string{}
	for _, binding := range cluster.Spec().Get("serviceBindings").Items() {
		for _, service := range cluster.Spec().Get("services").Items() {
			name := service.Get("service_type").Text()
			if id := service.Get("service_id").Text(); id != "" {
				name += "." + id
			}
			if name == binding.Get("serviceRef").Text() {
				types = append(types, service.Get("service_type").Text())
			}
		}
	}
	slices.Sort(types)
	return types
}

func TestExampleServiceBindingsPlanByTheRoutingTable(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-4-nodes-ceph-observability"))
	if got := bindingTypes(t, requireObject(t, state.Effective(), api.CephCluster, "ceph-lab")); !slices.Equal(got, []string{"grafana", "mgmt-gateway", "oauth2-proxy", "snmp-gateway"}) {
		t.Fatalf("the observability example binds %v", got)
	}
	if plan := planIngressFixture(t, state, "libvirt-4-nodes-ceph-observability"); len(plan.Blocks) == 0 {
		t.Fatal("empty observability plan")
	}

	const anchor = "        enable_auth: false\n\n  images:\n"
	bindings := "        enable_auth: true\n\n  serviceBindings:\n    - serviceRef: nvmeof.nvme-config.primary\n      secrets:\n"
	for _, leaf := range []struct{ path, secret string }{{"root_ca_cert", "nvmeof-root"}, {"server_cert", "nvmeof-server"}, {"server_key", "nvmeof-server"}, {"client_cert", "nvmeof-client"}, {"client_key", "nvmeof-client"}} {
		bindings += "        - path: /spec/" + leaf.path + "\n          secretRef: " + leaf.secret + "\n"
	}
	secrets := map[string]string{}
	for _, name := range []string{"nvmeof-server", "nvmeof-client"} {
		secrets["secret-descriptors/"+name+".yaml"] = "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: " + name + "\nspec:\n  type: tlsCertificate\n  source:\n    generated:\n      commonName: " + name + ".lab.example.test\n      validityDays: 365\n"
	}
	secrets["secret-descriptors/nvmeof-root.yaml"] = "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: nvmeof-root\nspec:\n  type: caBundle\n  source:\n    contextStore: {}\n"
	sources := extendedExample(t, "libvirt-5-nodes-ceph-nvmeof", secrets, exampleEdit{"storage/cluster.yaml", anchor, bindings + "\n  images:\n"})
	state, _ = compileAcceptance(t, sources)
	if got := bindingTypes(t, requireObject(t, state.Effective(), api.CephCluster, "ceph-lab")); !slices.Equal(got, []string{"nvmeof"}) {
		t.Fatalf("the NVMe-oF example binds %v", got)
	}
	if plan := planIngressFixture(t, state, "libvirt-5-nodes-ceph-nvmeof"); len(plan.Blocks) == 0 {
		t.Fatal("empty NVMe-oF plan")
	}

	const nfs = "  topology:\n"
	sources = editedExample(t, "libvirt-3-nodes-ceph-nfs", exampleEdit{"storage/cluster.yaml", nfs,
		"  serviceBindings:\n    - serviceRef: nfs.shares\n      secrets:\n        - path: /spec/ssl_cert\n          secretRef: artifact-server-tls\n\n" + nfs})
	_, _, err := wireCompiler().Compile(context.Background(), sources)
	refused := diagnostics.Of(err)
	if len(refused) == 0 || !strings.Contains(refused[0].Message, "NFS services take no secret bindings") {
		t.Fatalf("an nfs binding was not refused: %+v", refused)
	}
}
