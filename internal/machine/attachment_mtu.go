package machine

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

type mtuBound struct {
	maximum      int64
	note, remedy string
}

func validateLibvirtAttachmentMTUs(c api.Catalog, o, provider api.Object, native api.Value) []api.Issue {
	return validateAttachmentMTUs(c, o, provider, native, func(attachment api.Value) mtuBound {
		mtu, ok := substrate.AttachmentMTU(attachment)
		if !ok {
			mtu = 1500
		}
		return mtuBound{mtu, "", "declare an attachment or managed Network mtu that carries it on " + provider.Identity()}
	})
}

func validateKubeVirtAttachmentMTUs(c api.Catalog, o, provider, host api.Object, native api.Value) []api.Issue {
	return validateAttachmentMTUs(c, o, provider, native, func(attachment api.Value) mtuBound {
		name, mtu, ok := substrate.HostNetworkMTU(host, attachment)
		if !ok {
			return mtuBound{65536, "", ""}
		}
		return mtuBound{mtu, ", the MTU of secondary network " + name + " of " + host.Identity(), "declare an mtu that carries it on that secondary network"}
	})
}

func validateAttachmentMTUs(c api.Catalog, o, provider api.Object, native api.Value, bound func(attachment api.Value) mtuBound) []api.Issue {
	var limit func(string, map[string]bool) mtuBound
	limit = func(name string, visiting map[string]bool) mtuBound {
		if visiting[name] {
			return mtuBound{1500, "", ""}
		}
		visiting[name] = true
		defer delete(visiting, name)
		iface, _ := namedValue(native.Get("interfaces"), name)
		if parent := iface.Get("vlan", "base-iface").Text(); parent != "" {
			return limit(parent, visiting)
		}
		if ports := iface.Get("link-aggregation", "port").Strings(); len(ports) > 0 {
			narrowest := mtuBound{65536, "", ""}
			for _, port := range ports {
				if held := limit(port, visiting); held.maximum < narrowest.maximum {
					narrowest = held
				}
			}
			return narrowest
		}
		reference := substrate.NICAttachment(c, provider, o, substrate.InterfaceNIC(o, name))
		attachment, found := namedValue(substrate.Segments(c, provider), reference)
		if reference != "" && !found {
			return mtuBound{65536, "", ""}
		}
		return bound(attachment)
	}
	var issues []api.Issue
	for _, iface := range native.Get("interfaces").Items() {
		if mtu, ok := iface.Get("mtu").Int64(); ok && !unavailableInterface(iface) {
			held := limit(iface.Get("name").Text(), map[string]bool{})
			if mtu > held.maximum {
				remedy := held.remedy
				if remedy == "" {
					remedy = "declare an attachment mtu that carries it on " + provider.Identity()
				}
				issues = append(issues, invariant("$.spec.network", fmt.Sprintf("interface %s declares MTU %d above its attachment MTU %d%s", iface.Get("name").Text(), mtu, held.maximum, held.note),
					"lower the interface mtu in the network configuration or "+remedy))
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
