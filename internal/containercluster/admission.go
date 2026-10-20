package containercluster

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

func Normalize(o api.Object, c api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() != api.ContainerCluster {
		return o, nil
	}
	s := o.Spec()
	distribution := normalizedDistribution(s.Get("distribution"))
	s = s.With("distribution", distribution)
	install := s.Get("install")
	if !install.Present() {
		return o.WithSpec(s), nil
	}
	s = s.With("install", normalizedInstall(o, c, install, distribution.Get("type").Text()))
	nodes := nodesWithFQDN(o, c, s.Get("nodes").Items())
	if s.Has("nodes") {
		s = s.With("nodes", api.ListValue(nodes...))
	}
	if disk := s.Get("security", "diskEncryption"); disk.Present() && !disk.Has("roles") {
		s = s.WithPath(api.StringList(declaredRoles(nodes)...), "security", "diskEncryption", "roles")
	}
	s = s.With("networking", normalizedNetworking(o, c, s.Get("networking")))
	return o.WithSpec(s), nil
}

func normalizedDistribution(distribution api.Value) api.Value {
	distribution = distribution.Default("type", api.StringValue("openshift"))
	release := distribution.Get("release")
	if image := release.Get("image"); image.Present() {
		release = release.With("image", api.StringValue(api.CanonicalImage(image.Text())))
	}
	if distribution.Get("type").Text() == "openshift" && !release.Has("image") && !release.Has("channel") {
		if channel, ok := releaseChannel(release.Get("version").Text()); ok {
			release = release.With("channel", api.StringValue(channel))
		}
	}
	if release.Present() {
		distribution = distribution.With("release", release)
	}
	return distribution
}

func normalizedInstall(o api.Object, c api.Catalog, install api.Value, distribution string) api.Value {
	if distribution == "openshift" {
		install = install.Default("pullSecretRef", api.StringValue("openshift-pull-secret"))
	}
	if !install.Has("nodeSSH") {
		install = install.With("nodeSSH", api.MapValue(api.FieldValue{Name: "keyPairRef", Value: api.StringValue(o.Name() + "-cluster-admin-ssh-key")}))
	}
	if !install.Has("platform") {
		if platform, ok := derivePlatform(o, c); ok {
			install = install.With("platform", platform)
		}
	}
	for _, side := range []string{"external", "internal"} {
		values := install.Get("platform", "vsphere", "nodeNetworking", side, "networkSubnetCidr")
		if values.Present() {
			install = install.WithPath(maskCIDRs(values), "platform", "vsphere", "nodeNetworking", side, "networkSubnetCidr")
		}
	}
	if endpoints := normalizedEndpoints(o, c, install.Get("endpoints")); endpoints.Present() {
		install = install.With("endpoints", endpoints)
	}
	install = install.With("proxy", infrastructureservices.NormalizeProxy(install.Get("proxy"), c))
	if install.Has("ntp") {
		install = install.With("ntp", infrastructureservices.NormalizeServerSelections(install.Get("ntp"), c, api.NTPServer))
	}
	if install.Has("registries", "mirror") {
		install = install.WithPath(infrastructureservices.NormalizeRegistrySelection(install.Get("registries", "mirror"), c), "registries", "mirror")
	}
	return install
}

func normalizedEndpoints(o api.Object, c api.Catalog, endpoints api.Value) api.Value {
	if !endpoints.Has("api-int") && endpoints.Has("api") {
		copy := api.MapValue()
		for _, key := range []string{"address", "source"} {
			if v := endpoints.Get("api", key); v.Present() {
				copy = copy.With(key, v)
			}
		}
		endpoints = endpoints.With("api-int", copy)
	}
	for _, slot := range endpointSlots {
		endpoint := endpoints.Get(slot)
		if !endpoint.Present() {
			continue
		}
		endpoint = endpoint.With("source", endpoint.Get("source").Default("type", api.StringValue("openshift")))
		if endpoint.Has("interfaceNetworks") {
			endpoint = endpoint.With("interfaceNetworks", maskCIDRs(endpoint.Get("interfaceNetworks")))
		}
		if !endpoint.Has("address") {
			if address, source, issues := endpointAddress(o, endpoint, c, "$.spec.install.endpoints."+slot); address.Present() && len(issues) == 0 {
				endpoint = endpoint.With("address", address)
				if source.Present() {
					endpoint = endpoint.With("source", source)
				}
			}
		}
		if addr, e := netip.ParseAddr(endpoint.Get("address").Text()); e == nil {
			endpoint = endpoint.With("address", api.StringValue(addr.String()))
		}
		endpoints = endpoints.With(slot, endpoint)
	}
	return endpoints
}

func nodesWithFQDN(o api.Object, c api.Catalog, nodes []api.Value) []api.Value {
	domain := environment.ContainerClusterDomain(c)
	for i, node := range nodes {
		if !node.Has("fqdn") && domain != "" && node.Get("name").Text() != "" {
			nodes[i] = node.With("fqdn", api.StringValue(node.Get("name").Text()+"."+o.Name()+"."+domain))
		}
	}
	return nodes
}

func declaredRoles(nodes []api.Value) []string {
	roles := []string{}
	for _, role := range []string{"master", "worker", "infra"} {
		for _, node := range nodes {
			if node.Get("role").Text() == role {
				roles = append(roles, role)
				break
			}
		}
	}
	return roles
}

func normalizedNetworking(o api.Object, c api.Catalog, networking api.Value) api.Value {
	networks, complete := machineNetworks(o, c)
	ipv6 := complete && networkFamily(networks) == 6
	clusterDefault := api.ListValue(api.MapValue(api.FieldValue{Name: "cidr", Value: api.StringValue("10.128.0.0/14")}, api.FieldValue{Name: "hostPrefix", Value: api.IntegerValue("23")}))
	serviceDefault := api.StringList("172.30.0.0/16")
	if ipv6 {
		clusterDefault = api.ListValue(api.MapValue(api.FieldValue{Name: "cidr", Value: api.StringValue("fd01::/48")}, api.FieldValue{Name: "hostPrefix", Value: api.IntegerValue("64")}))
		serviceDefault = api.StringList("fd02::/112")
	}
	if !networking.Has("clusterNetwork") || networking.Get("clusterNetwork").Type() == api.Sequence && networking.Get("clusterNetwork").Len() == 0 {
		networking = networking.With("clusterNetwork", clusterDefault)
	}
	if !networking.Has("serviceNetwork") || networking.Get("serviceNetwork").Type() == api.Sequence && networking.Get("serviceNetwork").Len() == 0 {
		networking = networking.With("serviceNetwork", serviceDefault)
	}
	clusters := networking.Get("clusterNetwork").Items()
	for i, entry := range clusters {
		if prefix, e := netip.ParsePrefix(entry.Get("cidr").Text()); e == nil {
			clusters[i] = entry.With("cidr", api.StringValue(prefix.Masked().String()))
		}
	}
	if networking.Has("clusterNetwork") {
		networking = networking.With("clusterNetwork", api.ListValue(clusters...))
	}
	return networking.With("serviceNetwork", maskCIDRs(networking.Get("serviceNetwork")))
}

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.ContainerCluster {
		return nil
	}
	issues := infrastructureservices.ValidateProxyChoice(o.Spec().Get("install", "proxy"), "$.spec.install.proxy")
	for _, slot := range endpointSlots {
		endpoint := o.Spec().Get("install", "endpoints", slot)
		kind := sourceType(endpoint)
		if endpoint.Has("address") && (kind == "loadBalancer" || kind == "node") {
			issues = add(issues, invariantRemediation("$.spec.install.endpoints."+slot+".address", "load-balancer and node endpoint sources forbid authored addresses", "remove the authored spec.install.endpoints."+slot+".address; the selected source supplies it"))
		}
	}
	return issues
}

func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.ContainerCluster {
		return nil
	}
	issues := ValidateAuthored(o, c)
	issues = add(issues, validateLocal(o, true)...)
	return issues
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.ContainerCluster {
		return nil
	}
	issues := validateLocal(o, false)
	s := o.Spec()
	install := s.Get("install")
	nodes := s.Get("nodes").Items()
	if !install.Has("platform") && len(nodes) > 1 {
		if _, ready := providerTypes(o, c); ready {
			issues = add(issues, invariantRemediation("$.spec.install.platform", "mixed provider types require an explicit installation platform", "set spec.install.platform.type and its matching configuration for the mixed node providers"))
		}
	}
	baremetal := false
	for i, node := range nodes {
		path := fmt.Sprintf("$.spec.nodes[%d].machineRef", i)
		bound, ok := c.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			continue
		}
		if !slices.Contains(bound.Spec().Get("capabilities").Strings(), "openshift-node") {
			issues = add(issues, reference(path, "container nodes require openshift-node Machine capability", "add openshift-node to spec.capabilities on "+bound.Identity()))
		}
		if provided := bound.Spec().Get("os", "provided"); provided.Present() && provided.Bool() {
			issues = add(issues, reference(path, "container installation requires a Machine whose OS is not provided", "set spec.os.provided to false on "+bound.Identity()))
		}
		if provider, ok := machine.Provider(bound, c); ok && substrate.RealizesPhysicalNICs(provider) {
			baremetal = true
		}
		configuration, configured := machine.NetworkConfig(bound, c)
		if configured && configuration.Get("dns").Len() > 0 {
			if composed, problems := machine.ComposeNetwork(bound, c); len(problems) == 0 && composed.Has("dns-resolver", "config", "server") {
				issues = add(issues, invariantRemediation(path, "node network declares both dns-resolver.config.server and selected dns name servers", "keep one of nmstate dns-resolver.config.server and dns on "+bound.Identity()+"; remove the other resolver declaration"))
			}
		}
		if !configured && !bound.Spec().Get("network").Has("configRef") && !bound.Spec().Get("network").Has("inline") {
			issues = add(issues, reference(path, "container installation requires configured Machine networking", "set spec.network.configRef or spec.network.inline on "+bound.Identity()))
		}
		if _, selectionIssues := machine.InstallAddress(bound, c); len(selectionIssues) > 0 {
			for _, issue := range selectionIssues {
				issue.Field = path
				issue.Message = "node Machine requires one eligible static installation address"
				issues = add(issues, issue)
			}
		}
	}
	if baremetal && !install.Has("agent", "redfishVirtualMedia", "artifactServerEndpoint") {
		issues = add(issues, invariantRemediation("$.spec.install.agent.redfishVirtualMedia.artifactServerEndpoint", "bare-metal nodes require a managed virtual-media artifact endpoint", "select a managed ArtifactServer endpoint in spec.install.agent.redfishVirtualMedia.artifactServerEndpoint"))
	}
	if install.Get("mode").Text() == "disconnected" {
		if !install.Has("agent", "bootArtifacts", "artifactServerEndpoint") {
			issues = add(issues, invariantRemediation("$.spec.install.agent.bootArtifacts.artifactServerEndpoint", "disconnected installation requires a managed boot-artifacts endpoint", "select a managed ArtifactServer endpoint in spec.install.agent.bootArtifacts.artifactServerEndpoint"))
		}
		mirror := install.Get("registries", "mirror")
		if !mirror.Present() {
			issues = add(issues, invariantRemediation("$.spec.install.registries.mirror", "disconnected installation requires a registry mirror selection", "set spec.install.registries.mirror to a Registry selection"))
		} else if registry, ok := c.Find(api.Registry, mirror.Get("registryRef").Text()); ok && !registry.Spec().Has("trustBundleRef") {
			issues = add(issues, invariantRemediation("$.spec.install.registries.mirror.registryRef", "disconnected installation requires trustBundleRef on the selected Registry", "set spec.trustBundleRef on the Registry selected by spec.install.registries.mirror.registryRef"))
		}
	}
	for _, consumer := range []string{"redfishVirtualMedia", "bootArtifacts"} {
		path := "$.spec.install.agent." + consumer + ".artifactServerEndpoint"
		selection := install.Get("agent", consumer, "artifactServerEndpoint")
		issues = add(issues, infrastructureservices.ValidateArtifactEndpoint(selection, c, path, false)...)
		if server, ok := infrastructureservices.ArtifactEndpoint(selection, c); ok && server.Spec().Get("management").Text() == "managed" {
			for _, node := range nodes {
				if server.Spec().Get("machineRef").Equal(node.Get("machineRef")) {
					issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".serverRef",
						Message:     "a cluster installation cannot publish through an artifact server placed on one of its nodes, because that server cannot serve until the node is installed",
						Remediation: "place " + server.Identity() + " on the controller Machine, or select a server placed there"})
					break
				}
			}
		}
	}
	issues = add(issues, infrastructureservices.ValidateProxy(install.Get("proxy"), c, "$.spec.install.proxy", false)...)
	issues = add(issues, infrastructureservices.ValidateServerSelections(install.Get("ntp"), c, api.NTPServer, "$.spec.install.ntp")...)
	issues = add(issues, infrastructureservices.ValidateRegistrySelection(install.Get("registries", "mirror"), c, "$.spec.install.registries.mirror")...)
	issues = add(issues, validateEndpoints(o, c)...)
	issues = add(issues, validateNetworks(o, c)...)
	return issues
}

var endpointSlots = []string{"api", "api-int", "ingress"}

func validateLocal(o api.Object, partial bool) []api.Issue {
	s := o.Spec()
	install := s.Get("install")
	issues := []api.Issue{}
	if len(o.Name()) > 47 {
		issues = add(issues, invariantRemediation("$.metadata.name", "ContainerCluster names must be at most 47 bytes so the installation block identity fits 63 bytes", "shorten metadata.name to at most 47 bytes"))
	}
	platform := install.Get("platform")
	if platform.Has("type") {
		for _, arm := range []string{"baremetal", "vsphere", "external"} {
			if platform.Has(arm) && platform.Get("type").Text() != arm {
				issues = add(issues, invariantRemediation("$.spec.install.platform."+arm, "platform configuration arm must match the selected platform type", "remove spec.install.platform."+arm+" or set spec.install.platform.type to "+arm))
			}
		}
	}
	issues = add(issues, validateRelease(s, partial)...)
	issues = add(issues, validateInstallChoices(install, partial)...)
	nodes := s.Get("nodes").Items()
	issues = add(issues, validateNodes(s, nodes, partial)...)
	issues = add(issues, validateEndpointSources(s, nodes, partial)...)
	for i, entry := range s.Get("networking", "clusterNetwork").Items() {
		prefix, err := netip.ParsePrefix(entry.Get("cidr").Text())
		host, ok := entry.Get("hostPrefix").Int64()
		if err == nil && ok && (host <= int64(prefix.Bits()) || host > int64(prefix.Addr().BitLen())) {
			issues = add(issues, invariantRemediation(fmt.Sprintf("$.spec.networking.clusterNetwork[%d].hostPrefix", i), "hostPrefix must be larger than the CIDR prefix and fit its address family", fmt.Sprintf("set spec.networking.clusterNetwork[%d].hostPrefix above the CIDR prefix and at most %d", i, prefix.Addr().BitLen())))
		}
	}
	return add(issues, validateInstallNames(o, install)...)
}

func validateRelease(s api.Value, partial bool) []api.Issue {
	distribution := s.Get("distribution")
	kind := distribution.Get("type").Text()
	release := distribution.Get("release")
	var issues []api.Issue
	if !partial && !release.Has("version") && !release.Has("image") {
		issues = add(issues, invariantRemediation("$.spec.distribution.release", "release requires version or a pinned image", "set spec.distribution.release.version or a pinned spec.distribution.release.image"))
	}
	if release.Has("version") && !api.ValidLexical("cluster-release", release.Get("version").Text()) {
		issues = add(issues, invariantRemediation("$.spec.distribution.release.version", "release version requires an exact major.minor.patch publisher release", "set spec.distribution.release.version to an exact publisher release, such as 4.22.0-rc.1"))
	}
	if kind == "okd" && release.Has("channel") {
		issues = add(issues, invariantRemediation("$.spec.distribution.release.channel", "release channels apply only to OpenShift", "remove spec.distribution.release.channel or select openshift in spec.distribution.type"))
	}
	if kind == "okd" && s.Get("security", "fips", "enabled").Bool() {
		issues = add(issues, invariantRemediation("$.spec.security.fips.enabled", "FIPS requires OpenShift", "disable spec.security.fips.enabled or select openshift in spec.distribution.type"))
	}
	return issues
}

func validateInstallChoices(install api.Value, partial bool) []api.Issue {
	var issues []api.Issue
	mode := install.Get("mode").Text()
	if mode == "connected" && install.Has("agent", "bootArtifacts") {
		issues = add(issues, invariantRemediation("$.spec.install.agent.bootArtifacts", "connected installation obtains boot artifacts from the release payload", "remove spec.install.agent.bootArtifacts or select disconnected in spec.install.mode"))
	}
	ssh := install.Get("nodeSSH")
	if ssh.Present() {
		if ssh.Has("keyPairRef") && (ssh.Has("publicKeyRef") || ssh.Has("privateKeyRef")) {
			issues = add(issues, invariantRemediation("$.spec.install.nodeSSH", "key-pair and split SSH references are mutually exclusive", "keep either keyPairRef or the publicKeyRef/privateKeyRef references in spec.install.nodeSSH"))
		}
		if !partial && !ssh.Has("keyPairRef") && !ssh.Has("publicKeyRef") {
			issues = add(issues, invariantRemediation("$.spec.install.nodeSSH.publicKeyRef", "split SSH material requires a public-key reference", "set spec.install.nodeSSH.publicKeyRef or use spec.install.nodeSSH.keyPairRef"))
		}
	}
	return issues
}

func validateNodes(s api.Value, nodes []api.Value, partial bool) []api.Issue {
	var issues []api.Issue
	roles := map[string]bool{}
	for i, node := range nodes {
		roles[node.Get("role").Text()] = true
		path := fmt.Sprintf("$.spec.nodes[%d]", i)
		for _, label := range node.Get("labels").Fields() {
			if !api.ValidLexical("kubernetes-label-key", label.Name) || !api.ValidLexical("kubernetes-label-value", label.Value.Text()) {
				issues = add(issues, invariantRemediation(path+".labels", "node labels require Kubernetes label keys and values", "correct the Kubernetes label keys and values in "+path+".labels"))
			}
		}
		for j, taint := range node.Get("taints").Items() {
			if (!partial || taint.Has("key")) && !api.ValidLexical("kubernetes-label-key", taint.Get("key").Text()) || taint.Has("value") && !api.ValidLexical("kubernetes-label-value", taint.Get("value").Text()) {
				issues = add(issues, invariantRemediation(fmt.Sprintf("%s.taints[%d]", path, j), "node taints require Kubernetes label keys and values", fmt.Sprintf("correct the Kubernetes key and value in %s.taints[%d]", path, j)))
			}
		}
	}
	issues = add(issues, validateTopology(s, partial)...)
	if selected := s.Get("security", "diskEncryption", "roles"); selected.Present() && (!partial || s.Has("nodes")) {
		if selected.Len() == 0 {
			issues = add(issues, invariantRemediation("$.spec.security.diskEncryption.roles", "encryption role selection must select declared nodes", "set spec.security.diskEncryption.roles to at least one role declared in spec.nodes"))
		}
		for _, role := range selected.Strings() {
			if !roles[role] {
				issues = add(issues, invariantRemediation("$.spec.security.diskEncryption.roles", "every selected encryption role must have a declared node", "keep only roles declared in spec.nodes in spec.security.diskEncryption.roles"))
			}
		}
	}
	return issues
}

func validateEndpointSources(s api.Value, nodes []api.Value, partial bool) []api.Issue {
	var issues []api.Issue
	for _, slot := range endpointSlots {
		endpoint := s.Get("install", "endpoints", slot)
		if !endpoint.Present() {
			continue
		}
		path := "$.spec.install.endpoints." + slot
		source := endpoint.Get("source")
		sourceKind := sourceType(endpoint)
		if sourceKind != "loadBalancer" && (!partial || source.Has("type")) {
			for _, key := range []string{"loadBalancerRef", "bindAddressRef"} {
				if source.Has(key) {
					issues = add(issues, invariantRemediation(path+".source."+key, "load-balancer selections require loadBalancer source", "remove "+path+".source."+key+" or set "+path+".source.type to loadBalancer"))
				}
			}
		}
		if !partial && len(nodes) == 1 && sourceKind == "openshift" {
			issues = add(issues, invariantRemediation(path+".source.type", "single-node clusters require node or external endpoint sources", "set "+path+".source.type to node or external"))
		}
		if sourceKind == "node" && s.Has("nodes") && len(nodes) != 1 {
			issues = add(issues, invariantRemediation(path+".source.type", "node endpoint source requires exactly one cluster node", "change "+path+".source.type to a source supported by the cluster topology, or declare exactly one spec.nodes entry"))
		}
		if prefix := endpoint.Get("prefixLength"); prefix.Present() {
			if !partial && !endpoint.Has("address") {
				issues = add(issues, invariantRemediation(path+".prefixLength", "endpoint prefix length requires an address", "set "+path+".address or remove "+path+".prefixLength"))
			}
			if address, err := netip.ParseAddr(endpoint.Get("address").Text()); err == nil {
				bits, ok := prefix.Int64()
				maxBits := int64(128)
				if address.Is4() {
					maxBits = 32
				}
				if ok && (bits < 1 || bits > maxBits) {
					issues = add(issues, invariantRemediation(path+".prefixLength", "endpoint prefix length must fit its IP family", fmt.Sprintf("set %s.prefixLength between 1 and %d", path, maxBits)))
				}
			}
		}
	}
	return issues
}

func validateInstallNames(o api.Object, install api.Value) []api.Issue {
	var issues []api.Issue
	internalNames := map[string]bool{"api-int": true}
	if name := install.Get("endpoints", "api-int", "dnsName").Text(); name != "" {
		internalNames[name] = true
	}
	for _, entry := range install.Get("servingCertificates", "apiServer", "namedCertificates").Items() {
		for _, name := range entry.Get("names").Strings() {
			if internalNames[name] || strings.HasPrefix(name, "api-int."+o.Name()+".") {
				issues = add(issues, invariantRemediation("$.spec.install.servingCertificates.apiServer.namedCertificates", "the internal API name cannot receive an API named certificate", "remove internal API names from spec.install.servingCertificates.apiServer.namedCertificates[].names"))
			}
		}
	}
	seenSources := map[string]bool{}
	for i, source := range install.Get("registries", "imageDigestSources").Items() {
		name := source.Get("source").Text()
		if name != "" && seenSources[name] {
			issues = add(issues, invariantRemediation(fmt.Sprintf("$.spec.install.registries.imageDigestSources[%d].source", i), "image digest sources must be unique", fmt.Sprintf("remove or combine the duplicate spec.install.registries.imageDigestSources[%d].source", i)))
		}
		seenSources[name] = true
	}
	return issues
}

func endpointAddress(o api.Object, endpoint api.Value, c api.Catalog, path string) (api.Value, api.Value, []api.Issue) {
	source := endpoint.Get("source")
	switch sourceType(endpoint) {
	case "loadBalancer":
		if !source.Has("loadBalancerRef") {
			return api.Value{}, source, []api.Issue{invariantRemediation(path+".source.loadBalancerRef", "load-balancer endpoint source requires a LoadBalancer reference", "set "+path+".source.loadBalancerRef to the selected LoadBalancer name")}
		}
		balancer, ok := c.Find(api.LoadBalancer, source.Get("loadBalancerRef").Text())
		if !ok {
			return api.Value{}, source, nil
		}
		addresses := balancer.Spec().Get("bindAddresses")
		var address api.Value
		if ref := source.Get("bindAddressRef"); ref.Present() {
			var ok bool
			address, ok = named(addresses, ref.Text())
			if !ok {
				return api.Value{}, source, []api.Issue{reference(path+".source.bindAddressRef", "bind address must resolve inside the selected load balancer", "set "+path+".source.bindAddressRef to a bindAddresses[].name on the selected LoadBalancer")}
			}
		} else if addresses.Len() == 1 {
			address = addresses.Items()[0]
			if address.Has("name") {
				source = source.With("bindAddressRef", address.Get("name"))
			}
		} else {
			return api.Value{}, source, []api.Issue{invariantRemediation(path+".source.bindAddressRef", "multiple load-balancer bind addresses require an explicit selection", "set "+path+".source.bindAddressRef to one of the selected LoadBalancer bindAddresses[].name values")}
		}
		return address.Get("address"), source, nil
	case "node":
		nodes := o.Spec().Get("nodes").Items()
		if len(nodes) != 1 {
			return api.Value{}, source, nil
		}
		bound, ok := c.Find(api.Machine, nodes[0].Get("machineRef").Text())
		if !ok {
			return api.Value{}, source, nil
		}
		address, issues := machine.InstallAddress(bound, c)
		if len(issues) > 0 {
			for i := range issues {
				issues[i].Field = path + ".source"
				issues[i].Message = "node endpoint source requires one eligible static installation address"
			}
			return api.Value{}, source, issues
		}
		if prefix, err := netip.ParsePrefix(address.Get("address").Text()); err == nil {
			return api.StringValue(prefix.Addr().String()), source, nil
		}
		return api.Value{}, source, nil
	}
	return endpoint.Get("address"), source, nil
}

func validateEndpoints(o api.Object, c api.Catalog) []api.Issue {
	issues := []api.Issue{}
	if !o.Spec().Has("install", "endpoints", "api", "address") {
		issues = add(issues, invariantRemediation("$.spec.install.endpoints.api.address", "installation requires an effective API endpoint address", "set spec.install.endpoints.api.address for an openshift or external source, or correct its node or loadBalancer source selection"))
	}
	networks, _ := machineNetworks(o, c)
	nodes := o.Spec().Get("nodes").Items()
	platform := o.Spec().Get("install", "platform", "type").Text()
	vip := len(nodes) > 1 && (platform == "baremetal" || platform == "vsphere")
	owned := 0
	for _, slot := range endpointSlots {
		if sourceType(o.Spec().Get("install", "endpoints", slot)) == "openshift" {
			owned++
		}
	}
	if len(nodes) > 1 && owned > 0 && owned < len(endpointSlots) {
		issues = add(issues, invariantRemediation("$.spec.install.endpoints", "the installer must own all three endpoint slots or none of them", "use openshift for every endpoint source, or external or loadBalancer for every source"))
	}
	if vip {
		endpoints := o.Spec().Get("install", "endpoints")
		apiAddress := endpoints.Get("api", "address").Text()
		if apiAddress != "" && endpoints.Get("api-int", "address").Text() != apiAddress {
			issues = add(issues, invariantRemediation("$.spec.install.endpoints.api-int.address", "the internal API VIP must equal the API VIP", "set api-int.address to api.address"))
		}
		if apiAddress != "" && endpoints.Get("ingress", "address").Text() == apiAddress {
			issues = add(issues, invariantRemediation("$.spec.install.endpoints.ingress.address", "the ingress VIP must differ from the API VIP", "choose a distinct ingress.address"))
		}
	}
	for _, slot := range endpointSlots {
		endpoint := o.Spec().Get("install", "endpoints", slot)
		if !endpoint.Present() {
			continue
		}
		path := "$.spec.install.endpoints." + slot
		expected, _, selectionIssues := endpointAddress(o, endpoint, c, path)
		issues = add(issues, selectionIssues...)
		source := sourceType(endpoint)
		if vip && source != "openshift" {
			issues = add(issues, invariantRemediation(path+".source.type", "the declared platform requires installer-owned endpoint VIPs", "use an openshift source or declare platform none for external or loadBalancer sources"))
		}
		if source == "node" || source == "loadBalancer" {
			if expected.Present() && endpoint.Has("address") && !expected.Equal(endpoint.Get("address")) {
				issues = add(issues, invariant(path+".address", "effective endpoint address must agree with its selected source"))
			}
		}
		if (source == "openshift" || source == "external") && !endpoint.Has("address") {
			issues = add(issues, invariantRemediation(path+".address", "an openshift or external endpoint requires an IP address; resolution before boot proves only a frozen address", "set "+path+".address to the endpoint IP literal"))
		}
		address, err := netip.ParseAddr(endpoint.Get("address").Text())
		if err != nil {
			continue
		}
		if ipv4Compatible(address) {
			issues = add(issues, invariantRemediation(path+".address", "an IPv4-compatible IPv6 endpoint address (::/96) prints differently in glibc; write the IPv4 address", "write "+path+".address as an IPv4 literal"))
		}
		if len(networks) > 0 && !contains(networks, address) {
			issues = add(issues, invariantRemediation(path+".address", "endpoint IP must belong to a consumed machine network", "set "+path+".address to an IP in the bound Machines machineNetwork"))
		}
		issues = add(issues, endpointCollisions(o, c, slot, endpoint, address)...)
		narrow := []netip.Prefix{}
		for _, raw := range endpoint.Get("interfaceNetworks").Strings() {
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				narrow = append(narrow, prefix)
			}
		}
		if len(narrow) > 0 && !contains(narrow, address) {
			issues = add(issues, invariantRemediation(path+".interfaceNetworks", "endpoint interface networks must contain the owned address", "set "+path+".interfaceNetworks to CIDRs containing "+path+".address"))
		}
	}
	return issues
}

// ipv4Compatible reports an IPv6 address in ::/96. Its frozen text is netip's
// hexadecimal form, and glibc prints much of the range in dotted form instead
// (::c000:201 as ::192.0.2.1), so resolution before boot would not see an
// answer equal the address; the whole range is refused rather than the part
// glibc happens to rewrite. An IPv4-mapped address has 0xffff in bytes 10 and
// 11, so it stays outside.
func ipv4Compatible(address netip.Addr) bool {
	bytes := address.As16()
	return address.Is6() && [12]byte(bytes[:12]) == [12]byte{}
}

func validateNetworks(o api.Object, c api.Catalog) []api.Issue {
	networks, _ := machineNetworks(o, c)
	family := networkFamily(networks)
	mixed := family == -1
	include := func(addr netip.Addr) {
		next := 6
		if addr.Is4() {
			next = 4
		}
		if family == 0 {
			family = next
		} else if family != next {
			mixed = true
		}
	}
	for _, entry := range o.Spec().Get("networking", "clusterNetwork").Items() {
		if prefix, err := netip.ParsePrefix(entry.Get("cidr").Text()); err == nil {
			include(prefix.Addr())
		}
	}
	for _, raw := range o.Spec().Get("networking", "serviceNetwork").Strings() {
		if prefix, err := netip.ParsePrefix(raw); err == nil {
			include(prefix.Addr())
		}
	}
	for _, node := range o.Spec().Get("nodes").Items() {
		if bound, ok := c.Find(api.Machine, node.Get("machineRef").Text()); ok {
			address, issues := machine.InstallAddress(bound, c)
			if prefix, err := netip.ParsePrefix(address.Get("address").Text()); len(issues) == 0 && err == nil {
				include(prefix.Addr())
			}
		}
	}
	for _, slot := range endpointSlots {
		if address, err := netip.ParseAddr(o.Spec().Get("install", "endpoints", slot, "address").Text()); err == nil {
			include(address)
		}
		for _, raw := range o.Spec().Get("install", "endpoints", slot, "interfaceNetworks").Strings() {
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				include(prefix.Addr())
			}
		}
	}
	if mixed {
		return []api.Issue{invariantRemediation("$.spec.networking", "machine, cluster, service, node, and endpoint networks must use one address family", "align spec.networking.clusterNetwork and serviceNetwork, spec.install.endpoints, and bound Machine networks to one address family")}
	}
	return nil
}

func derivePlatform(o api.Object, c api.Catalog) (api.Value, bool) {
	if o.Spec().Get("nodes").Len() == 1 {
		return api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("none")}), true
	}
	types, ready := providerTypes(o, c)
	if !ready || len(types) != 1 {
		return api.Value{}, false
	}
	switch types[0] {
	case "baremetal", "libvirt":
		owns := false
		for _, slot := range endpointSlots {
			endpoint := o.Spec().Get("install", "endpoints", slot)
			if !endpoint.Present() && slot == "api-int" {
				endpoint = o.Spec().Get("install", "endpoints", "api")
			}
			owns = owns || sourceType(endpoint) == "openshift"
		}
		if !owns {
			return api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("none")}), true
		}
		return api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("baremetal")}, api.FieldValue{Name: "baremetal", Value: api.MapValue(api.FieldValue{Name: "provisioningNetwork", Value: api.StringValue("disabled")})}), true
	case "vsphere":
		return api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("vsphere")}), true
	case "kubevirt":
		return api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("none")}), true
	}
	return api.Value{}, false
}
func providerTypes(o api.Object, c api.Catalog) ([]string, bool) {
	types := []string{}
	ready := o.Spec().Get("nodes").Len() > 0
	for _, node := range o.Spec().Get("nodes").Items() {
		bound, ok := c.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			ready = false
			continue
		}
		provider, ok := machine.Provider(bound, c)
		if !ok {
			ready = false
			continue
		}
		variant := substrate.Variant(provider)
		if variant == "" {
			ready = false
			continue
		}
		if !slices.Contains(types, variant) {
			types = append(types, variant)
		}
	}
	return types, ready
}
func machineNetworks(o api.Object, c api.Catalog) ([]netip.Prefix, bool) {
	out := []netip.Prefix{}
	complete := o.Spec().Get("nodes").Len() > 0
	for _, node := range o.Spec().Get("nodes").Items() {
		bound, ok := c.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			complete = false
			continue
		}
		config, ok := machine.NetworkConfig(bound, c)
		if !ok || config.Get("machineNetwork").Len() == 0 {
			complete = false
			continue
		}
		for _, entry := range config.Get("machineNetwork").Items() {
			if prefix, err := netip.ParsePrefix(entry.Get("cidr").Text()); err == nil {
				out = append(out, prefix.Masked())
			} else {
				complete = false
			}
		}
	}
	return out, complete
}
func networkFamily(networks []netip.Prefix) int {
	family := 0
	for _, prefix := range networks {
		next := 6
		if prefix.Addr().Is4() {
			next = 4
		}
		if family != 0 && family != next {
			return -1
		}
		family = next
	}
	return family
}
func contains(networks []netip.Prefix, address netip.Addr) bool {
	for _, network := range networks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}
func maskCIDRs(values api.Value) api.Value {
	if values.Type() != api.Sequence {
		return values
	}
	items := values.Items()
	for i, value := range items {
		if prefix, err := netip.ParsePrefix(value.Text()); err == nil {
			items[i] = api.StringValue(prefix.Masked().String())
		}
	}
	return api.ListValue(items...)
}
func sourceType(endpoint api.Value) string {
	kind := endpoint.Get("source", "type").Text()
	if kind == "" {
		return "openshift"
	}
	return kind
}
func releaseChannel(version string) (string, bool) {
	minor, ok := releaseMinor(version)
	if !ok {
		return "", false
	}
	return "stable-" + minor, true
}
func decimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func named(values api.Value, name string) (api.Value, bool) {
	var found api.Value
	count := 0
	for _, value := range values.Items() {
		if value.Get("name").Text() == name {
			found = value
			count++
		}
	}
	return found, count == 1
}
func invariant(field, message string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message}
}
func invariantRemediation(field, message, remediation string) api.Issue {
	issue := invariant(field, message)
	issue.Remediation = remediation
	return issue
}
func reference(field, message, remediation string) api.Issue {
	i := invariantRemediation(field, message, remediation)
	i.Code = "api.reference"
	return i
}
func add(issues []api.Issue, more ...api.Issue) []api.Issue {
	return append(issues, more[:min(len(more), 999-len(issues))]...)
}
