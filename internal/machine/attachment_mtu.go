package machine

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func validateLibvirtAttachmentMTUs(o, provider api.Object, native api.Value) []api.Issue {
	network := o.Spec().Get("network")
	var limit func(string, map[string]bool) int64
	limit = func(name string, visiting map[string]bool) int64 {
		if visiting[name] {
			return 1500
		}
		visiting[name] = true
		defer delete(visiting, name)
		iface, _ := namedValue(native.Get("interfaces"), name)
		if parent := iface.Get("vlan", "base-iface").Text(); parent != "" {
			return limit(parent, visiting)
		}
		if ports := iface.Get("link-aggregation", "port").Strings(); len(ports) > 0 {
			mtu := int64(65536)
			for _, port := range ports {
				mtu = min(mtu, limit(port, visiting))
			}
			return mtu
		}
		reference := network.Get("attachmentRef").Text()
		if network.Has("interfaceAttachments") {
			entry, _ := namedValueBy(network.Get("interfaceAttachments"), "interface", name)
			reference = entry.Get("attachmentRef").Text()
		}
		attachment, _ := namedValue(provider.Spec().Get("networkAttachments"), reference)
		if mtu, ok := substrate.AttachmentMTU(attachment); ok {
			return mtu
		}
		return 1500
	}
	var issues []api.Issue
	for _, iface := range native.Get("interfaces").Items() {
		if mtu, ok := iface.Get("mtu").Int64(); ok && !unavailableInterface(iface) {
			maximum := limit(iface.Get("name").Text(), map[string]bool{})
			if mtu > maximum {
				issues = append(issues, invariant("$.spec.network", fmt.Sprintf("interface %s declares MTU %d above its attachment MTU %d", iface.Get("name").Text(), mtu, maximum),
					"lower the interface mtu in the network configuration or declare an attachment mtu that carries it on "+provider.Identity()))
			}
		}
	}
	return issues
}

func namedValueBy(values api.Value, key, name string) (api.Value, bool) {
	for _, value := range values.Items() {
		if value.Get(key).Text() == name {
			return value, true
		}
	}
	return api.Value{}, false
}
