// Package networking compiles typed secondary-network intent into cluster objects.
package networking

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
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
func Render(cluster api.Object) (co.Program, error) {
	networks := cluster.Spec().Get("networking", "secondaryNetworks").Items()
	program := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: []co.Check{}}
	if len(networks) == 0 {
		return program, nil
	}
	policies := co.Phase{Gate: []co.Check{crd("nodenetworkconfigurationpolicies.nmstate.io")}}
	networkPhase := co.Phase{Gate: []co.Check{crd("clusteruserdefinednetworks.k8s.ovn.org")}}
	for _, node := range cluster.Spec().Get("nodes").Items() {
		name := node.Get("name").Text()
		policy := nodePolicy(name, networks)
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
func nodePolicy(node string, networks []api.Value) co.Object {
	interfaces := []api.Value{}
	mappings := []api.Value{}
	seen := map[string]bool{}
	for _, network := range networks {
		uplink := network.Get("uplink")
		bridge := uplink.Get("bridge").Text()
		iface := uplink.Get("interface").Text()
		if iface != "" && !seen[bridge] {
			seen[bridge] = true
			interfaces = append(interfaces, mapping(field("name", text(bridge)), field("type", text("ovs-bridge")), field("state", text("up")), field("bridge", mapping(field("allow-extra-patch-ports", api.BoolValue(true)), field("options", mapping(field("stp", api.BoolValue(false)), field("mcast-snooping-enable", api.BoolValue(true)))), field("port", api.ListValue(mapping(field("name", text(iface)))))))))
		}
		mappings = append(mappings, mapping(field("localnet", network.Get("name")), field("bridge", text(bridge)), field("state", text("present"))))
	}
	slices.SortFunc(interfaces, func(a, b api.Value) int {
		if a.Get("name").Text() < b.Get("name").Text() {
			return -1
		}
		return 1
	})
	desired := mapping(field("ovn", mapping(field("bridge-mappings", api.ListValue(mappings...)))))
	if len(interfaces) > 0 {
		desired = desired.With("interfaces", api.ListValue(interfaces...))
	}
	return object("nmstate.io/v1", "NodeNetworkConfigurationPolicy", "bootwright-"+node, mapping(field("nodeSelector", mapping(field("kubernetes.io/hostname", text(node)))), field("desiredState", desired)))
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
