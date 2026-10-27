package cephcluster

import (
	"net/url"
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
	for _, key := range []string{"release", "cephadm", "services", "topology"} {
		a.required(spec, key, "$.spec")
	}
	ceph := spec
	a.distribution(ceph)
	a.packageRepositoryTrust(ceph)
	a.clusterAccess(ceph)
	a.topology(ceph)
	a.clusterServices(ceph)
	a.sharedServiceIdentities(a.object)
}

func (a *admission) distribution(ceph api.Value) {
	distribution := api.CephDistribution(ceph)
	if distribution == "" {
		distribution = "oss"
	}
	switch distribution {
	case "oss":
		if ceph.Get("security", "fips", "enabled").Bool() {
			a.invalid("$.spec.security.fips.enabled", "FIPS storage requires a vendor distribution", "select spec.distribution.ibm or spec.distribution.redhat, or disable spec.security.fips.enabled")
		}
	case "redhat", "ibm":
		a.required(ceph, "entitlementRef", "$.spec")
		expected := "redhatCeph"
		if distribution == "ibm" {
			expected = "ibmStorageCeph"
		}
		a.entitlement(ceph.Get("entitlementRef"), "$.spec.entitlementRef", expected)
		if distribution == "ibm" {
			for _, key := range []string{"packageVersion", "image"} {
				a.required(ceph, key, "$.spec")
			}
			a.required(ceph.Get("cephadm"), "ansible", "$.spec.cephadm")
			if ceph.Has("cephadm", "ansible") {
				a.required(ceph.Get("cephadm", "ansible"), "packageVersion", "$.spec.cephadm.ansible")
			}
			if ceph.Has("image") {
				for _, key := range []string{"base", "version"} {
					a.required(ceph.Get("image"), key, "$.spec.image")
				}
			}
			for _, entry := range ceph.Get("images").Fields() {
				for _, key := range []string{"base", "version"} {
					a.required(entry.Value, key, "$.spec.images."+entry.Name)
				}
			}
		}
		a.vendorImages(ceph)
	}
}

func (a *admission) packageRepositoryTrust(ceph api.Value) {
	if ceph.Has("packageRepository") {
		a.required(ceph, "packageRepositoryKey", "$.spec")
		if ceph.Has("packageRepositoryKey") && !samePackageOrigin(textOf(ceph, "packageRepository"), textOf(ceph, "packageRepositoryKey")) {
			a.issue("api.invariant", "$.spec.packageRepositoryKey", "the package publisher key must share the repository HTTPS scheme, host and port", "serve the unchanged IBM publisher key from the authored package repository origin")
		}
	} else if ceph.Has("packageRepositoryKey") {
		a.issue("api.invariant", "$.spec.packageRepositoryKey", "an authored package publisher key requires an authored package repository", "set spec.packageRepository or remove spec.packageRepositoryKey")
	}
	if ceph.Has("packageRepositoryTrustBundleRef") && !ceph.Has("packageRepository") {
		a.issue("api.invariant", "$.spec.packageRepositoryTrustBundleRef",
			"spec.packageRepositoryTrustBundleRef verifies only an authored spec.packageRepository, and this CephCluster sets none",
			"set spec.packageRepository to the HTTPS repository this bundle verifies, or remove spec.packageRepositoryTrustBundleRef")
	}
}

func (a *admission) vendorImages(ceph api.Value) {
	entitlement, ok := a.catalog.Find(api.Entitlement, textOf(ceph, "entitlementRef"))
	if !ok {
		return
	}
	registry := textOf(entitlement.Spec(), "registry", "url")
	if base := textOf(ceph, "image", "base"); base != "" && registry != "" && !strings.HasPrefix(base, registry+"/") {
		a.invalid("$.spec.image.base", "vendor image must be beneath its entitlement registry namespace", "set this image base beneath the registry URL of the Entitlement named by spec.entitlementRef")
	}
	for _, entry := range ceph.Get("images").Fields() {
		if base := textOf(entry.Value, "base"); base != "" && registry != "" && !strings.HasPrefix(base, registry+"/") {
			a.invalid("$.spec.images."+entry.Name+".base", "vendor image must be beneath its entitlement registry namespace", "set this image base beneath the registry URL of the Entitlement named by spec.entitlementRef")
		}
	}
}

func (a *admission) clusterAccess(ceph api.Value) {
	ssh := ceph.Get("cephadm", "clusterSSH")
	if fallback(ssh.Get("user"), "cephadm") != "root" && !ssh.Has("keyRef") {
		a.required(ssh, "keyRef", "$.spec.cephadm.clusterSSH")
	}
	if textOf(ssh, "transport") == "native" {
		a.invalid("$.spec.cephadm.clusterSSH.transport", "the native cluster SSH transport is reserved, and no build realizes it", "set spec.cephadm.clusterSSH.transport to verifiedTunnel, or omit it")
	}
	key := textOf(ssh, "keyRef")
	if key != "" {
		for _, environment := range a.catalog.OfKind(api.Environment) {
			if textOf(environment.Spec(), "remoteMachinesAccessKey", "keyRef") == key {
				a.invalid("$.spec.cephadm.clusterSSH.keyRef", "cluster SSH key must differ from the fleet install key", "set spec.cephadm.clusterSSH.keyRef to a dedicated Secret distinct from the fleet install key")
			}
		}
		for _, machine := range a.catalog.OfKind(api.Machine) {
			if textOf(machine.Spec(), "access", "ssh", "auth", "privateKeyRef") == key {
				a.invalid("$.spec.cephadm.clusterSSH.keyRef", "cluster SSH key must differ from Machine access keys", "set spec.cephadm.clusterSSH.keyRef to a dedicated Secret distinct from every Machine access key")
				break
			}
		}
		for _, cluster := range a.catalog.OfKind(api.CephCluster) {
			if cluster.Name() != a.object.Name() && textOf(cluster.Spec(), "cephadm", "clusterSSH", "keyRef") == key {
				a.invalid("$.spec.cephadm.clusterSSH.keyRef", "each storage cluster requires a distinct cluster SSH key", "set spec.cephadm.clusterSSH.keyRef to a Secret unused by other CephClusters")
				break
			}
		}
	}
	bootstrap := ceph.Get("cephadm", "bootstrap")
	if name := textOf(bootstrap, "nodeRef"); name != "" && ceph.Get("topology", "nodes").Len() > 0 {
		if node, ok := lookupNode(a.object, name); !ok {
			a.invalid("$.spec.cephadm.bootstrap.nodeRef", "bootstrap node must resolve to one storage topology node", "set spec.cephadm.bootstrap.nodeRef to one name declared in spec.topology.nodes")
		} else {
			if !nativeMember(a.object, "mon", textOf(node, "name")) {
				a.invalid("$.spec.cephadm.bootstrap.nodeRef", "bootstrap node must be eligible for the declared MON service", "select a bootstrap node included by the MON service placement or expand that placement")
			}
			if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok {
				a.machineAddress(machine, textOf(bootstrap, "addressRef"), "$.spec.cephadm.bootstrap.addressRef")
			}
		}
	}
	for i, node := range ceph.Get("topology", "nodes").Items() {
		if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok && ceph.Has("cephadm", "addressRef") {
			a.machineAddress(machine, textOf(ceph, "cephadm", "addressRef"), indexed("$.spec.topology.nodes", i)+".machineRef")
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
		a.invalid(field, "address reference must resolve to one address on the bound Machine", "set this address reference to a network and address declared on the bound Machine")
	}
}

func (a *admission) topology(ceph api.Value) {
	if ceph.Get("security", "fips", "enabled").Bool() && ceph.Get("security", "cephx", "keyType").Text() == "aes256k" {
		a.invalid("$.spec.security.cephx.keyType", "FIPS storage refuses AES256K CephX keys", "set spec.security.cephx.keyType to aes for FIPS storage")
	}
	nodes := ceph.Get("topology", "nodes").Items()
	fqdns, names, machines := []string{}, []string{}, []string{}
	for i, node := range nodes {
		field := indexed("$.spec.topology.nodes", i)
		fqdns = append(fqdns, NativeHostname(node))
		names = append(names, textOf(node, "name"))
		machines = append(machines, textOf(node, "machineRef"))
		if machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef")); ok {
			if site := textOf(machine.Spec(), "placement", "site"); site != "" && textOf(node, "site") != "" && site != textOf(node, "site") {
				a.invalid(field+".site", "storage node site must agree with its Machine placement", "set the topology node site to its Machine placement site")
			}
			if ceph.Get("security", "fips", "enabled").Bool() {
				if profile, ok := a.catalog.Find(api.MachineInstallProfile, textOf(machine.Spec(), "os", "installProfileRef")); !ok || machine.Spec().Get("os", "provided").Bool() || !profile.Spec().Get("customizations", "security", "fips", "enabled").Bool() {
					a.invalid(field+".machineRef", "FIPS storage requires a FIPS install profile on installed nodes", "select Machines installed with FIPS enabled or disable FIPS for the storage cluster")
				}
			}
		}
		a.storageSite(textOf(node, "site"), field+".site")
		for _, location := range node.Get("location").Fields() {
			if location.Name == "host" || location.Value.Type() != api.String || !nativeID.MatchString(location.Name) || !nativeID.MatchString(location.Value.Text()) {
				a.invalid(field+".location", "CRUSH location uses nonempty qualified bucket names; host identity is generated", "remove the host bucket and use nonempty qualified bucket names and values in location")
			}
			if domain := textOf(ceph, "topology", "stretch", "failureDomain"); domain != "" && location.Name == domain && location.Value.Text() != textOf(node, "site") {
				a.invalid(field+".location", "CRUSH site location must agree with the proved site", "set location."+domain+" to the topology node's proved site")
			}
		}
	}
	a.uniqueValues(fqdns, "$.spec.topology.nodes", "registered storage hostnames must be unique")
	a.uniqueValues(names, "$.spec.topology.nodes", "storage node names must be unique")
	a.uniqueValues(machines, "$.spec.topology.nodes", "one Machine cannot register as multiple storage nodes")
	if ceph.Get("cephadm", "bootstrap", "singleHostDefaults").Bool() {
		if len(nodes) != 1 || ceph.Has("topology", "stretch") {
			a.invalid("$.spec.cephadm.bootstrap.singleHostDefaults", "single-host defaults require exactly one non-stretch node", "remove spec.cephadm.bootstrap.singleHostDefaults or use exactly one topology node without stretch")
		}
	}
	if ceph.Has("topology", "stretch") {
		for i, site := range ceph.Get("topology", "stretch", "dataSites").Strings() {
			a.storageSite(site, indexed("$.spec.topology.stretch.dataSites", i))
		}
		a.storageSite(textOf(ceph, "topology", "stretch", "tiebreaker", "site"), "$.spec.topology.stretch.tiebreaker.site")
		a.deferred("$.spec.topology.stretch", "native stretch lifecycle requires the later qualified stretch slice", "stretch from $.spec.topology", "B486")
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
	a.invalid(field, "storage site must resolve to a declared Environment site", "set this site reference to a name in Environment.spec.sites")
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
		a.invalid("$.spec.topology.stretch.dataSites", "stretch requires exactly two data sites", "declare exactly two distinct sites in spec.topology.stretch.dataSites")
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
			a.invalid("$.spec.topology.stretch.dataSites", "each stretch data site requires monitor membership and capacity for two replicas", "assign MON membership and at least two OSD-bearing nodes to each stretch data site")
		}
	}
	tie := stretch.Get("tiebreaker")
	if textOf(tie, "node") == "" && textOf(tie, "site") == "" {
		a.issue("api.deferred", "$.spec.topology.stretch.tiebreaker", "native stretch activation is deferred without an independent tiebreaker", "declare an independent tiebreaker node to enable native stretch")
		return
	}
	if textOf(tie, "node") == "" {
		a.invalid("$.spec.topology.stretch.tiebreaker.node", "a populated tiebreaker requires a node", "set spec.topology.stretch.tiebreaker.node or omit tiebreaker")
		return
	}
	node, ok := lookupNode(a.object, textOf(tie, "node"))
	if !ok {
		a.invalid("$.spec.topology.stretch.tiebreaker.node", "tiebreaker must resolve to one topology node", "set spec.topology.stretch.tiebreaker.node to a declared topology node name")
		return
	}
	if !hasRole(node, "mon") || node.Get("roles").Len() != 1 {
		a.invalid("$.spec.topology.stretch.tiebreaker.node", "tiebreaker requires a monitor-only non-OSD node", "select a tiebreaker node with only the mon role and no OSD membership")
	}
	if textOf(tie, "site") == "" || textOf(node, "site") != textOf(tie, "site") || slices.Contains(sites, textOf(tie, "site")) {
		a.invalid("$.spec.topology.stretch.tiebreaker.site", "tiebreaker must occupy its node's distinct third site", "set tiebreaker.site to its node's site, distinct from both stretch dataSites")
	}
}

func samePackageOrigin(repository, key string) bool {
	origin := func(value string) string {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" {
			return ""
		}
		port := u.Port()
		if port == "" {
			port = "443"
		}
		return strings.ToLower(u.Hostname()) + ":" + port
	}
	a, b := origin(repository), origin(key)
	return a != "" && a == b
}
