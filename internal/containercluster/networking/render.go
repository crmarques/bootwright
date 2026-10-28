// Package networking compiles typed secondary-network intent into cluster objects.
package networking

import (
	"cmp"
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/machine"
)

func field(name string, v api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: v} }
func mapping(values ...api.FieldValue) api.Value    { return api.MapValue(values...) }
func text(v string) api.Value                       { return api.StringValue(v) }
func object(version, kind, name string, spec api.Value) co.Object {
	return co.Object{Ref: co.Ref{APIVersion: version, Kind: kind, Name: name}, Body: mapping(field("apiVersion", text(version)), field("kind", text(kind)), field("metadata", mapping(field("name", text(name)))), field("spec", spec))}
}
func condition(ref co.Ref, typ string) co.Check {
	return co.Check{Condition: &co.ConditionCheck{Ref: ref, Type: typ, Status: "True"}}
}
func crd(name string) co.Check {
	return condition(co.Ref{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition", Name: name}, "Established")
}

// Render returns a create-only program with NNCP readiness before any CUDN.
func Render(cluster api.Object, catalog api.Catalog) (co.Program, error) {
	networks := cluster.Spec().Get("networking", "secondaryNetworks").Items()
	program := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: []co.Check{}}
	if len(networks) == 0 {
		return program, nil
	}
	policies := co.Phase{Gate: []co.Check{crd("nodenetworkconfigurationpolicies.nmstate.io")}}
	networkPhase := co.Phase{Gate: []co.Check{crd("clusteruserdefinednetworks.k8s.ovn.org")}}
	for _, node := range cluster.Spec().Get("nodes").Items() {
		name := node.Get("name").Text()
		var native api.Value
		if bound, found := catalog.Find(api.Machine, node.Get("machineRef").Text()); found {
			native, _ = machine.ComposeNetwork(bound, catalog)
		}
		policy := nodePolicy(name, networks, native)
		policies.Objects = append(policies.Objects, policy)
		check := condition(policy.Ref, "Available")
		check.Condition.FailOn = []co.Failure{{Type: "Degraded", Status: "True"}, {Type: "Ignored", Status: "True", Reason: "NoMatchingNode"}}
		networkPhase.Gate = append(networkPhase.Gate, check)
	}
	for _, network := range networks {
		resource := userNetwork(network)
		networkPhase.Objects = append(networkPhase.Objects, resource)
		check := condition(resource.Ref, "NetworkCreated")
		check.Condition.FailOn = []co.Failure{{Type: "NetworkCreated", Status: "False", Reason: "SyncError"}}
		program.Checks = append(program.Checks, check)
	}
	for _, phase := range []co.Phase{policies, networkPhase} {
		steps, err := co.CompilePhase(phase)
		if err != nil {
			return co.Program{}, err
		}
		program.Steps = append(program.Steps, steps...)
	}
	if err := co.Validate(program, co.Options{}); err != nil {
		return co.Program{}, fmt.Errorf("secondary networks: %w", err)
	}
	return program, nil
}
func nodePolicy(node string, networks []api.Value, native api.Value) co.Object {
	interfaces := []api.Value{}
	mappings := []api.Value{}
	seen := map[string]bool{}
	for _, network := range networks {
		uplink := network.Get("uplink")
		bridge := uplink.Get("bridge").Text()
		iface := uplink.Get("interface").Text()
		if iface != "" && !seen[bridge] {
			seen[bridge] = true
			ports := []api.Value{mapping(field("name", text(iface)))}
			if nic, addressed := addressedInterface(native, iface); addressed && uplink.Get("hostAddresses").Text() == "bridge" {
				ports = append(ports, mapping(field("name", text(bridge))))
				interfaces = append(interfaces, bridgeInterface(bridge, nic), restatedUplink(nic))
			}
			interfaces = append(interfaces, mapping(field("name", text(bridge)), field("type", text("ovs-bridge")), field("state", text("up")), field("bridge", mapping(field("allow-extra-patch-ports", api.BoolValue(true)), field("options", mapping(field("stp", api.BoolValue(false)), field("mcast-snooping-enable", api.BoolValue(true)))), field("port", api.ListValue(ports...))))))
		}
		mappings = append(mappings, mapping(field("localnet", network.Get("name")), field("bridge", text(bridge)), field("state", text("present"))))
	}
	slices.SortFunc(interfaces, func(a, b api.Value) int {
		return cmp.Or(cmp.Compare(a.Get("name").Text(), b.Get("name").Text()), cmp.Compare(a.Get("type").Text(), b.Get("type").Text()))
	})
	desired := mapping(field("ovn", mapping(field("bridge-mappings", api.ListValue(mappings...)))))
	if len(interfaces) > 0 {
		desired = desired.With("interfaces", api.ListValue(interfaces...))
	}
	return object("nmstate.io/v1", "NodeNetworkConfigurationPolicy", "bootwright-"+node, mapping(field("nodeSelector", mapping(field("kubernetes.io/hostname", text(node)))), field("desiredState", desired)))
}

func addressedInterface(native api.Value, name string) (api.Value, bool) {
	for _, nic := range native.Get("interfaces").Items() {
		if nic.Get("name").Text() == name {
			return nic, nic.Get("ipv4", "address").Len()+nic.Get("ipv6", "address").Len() > 0
		}
	}
	return api.Value{}, false
}

func bridgeInterface(bridge string, uplink api.Value) api.Value {
	nic := mapping(field("name", text(bridge)), field("type", text("ovs-interface")), field("state", text("up")), field("copy-mac-from", uplink.Get("name")), field("ipv4", addressFamily(uplink.Get("ipv4", "address"), false)), field("ipv6", addressFamily(uplink.Get("ipv6", "address"), true)))
	if uplink.Has("mtu") {
		nic = nic.With("mtu", uplink.Get("mtu"))
	}
	return nic
}

func addressFamily(addresses api.Value, autoconf bool) api.Value {
	if addresses.Len() == 0 {
		return mapping(field("enabled", api.BoolValue(false)))
	}
	family := mapping(field("enabled", api.BoolValue(true)), field("dhcp", api.BoolValue(false)))
	if autoconf {
		family = family.With("autoconf", api.BoolValue(false))
	}
	return family.With("address", addresses)
}

func restatedUplink(uplink api.Value) api.Value {
	disabled := mapping(field("enabled", api.BoolValue(false)))
	return mapping(field("name", uplink.Get("name")), field("type", uplink.Get("type")), field("state", text("up")), field("ipv4", disabled), field("ipv6", disabled))
}
func userNetwork(n api.Value) co.Object {
	ipam := n.Get("ipam")
	mode := ipam.Get("mode").Text()
	if mode == "" {
		mode = "Disabled"
	}
	nativeIPAM := mapping(field("mode", text(mode)))
	if ipam.Has("lifecycle") {
		nativeIPAM = nativeIPAM.With("lifecycle", ipam.Get("lifecycle"))
	}
	localnet := mapping(field("role", text("Secondary")), field("physicalNetworkName", n.Get("name")), field("ipam", nativeIPAM))
	for _, key := range []string{"subnets", "excludeSubnets"} {
		if ipam.Has(key) {
			localnet = localnet.With(key, ipam.Get(key))
		}
	}
	if n.Has("mtu") {
		localnet = localnet.With("mtu", n.Get("mtu"))
	}
	if n.Has("vlan") {
		localnet = localnet.With("vlan", mapping(field("mode", text("Access")), field("access", mapping(field("id", n.Get("vlan"))))))
	}
	return object("k8s.ovn.org/v1", "ClusterUserDefinedNetwork", n.Get("name").Text(), mapping(field("namespaceSelector", n.Get("namespaceSelector")), field("network", mapping(field("topology", text("Localnet")), field("localnet", localnet)))))
}
