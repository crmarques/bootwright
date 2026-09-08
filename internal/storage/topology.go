package storage

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func (a *admission) cluster() {
	spec := a.object.Spec()
	management := fallback(spec.Get("management"), "managed")
	if management == "external" {
		a.forbid(spec, "$.spec", "ceph")
		return
	}
	if management != "managed" {
		return
	}
	a.required(spec, "ceph", "$.spec")
	ceph := spec.Get("ceph")
	if ceph.Type() != api.Mapping {
		return
	}
	a.distribution(ceph)
	a.clusterAccess(ceph)
	a.topology(ceph)
	a.clusterServices(ceph)
	a.sharedServiceIdentities(a.object)
}

func (a *admission) distribution(ceph api.Value) {
	distribution := fallback(ceph.Get("distribution"), "oss")
	switch distribution {
	case "oss":
		a.forbid(ceph, "$.spec.ceph", "entitlementRef", "ibm", "packageVersion")
		a.forbid(ceph.Get("cephadm", "ansible"), "$.spec.ceph.cephadm.ansible", "packageVersion")
		if ceph.Get("security", "fips", "enabled").Bool() {
			a.invalid("$.spec.ceph.security.fips.enabled", "FIPS storage requires a vendor distribution")
		}
	case "redhat", "ibm":
		a.required(ceph, "entitlementRef", "$.spec.ceph")
		expected := "redhat-ceph"
		if distribution == "ibm" {
			expected = "ibm-storage-ceph"
		}
		a.entitlement(ceph.Get("entitlementRef"), "$.spec.ceph.entitlementRef", expected)
		a.forbid(ceph, "$.spec.ceph", "community")
		if distribution == "redhat" {
			a.forbid(ceph, "$.spec.ceph", "ibm")
		}
		if distribution == "ibm" {
			for _, key := range []string{"ibm", "packageVersion", "image"} {
				a.required(ceph, key, "$.spec.ceph")
			}
			a.required(ceph.Get("cephadm"), "ansible", "$.spec.ceph.cephadm")
			if ceph.Has("cephadm", "ansible") {
				a.required(ceph.Get("cephadm", "ansible"), "packageVersion", "$.spec.ceph.cephadm.ansible")
			}
			if ceph.Has("image") {
				for _, key := range []string{"base", "version"} {
					a.required(ceph.Get("image"), key, "$.spec.ceph.image")
				}
			}
		}
		if base := textOf(ceph, "image", "base"); base != "" {
			if entitlement, ok := a.catalog.Find(api.Entitlement, textOf(ceph, "entitlementRef")); ok {
				registry := textOf(entitlement.Spec(), "registry", "url")
				if registry != "" && !strings.HasPrefix(base, registry+"/") {
					a.invalid("$.spec.ceph.image.base", "vendor image must be beneath its entitlement registry namespace")
				}
			}
		}
	}
	packages := ceph.Get("ibm", "packages")
	if packages.Present() {
		if textOf(packages, "source") == "subscription" && packages.Get("subscriptionRepos").Len() == 0 {
			a.invalid("$.spec.ceph.ibm.packages.subscriptionRepos", "subscription package source requires at least one repository")
		}
		if textOf(packages, "source") == "vendor" {
			a.forbid(packages, "$.spec.ceph.ibm.packages", "subscriptionRepos")
		}
	}
	a.entitlement(ceph.Get("osSubscriptionRef"), "$.spec.ceph.osSubscriptionRef", "redhat-rhel")
}

func (a *admission) clusterAccess(ceph api.Value) {
	ssh := ceph.Get("cephadm", "clusterSSH")
	if fallback(ssh.Get("user"), "cephadm") != "root" && !ssh.Has("keyRef") {
		a.required(ssh, "keyRef", "$.spec.ceph.cephadm.clusterSSH")
	}
	key := textOf(ssh, "keyRef")
	if key != "" {
		for _, environment := range a.catalog.OfKind(api.Environment) {
			if textOf(environment.Spec(), "remoteMachinesAccessKey", "keyRef") == key {
				a.invalid("$.spec.ceph.cephadm.clusterSSH.keyRef", "cluster SSH key must differ from the fleet install key")
			}
		}
		for _, machine := range a.catalog.OfKind(api.Machine) {
			if textOf(machine.Spec(), "access", "ssh", "auth", "privateKeyRef") == key {
				a.invalid("$.spec.ceph.cephadm.clusterSSH.keyRef", "cluster SSH key must differ from Machine access keys")
				break
			}
		}
		for _, cluster := range a.catalog.OfKind(api.StorageCluster) {
			if cluster.Name() != a.object.Name() && textOf(cluster.Spec(), "ceph", "cephadm", "clusterSSH", "keyRef") == key {
				a.invalid("$.spec.ceph.cephadm.clusterSSH.keyRef", "each storage cluster requires a distinct cluster SSH key")
				break
			}
		}
	}
	bootstrap := ceph.Get("cephadm", "bootstrap")
	if name := textOf(bootstrap, "node"); name != "" && ceph.Get("topology", "nodes").Len() > 0 {
		if node, ok := lookupNode(a.object, name); !ok {
			a.invalid("$.spec.ceph.cephadm.bootstrap.node", "bootstrap node must resolve to one storage topology node")
		} else {
			if !hasRole(node, "mon") {
				a.invalid("$.spec.ceph.cephadm.bootstrap.node", "bootstrap node requires the monitor role")
			}
			if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok {
				a.machineAddress(machine, textOf(bootstrap, "addressRef"), "$.spec.ceph.cephadm.bootstrap.addressRef")
			}
		}
	}
	for i, node := range ceph.Get("topology", "nodes").Items() {
		if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok && ceph.Has("cephadm", "addressRef") {
			a.machineAddress(machine, textOf(ceph, "cephadm", "addressRef"), indexed("$.spec.ceph.topology.nodes", i)+".machineRef")
		}
	}
}
func (a *admission) machineAddress(machine api.Object, name, field string) {
	if name == "" || machine.Spec().Get("network", "addresses").Len() == 0 {
		return
	}
	count := 0
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		if textOf(address, "name") == name {
			count++
		}
	}
	if count != 1 {
		a.invalid(field, "address reference must resolve to one address on the bound Machine")
	}
}

func (a *admission) topology(ceph api.Value) {
	nodes := ceph.Get("topology", "nodes").Items()
	if len(nodes) == 0 {
		return
	}
	owners := map[string]int{}
	fqdns := []string{}
	for i, node := range nodes {
		field := indexed("$.spec.ceph.topology.nodes", i)
		fqdns = append(fqdns, textOf(node, "fqdn"))
		if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok {
			if !contains(machine.Spec().Get("capabilities"), "ceph-node") {
				a.invalid(field+".machineRef", "storage nodes require a Machine with the ceph-node capability")
			}
			if site := textOf(machine.Spec(), "placement", "site"); site != "" && textOf(node, "site") != "" && site != textOf(node, "site") {
				a.invalid(field+".site", "storage node site must agree with its Machine placement")
			}
			if ceph.Get("security", "fips", "enabled").Bool() && machine.Spec().Has("os", "installProfileRef") {
				if profile, ok := a.catalog.Find(api.MachineInstallProfile, textOf(machine.Spec(), "os", "installProfileRef")); ok && !profile.Spec().Get("customizations", "security", "fips", "enabled").Bool() {
					a.invalid(field+".machineRef", "FIPS storage requires a FIPS install profile on installed nodes")
				}
			}
		}
		for _, label := range node.Get("labels").Strings() {
			if hasRole(node, label) {
				a.invalid(field+".labels", "additional labels must be disjoint from topology roles")
			}
		}
		if node.Has("devices") && node.Has("osd") {
			a.invalid(field+".osd", "device shorthand and per-node OSD configuration are mutually exclusive")
		}
		if node.Get("devices").Len() > 0 || node.Has("osd") {
			owners[textOf(node, "name")]++
			if !hasRole(node, "osd") {
				a.invalid(field, "an OSD declaration requires the node's OSD role")
			}
			osd := node.Get("osd")
			if node.Get("devices").Len() > 0 {
				osd = api.MapValue(api.FieldValue{Name: "dataDevices", Value: api.MapValue(api.FieldValue{Name: "paths", Value: node.Get("devices")})})
			}
			a.osd(osd, field+".osd", []api.Value{node})
		}
	}
	a.uniqueValues(fqdns, "$.spec.ceph.topology.nodes", "storage-node FQDNs must be unique")
	groups := ceph.Get("topology", "osdDrivegroups").Items()
	ids := []string{}
	for i, group := range groups {
		field := indexed("$.spec.ceph.topology.osdDrivegroups", i)
		ids = append(ids, textOf(group, "serviceID"))
		placed := a.placement(a.object, group.Get("placement"), field+".placement", "osd", false)
		for _, node := range placed {
			owners[textOf(node, "name")]++
		}
		a.osd(group.Get("osd"), field+".osd", placed)
	}
	a.uniqueValues(ids, "$.spec.ceph.topology.osdDrivegroups", "OSD drivegroup service IDs must be unique")
	for i, node := range nodes {
		if hasRole(node, "osd") && owners[textOf(node, "name")] != 1 {
			a.invalid(indexed("$.spec.ceph.topology.nodes", i), "each OSD host must have exactly one OSD declaration")
		}
	}
	if ceph.Get("cephadm", "bootstrap", "singleHostDefaults").Bool() {
		if len(nodes) != 1 || ceph.Has("topology", "stretch") {
			a.invalid("$.spec.ceph.cephadm.bootstrap.singleHostDefaults", "single-host defaults require exactly one non-stretch node")
		} else {
			count := nodes[0].Get("devices").Len()
			count += declaredDataDevices(nodes[0].Get("osd"))
			for _, group := range groups {
				count += declaredDataDevices(group.Get("osd"))
			}
			if count < 2 {
				a.invalid("$.spec.ceph.cephadm.bootstrap.singleHostDefaults", "single-host defaults require at least two declared OSDs")
			}
		}
	}
	a.stretch(ceph)
}
func declaredDataDevices(osd api.Value) int {
	return osd.Get("dataDevices", "paths").Len() + osd.Get("dataDevices", "pathSpecs").Len()
}

func (a *admission) placement(cluster api.Object, placement api.Value, field, role string, explicit bool) []api.Value {
	hosts, sites := placement.Get("hosts").Strings(), placement.Get("sites").Strings()
	if explicit && len(hosts) == 0 && len(sites) == 0 {
		a.invalid(field, "this service requires explicit hosts or sites")
		return nil
	}
	selected := []api.Value{}
	if cluster.Spec().Get("ceph", "topology", "nodes").Len() == 0 {
		return selected
	}
	for _, name := range hosts {
		if _, ok := lookupNode(cluster, name); !ok {
			a.invalid(field+".hosts", "placement host must resolve to a storage topology node")
		}
	}
	for _, site := range sites {
		found := false
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			if textOf(node, "site") == site {
				found = true
			}
		}
		if !found {
			a.invalid(field+".sites", "placement site must contain a storage topology node")
		}
	}
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		if len(hosts) > 0 && !slices.Contains(hosts, textOf(node, "name")) {
			continue
		}
		if len(sites) > 0 && !slices.Contains(sites, textOf(node, "site")) {
			if len(hosts) > 0 {
				a.invalid(field, "every explicit placement host must belong to a selected site")
			}
			continue
		}
		if role != "" && !hasRole(node, role) {
			if len(hosts) > 0 {
				a.invalid(field+".hosts", "placement host does not have the service's required role")
			}
			continue
		}
		selected = append(selected, node)
	}
	if role != "" && len(selected) == 0 {
		a.invalid(field, "no topology node has the service's required placement role")
	}
	return selected
}

func (a *admission) stretch(ceph api.Value) {
	stretch := ceph.Get("topology", "stretch")
	if !stretch.Present() {
		return
	}
	sites := stretch.Get("dataSites").Strings()
	if len(sites) != 2 {
		a.invalid("$.spec.ceph.topology.stretch.dataSites", "stretch requires exactly two data sites")
	}
	for _, site := range sites {
		mon, osd := 0, 0
		for _, node := range ceph.Get("topology", "nodes").Items() {
			if textOf(node, "site") == site {
				if hasRole(node, "mon") {
					mon++
				}
				if hasRole(node, "osd") {
					osd++
				}
			}
		}
		if mon == 0 || osd < 2 {
			a.invalid("$.spec.ceph.topology.stretch.dataSites", "each stretch data site requires monitor membership and capacity for two replicas")
		}
	}
	tie := stretch.Get("tiebreaker")
	if textOf(tie, "node") == "" && textOf(tie, "site") == "" {
		a.issue("api.deferred", "$.spec.ceph.topology.stretch.tiebreaker", "native stretch activation is deferred without an independent tiebreaker", "declare an independent tiebreaker node to enable native stretch")
		return
	}
	if textOf(tie, "node") == "" {
		a.invalid("$.spec.ceph.topology.stretch.tiebreaker.node", "a populated tiebreaker requires a node")
		return
	}
	node, ok := lookupNode(a.object, textOf(tie, "node"))
	if !ok {
		a.invalid("$.spec.ceph.topology.stretch.tiebreaker.node", "tiebreaker must resolve to one topology node")
		return
	}
	if !hasRole(node, "mon") || node.Get("roles").Len() != 1 {
		a.invalid("$.spec.ceph.topology.stretch.tiebreaker.node", "tiebreaker requires a monitor-only non-OSD node")
	}
	if textOf(tie, "site") == "" || textOf(node, "site") != textOf(tie, "site") || slices.Contains(sites, textOf(tie, "site")) {
		a.invalid("$.spec.ceph.topology.stretch.tiebreaker.site", "tiebreaker must occupy its node's distinct third site")
	}
}
