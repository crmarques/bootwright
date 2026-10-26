package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestLibvirtInterfaceAttachmentsCoverNICsAndBoundGuestMTU(t *testing.T) {
	native := m("interfaces", list(m("name", "eth0", "type", "ethernet", "mtu", api.IntegerValue("1500")), m("name", "eth1", "type", "ethernet", "mtu", api.IntegerValue("9000"))))
	provider := object(api.InfraProvider, "hypervisor", m("libvirt", m(), "networkAttachments", list(m("name", "primary", "libvirt", m()), m("name", "storage", "libvirt", m("mtu", api.IntegerValue("9000"))))))
	entries := list(m("interface", "eth0", "attachmentRef", "primary"), m("interface", "eth1", "attachmentRef", "storage"))
	for name, test := range map[string]struct {
		entries api.Value
		native  api.Value
		want    string
	}{
		"complete":       {entries, native, ""},
		"missing":        {list(entries.Items()[0]), native, "cover every"},
		"duplicate":      {list(entries.Items()[0], entries.Items()[0]), native, "attached once"},
		"unknown":        {list(entries.Items()[0], m("interface", "eth1", "attachmentRef", "absent")), native, "matching arm"},
		"oversized":      {entries, native.With("interfaces", list(native.Get("interfaces").Items()[0].With("mtu", api.IntegerValue("9000")), native.Get("interfaces").Items()[1])), "above its attachment"},
		"bond oversized": {entries, native.With("interfaces", list(append(native.Get("interfaces").Items(), m("name", "bond0", "type", "bond", "mtu", api.IntegerValue("9000"), "link-aggregation", m("port", api.StringList("eth0", "eth1"))))...)), "above its attachment"},
	} {
		t.Run(name, func(t *testing.T) {
			machine := object(api.Machine, "node", m("network", m("interfaceAttachments", test.entries)))
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
