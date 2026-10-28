package cephcluster

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
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
		if node, ok := lookupNode(a.object, name); ok && !nativeMember(a.object, "mon", textOf(node, "name")) {
			a.invalid("$.spec.cephadm.bootstrap.nodeRef", "bootstrap node must be eligible for the declared MON service", "select a bootstrap node included by the MON service placement or expand that placement")
		}
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
			provided := machine.Spec().Get("os", "provided").Bool()
			if ceph.Get("security", "fips", "enabled").Bool() && !provided {
				if profile, ok := a.catalog.Find(api.MachineInstallProfile, textOf(machine.Spec(), "os", "installProfileRef")); !ok || !profile.Spec().Get("customizations", "security", "fips", "enabled").Bool() {
					a.invalid(field+".machineRef", "FIPS storage requires a FIPS install profile on installed nodes", "select Machines installed with FIPS enabled or disable FIPS for the storage cluster")
				}
			}
		}
		a.storageSite(textOf(node, "site"), field+".site")
		a.location(ceph, node, field)
	}
	a.monNetworks(ceph)
	a.uniqueValues(fqdns, "$.spec.topology.nodes", "registered storage hostnames must be unique")
	a.uniqueValues(names, "$.spec.topology.nodes", "storage node names must be unique")
	a.uniqueValues(machines, "$.spec.topology.nodes", "one Machine cannot register as multiple storage nodes")
	if ceph.Get("cephadm", "bootstrap", "singleHostDefaults").Bool() {
		if len(nodes) != 1 || ceph.Has("topology", "stretch") {
			a.invalid("$.spec.cephadm.bootstrap.singleHostDefaults", "single-host defaults require exactly one non-stretch node", "remove spec.cephadm.bootstrap.singleHostDefaults or use exactly one topology node without stretch")
		}
	}
	a.stretch(ceph)
}

func (a *admission) location(ceph api.Value, node api.Value, field string) {
	domain := textOf(ceph, "topology", "stretch", "failureDomain")
	for _, location := range node.Get("location").Fields() {
		if location.Name == "host" || location.Value.Type() != api.String || !nativeID.MatchString(location.Name) || !nativeID.MatchString(location.Value.Text()) {
			a.invalid(field+".location", "CRUSH location uses nonempty qualified bucket names; host identity is generated", "remove the host bucket and use nonempty qualified bucket names and values in location")
			continue
		}
		switch {
		case domain == "":
			a.invalid(field+".location."+location.Name, "a host CRUSH location is admitted only on a stretch topology, whose failure domain it names", "remove topology.nodes[].location; host-domain placement needs none")
		case location.Name != domain:
			a.invalid(field+".location."+location.Name, "a host CRUSH location names only the stretch failure domain "+domain+", which the node's site already gives", "remove topology.nodes[].location."+location.Name+"; each data host is placed under its site's "+domain+" bucket")
		case textOf(node, "name") == textOf(ceph, "topology", "stretch", "tiebreaker", "node"):
			a.invalid(field+".location."+location.Name, "the tiebreaker holds no OSD, so it has no host CRUSH location; its MON is located by the stretch topology", "remove topology.nodes[].location from the tiebreaker node")
		case location.Value.Text() != textOf(node, "site"):
			a.invalid(field+".location", "CRUSH site location must agree with the proved site", "set location."+domain+" to the topology node's proved site")
		}
	}
}

func (a *admission) monNetworks(ceph api.Value) {
	cidrs := ceph.Get("networks", "publicCIDRs").Strings()
	if len(cidrs) == 0 {
		return
	}
	for i, node := range ceph.Get("topology", "nodes").Items() {
		m, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !ok || !nativeMember(a.object, "mon", textOf(node, "name")) {
			continue
		}
		reference := textOf(ceph, "cephadm", "addressRef")
		if reference == "" {
			reference = textOf(m.Spec(), "access", "ssh", "addressRef")
		}
		address, declared := machine.Address(m, reference)
		ip, err := netip.ParseAddr(address)
		if !declared || err != nil {
			continue
		}
		inside := false
		for _, cidr := range cidrs {
			if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Masked().Contains(ip) {
				inside = true
			}
		}
		if !inside {
			a.invalid(indexed("$.spec.topology.nodes", i)+".machineRef", "MON host "+NativeHostname(node)+" address "+address+" lies inside no public CIDR "+strings.Join(cidrs, ", ")+", so cephadm cannot place its MON",
				"add the CIDR that holds "+address+" to spec.networks.publicCIDRs, or address Machine/"+m.Name()+" inside one of them")
		}
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

var stretchDomains = []string{"chassis", "rack", "row", "pdu", "pod", "room", "datacenter", "zone", "region"}

func (a *admission) stretch(ceph api.Value) {
	stretch := ceph.Get("topology", "stretch")
	if !stretch.Present() {
		return
	}
	sites := stretch.Get("dataSites").Strings()
	for i, site := range sites {
		a.storageSite(site, indexed("$.spec.topology.stretch.dataSites", i))
	}
	tie := stretch.Get("tiebreaker")
	a.storageSite(textOf(tie, "site"), "$.spec.topology.stretch.tiebreaker.site")
	if domain := textOf(stretch, "failureDomain"); domain != "" && !slices.Contains(stretchDomains, domain) {
		a.invalid("$.spec.topology.stretch.failureDomain", "the stretch failure domain "+domain+" is not a native default CRUSH bucket type above host", "set spec.topology.stretch.failureDomain to one of "+strings.Join(stretchDomains, ", ")+"; datacenter is the usual choice")
	}
	if len(sites) != 2 {
		a.invalid("$.spec.topology.stretch.dataSites", "stretch requires exactly two data sites", "declare exactly two distinct sites in spec.topology.stretch.dataSites")
	}
	if textOf(tie, "node") == "" {
		a.invalid("$.spec.topology.stretch.tiebreaker.node", "native stretch mode needs a tiebreaker MON at a third site", "set spec.topology.stretch.tiebreaker.node to a topology node at a site other than both data sites")
		return
	}
	node, ok := lookupNode(a.object, textOf(tie, "node"))
	if !ok {
		a.invalid("$.spec.topology.stretch.tiebreaker.node", "tiebreaker must resolve to one topology node", "set spec.topology.stretch.tiebreaker.node to a declared topology node name")
		return
	}
	tiebreaker := textOf(node, "name")
	if textOf(tie, "site") == "" || textOf(node, "site") != textOf(tie, "site") || slices.Contains(sites, textOf(tie, "site")) {
		a.invalid("$.spec.topology.stretch.tiebreaker.site", "tiebreaker must occupy its node's distinct third site", "set tiebreaker.site to its node's site, distinct from both stretch dataSites")
	}
	if !nativeMember(a.object, "mon", tiebreaker) {
		a.invalid("$.spec.topology.stretch.tiebreaker.node", "the tiebreaker node must be eligible for the declared MON service", "include the tiebreaker node in the mon service placement")
	}
	if textOf(ceph, "cephadm", "bootstrap", "nodeRef") == tiebreaker {
		a.invalid("$.spec.cephadm.bootstrap.nodeRef", "the bootstrap node runs the first MGR, and the tiebreaker runs a MON alone", "select a data-site node as spec.cephadm.bootstrap.nodeRef")
	}
	for i, service := range ceph.Get("services").Items() {
		kind := textOf(service, "service_type")
		names, issues := ResolveServicePlacement(a.object, service)
		if kind != "mon" && len(issues) == 0 && slices.Contains(names, tiebreaker) {
			a.invalid(indexed("$.spec.services", i)+".placement", "the "+kind+" service places on the stretch tiebreaker "+tiebreaker+", which runs a MON alone", "narrow this service's placement to data-site nodes")
		}
	}
	for i, node := range ceph.Get("topology", "nodes").Items() {
		if site := textOf(node, "site"); textOf(node, "name") != tiebreaker && !slices.Contains(sites, site) {
			a.invalid(indexed("$.spec.topology.nodes", i)+".site", "a stretch node occupies one of the two data sites, or is the tiebreaker", "place this node's Machine at one of spec.topology.stretch.dataSites")
		}
	}
	for _, site := range sites {
		mon, osd := 0, 0
		for _, node := range ceph.Get("topology", "nodes").Items() {
			if textOf(node, "site") != site {
				continue
			}
			if nativeMember(a.object, "mon", textOf(node, "name")) {
				mon++
			}
			if nativeMember(a.object, "osd", textOf(node, "name")) {
				osd++
			}
		}
		if mon == 0 || osd < 2 {
			a.invalid("$.spec.topology.stretch.dataSites", "stretch data site "+site+" needs a MON and at least two OSD hosts, which hold its two replicas", "place MON membership and at least two OSD-bearing nodes at site "+site)
		}
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
