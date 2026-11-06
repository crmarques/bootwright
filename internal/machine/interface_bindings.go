package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type InterfaceBinding struct {
	NIC       string
	Interface string
}

func InterfaceBindings(o api.Object, c api.Catalog) []InterfaceBinding {
	native, issues := ComposeNetwork(o, c)
	if len(issues) != 0 || !native.Present() {
		return nil
	}
	bindings, issues := resolveBindings(o, c, native)
	if len(issues) != 0 {
		return nil
	}
	resolved := make([]InterfaceBinding, 0, bindings.Len())
	for _, binding := range bindings.Items() {
		resolved = append(resolved, InterfaceBinding{NIC: binding.Get("nicRef").Text(), Interface: binding.Get("interfaceName").Text()})
	}
	return resolved
}
