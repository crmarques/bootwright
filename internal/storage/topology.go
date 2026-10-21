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
	a.packageRepositoryTrust(ceph)
	a.clusterAccess(ceph)
	a.topology(ceph)
	a.clusterServices(ceph)
	a.sharedServiceIdentities(a.object)
}

func (a *admission) distribution(ceph api.Value) {
	distribution := fallback(ceph.Get("distribution"), "oss")
	switch distribution {
	case "oss":
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
			for _, entry := range ceph.Get("images").Fields() {
				for _, key := range []string{"base", "version"} {
					a.required(entry.Value, key, "$.spec.ceph.images."+entry.Name)
				}
			}
		}
		a.vendorImages(ceph)
	}
}

func (a *admission) packageRepositoryTrust(ceph api.Value) {
	if ceph.Has("packageRepositoryTrustBundleRef") && !ceph.Has("packageRepository") {
		a.issue("api.invariant", "$.spec.ceph.packageRepositoryTrustBundleRef",
			"spec.ceph.packageRepositoryTrustBundleRef verifies only an authored spec.ceph.packageRepository, and this StorageCluster sets none",
			"set spec.ceph.packageRepository to the HTTPS repository this bundle verifies, or remove spec.ceph.packageRepositoryTrustBundleRef")
	}
}

func (a *admission) vendorImages(ceph api.Value) {
	entitlement, ok := a.catalog.Find(api.Entitlement, textOf(ceph, "entitlementRef"))
	if !ok {
		return
	}
	registry := textOf(entitlement.Spec(), "registry", "url")
	if base := textOf(ceph, "image", "base"); base != "" && registry != "" && !strings.HasPrefix(base, registry+"/") {
		a.invalid("$.spec.ceph.image.base", "vendor image must be beneath its entitlement registry namespace")
	}
	for _, entry := range ceph.Get("images").Fields() {
		if base := textOf(entry.Value, "base"); base != "" && registry != "" && !strings.HasPrefix(base, registry+"/") {
			a.invalid("$.spec.ceph.images."+entry.Name+".base", "vendor image must be beneath its entitlement registry namespace")
		}
	}
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
			if !nativeMember(a.object, "mon", textOf(node, "name")) {
				a.invalid("$.spec.ceph.cephadm.bootstrap.node", "bootstrap node must be eligible for the declared MON service")
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
	if ceph.Get("security", "fips", "enabled").Bool() && ceph.Get("security", "cephx", "keyType").Text() == "aes256k" {
		a.invalid("$.spec.ceph.security.cephx.keyType", "FIPS storage refuses AES256K CephX keys")
	}
	nodes := ceph.Get("topology", "nodes").Items()
	fqdns, names, machines := []string{}, []string{}, []string{}
	for i, node := range nodes {
		field := indexed("$.spec.ceph.topology.nodes", i)
		fqdns = append(fqdns, NativeHostname(node))
		names = append(names, textOf(node, "name"))
		machines = append(machines, textOf(node, "machineRef"))
		if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok {
			if !contains(machine.Spec().Get("capabilities"), "ceph-node") {
				a.invalid(field+".machineRef", "storage nodes require a Machine with the ceph-node capability")
			}
			if site := textOf(machine.Spec(), "placement", "site"); site != "" && textOf(node, "site") != "" && site != textOf(node, "site") {
				a.invalid(field+".site", "storage node site must agree with its Machine placement")
			}
			if ceph.Get("security", "fips", "enabled").Bool() {
				if profile, ok := a.catalog.Find(api.MachineInstallProfile, textOf(machine.Spec(), "os", "installProfileRef")); !ok || machine.Spec().Get("os", "provided").Bool() || !profile.Spec().Get("customizations", "security", "fips", "enabled").Bool() {
					a.invalid(field+".machineRef", "FIPS storage requires a FIPS install profile on installed nodes")
				}
			}
		}
		a.storageSite(textOf(node, "site"), field+".site")
		for _, location := range node.Get("location").Fields() {
			if location.Name == "host" || location.Value.Type() != api.String || !nativeID.MatchString(location.Name) || !nativeID.MatchString(location.Value.Text()) {
				a.invalid(field+".location", "CRUSH location uses nonempty qualified bucket names; host identity is generated")
			}
			if domain := textOf(ceph, "topology", "stretch", "failureDomain"); domain != "" && location.Name == domain && location.Value.Text() != textOf(node, "site") {
				a.invalid(field+".location", "CRUSH site location must agree with the proved site")
			}
		}
	}
	a.uniqueValues(fqdns, "$.spec.ceph.topology.nodes", "registered storage hostnames must be unique")
	a.uniqueValues(names, "$.spec.ceph.topology.nodes", "storage node names must be unique")
	a.uniqueValues(machines, "$.spec.ceph.topology.nodes", "one Machine cannot register as multiple storage nodes")
	if ceph.Get("cephadm", "bootstrap", "singleHostDefaults").Bool() {
		if len(nodes) != 1 || ceph.Has("topology", "stretch") {
			a.invalid("$.spec.ceph.cephadm.bootstrap.singleHostDefaults", "single-host defaults require exactly one non-stretch node")
		}
	}
	if ceph.Has("topology", "stretch") {
		for i, site := range ceph.Get("topology", "stretch", "dataSites").Strings() {
			a.storageSite(site, indexed("$.spec.ceph.topology.stretch.dataSites", i))
		}
		a.storageSite(textOf(ceph, "topology", "stretch", "tiebreaker", "site"), "$.spec.ceph.topology.stretch.tiebreaker.site")
		a.deferred("$.spec.ceph.topology.stretch", "native stretch lifecycle requires the later qualified stretch slice", "stretch from $.spec.ceph.topology", "B486")
	}
}

func (a *admission) storageSite(site, field string) {
	if site == "" {
		return
	}
	environments := a.catalog.OfKind(api.Environment)
	if len(environments) != 1 {
		return
	}
	for _, declared := range environments[0].Spec().Get("sites").Items() {
		if textOf(declared, "name") == site {
			return
		}
	}
	a.invalid(field, "storage site must resolve to a declared Environment site")
}

func declaredDataDevices(osd api.Value) int {
	return osd.Get("dataDevices", "paths").Len() + osd.Get("dataDevices", "pathSpecs").Len()
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
