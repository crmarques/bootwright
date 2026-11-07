package machine

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestLibvirtNICAttachmentsBoundGuestMTU(t *testing.T) {
	native := m("interfaces", list(m("name", "eth0", "type", "ethernet", "mtu", api.IntegerValue("1500")), m("name", "eth1", "type", "ethernet", "mtu", api.IntegerValue("9000"))))
	provider := object(api.InfraProvider, "hypervisor", m("libvirt", m(), "networkAttachments", list(m("name", "primary", "libvirt", m()), m("name", "storage", "libvirt", m("mtu", api.IntegerValue("9000"))))))
	entries := list(m("nicRef", "eth0", "attachmentRef", "primary"), m("nicRef", "eth1", "attachmentRef", "storage"))
	for name, test := range map[string]struct {
		entries api.Value
		native  api.Value
		want    string
	}{
		"complete":       {entries, native, ""},
		"unknown":        {list(entries.Items()[0], m("nicRef", "eth1", "attachmentRef", "absent")), native, ""},
		"oversized":      {entries, native.With("interfaces", list(native.Get("interfaces").Items()[0].With("mtu", api.IntegerValue("9000")), native.Get("interfaces").Items()[1])), "above its attachment"},
		"bond oversized": {entries, native.With("interfaces", list(append(native.Get("interfaces").Items(), m("name", "bond0", "type", "bond", "mtu", api.IntegerValue("9000"), "link-aggregation", m("port", api.StringList("eth0", "eth1"))))...)), "above its attachment"},
	} {
		t.Run(name, func(t *testing.T) {
			machine := object(api.Machine, "node", m("substrate", m("providerRef", "hypervisor", "libvirt", m("nics", test.entries)), "hardware", m("nics", list(m("name", "eth0"), m("name", "eth1")))))
			issues := validateAttachments(machine, provider, test.native)
			if test.want == "" && len(issues) != 0 {
				t.Fatal(issues)
			}
			if test.want != "" {
				for _, issue := range issues {
					if strings.Contains(issue.Message, test.want) {
						return
					}
				}
				t.Fatalf("missing %q in %v", test.want, issues)
			}
		})
	}
}

func TestKubeVirtGuestMTUIsBoundByTheHostNetworkItsAttachmentNames(t *testing.T) {
	cudn := func(name, network string) api.Value {
		return m("name", name, "kubevirt", m("networkRef", m("apiGroup", "k8s.ovn.org", "kind", "ClusterUserDefinedNetwork", "name", network)))
	}
	hosted := object(api.InfraProvider, "kv", m("kubevirt", m("hostClusterRef", "host", "namespace", "guests"),
		"networkAttachments", list(cudn("primary", "plain"), cudn("storage", "jumbo"), cudn("outside", "undeclared"))))
	external := object(api.InfraProvider, "kv", m("kubevirt", m("kubeconfigRef", "host-kubeconfig", "namespace", "guests"),
		"networkAttachments", list(cudn("primary", "plain"))))
	host := object(api.OpenShiftCluster, "host", m("networking", m("secondaryNetworks", list(
		m("name", "plain"), m("name", "jumbo", "mtu", api.IntegerValue("9000"))))))
	for name, test := range map[string]struct {
		provider   api.Object
		attachment string
		mtu        int64
		want       string
	}{
		"at the default":          {hosted, "primary", 1500, ""},
		"above the default":       {hosted, "primary", 9000, "secondary network plain of OpenShiftCluster/host"},
		"at a declared mtu":       {hosted, "storage", 9000, ""},
		"above a declared mtu":    {hosted, "storage", 9001, "secondary network jumbo of OpenShiftCluster/host"},
		"on an undeclared":        {hosted, "outside", 9000, ""},
		"on an external host":     {external, "primary", 9000, ""},
		"below the default (low)": {hosted, "primary", 1400, ""},
	} {
		t.Run(name, func(t *testing.T) {
			config := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet", "mtu", api.IntegerValue(fmt.Sprint(test.mtu)))))))
			machine := object(api.Machine, "node", m("substrate", m("providerRef", "kv", "kubevirt", m("profileRef", "node", "nics", list(m("nicRef", "eth0", "attachmentRef", test.attachment)))), "os", m("provided", false),
				"hardware", m("nics", list(m("name", "eth0"))), "network", m("configRef", "net")))
			catalog := api.NewCatalog([]api.Object{machine, config, test.provider, host})
			var found []api.Issue
			for _, issue := range Validate(machine, catalog) {
				if strings.Contains(issue.Message, "above its attachment MTU") {
					found = append(found, issue)
				}
			}
			if test.want == "" {
				if len(found) != 0 {
					t.Fatal(found)
				}
				return
			}
			if len(found) != 1 || found[0].Field != "$.spec.network" || !strings.Contains(found[0].Message, test.want) || !strings.Contains(found[0].Remediation, "declare an mtu that carries it") {
				t.Fatalf("issues %+v", found)
			}
		})
	}
}
