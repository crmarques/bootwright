package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func endpointsVars(state v1alpha1.State, ci v1alpha1.ClusterInstall) []any {
	out := make([]any, 0, len(ci.Endpoints))
	names := make([]string, 0, len(ci.Endpoints))
	for name := range ci.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := ci.Endpoints[name]
		entry := map[string]any{
			"name": name,
		}
		if address := endpointAddress(state, ci, name); address != "" {
			entry["address"] = address
		}
		if e.DNSName != "" {
			entry["dnsName"] = e.DNSName
		}
		if e.Port > 0 {
			entry["port"] = e.Port
		}
		if e.Scheme != "" {
			entry["scheme"] = e.Scheme
		}
		if e.PrefixLength > 0 {
			entry["prefixLength"] = e.PrefixLength
		}
		if len(e.InterfaceNetworks) > 0 {
			entry["interfaceNetworks"] = stringSliceAny(e.InterfaceNetworks)
		}
		if e.Source.Type != "" {
			source := map[string]any{"type": e.Source.Type}
			if e.Source.ComponentRef.Name != "" {
				source["componentRef"] = e.Source.ComponentRef.Name
			}
			if e.Source.BindAddress != "" {
				source["bindAddress"] = e.Source.BindAddress
			}
			entry["source"] = source
		}
		out = append(out, entry)
	}
	return out
}
