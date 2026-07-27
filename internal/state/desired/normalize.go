package desiredstate

import (
	"net"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
)

const (
	defaultClusterNetworkCIDRIPv6       = "fd01::/48"
	defaultClusterNetworkHostPrefixIPv6 = 64
	defaultServiceNetworkCIDRIPv6       = "fd02::/112"
)

func Normalize(state *v1alpha1.State) {
	for i := range state.Environments {
		normalizeEnvironment(&state.Environments[i])
	}
	for i := range state.Entitlements {
		normalizeEntitlementSatellite(state.Entitlements[i].Spec.RHSM)
	}
	normalizeSecrets(state)
	for i := range state.Machines {
		normalizeMachine(&state.Machines[i])
	}
	normalizeMachineFQDNs(state)
	for i := range state.InfraProviders {
		normalizeProvider(&state.InfraProviders[i])
	}
	for i := range state.InfraComponents {
		normalizeInfraComponent(&state.InfraComponents[i])
	}
	env := primaryEnvironment(state)
	for i := range state.ContainerClusters {
		normalizeContainerCluster(&state.ContainerClusters[i], env)
	}
	applyClusterPlatformDefaults(state)
	applyClusterNetworkDefaults(state)
	applyBareMetalBMCDefaults(state)
	for i := range state.ClusterAddons {
		normalizeClusterAddon(&state.ClusterAddons[i])
	}
	for i := range state.StorageClusters {
		normalizeStorageCluster(&state.StorageClusters[i], *state)
	}
	normalizeNodeNames(state)
	normalizeStorageDNSAliases(state)
	for i := range state.StoragePools {
		normalizeStoragePool(&state.StoragePools[i])
	}
	for i := range state.StorageFilesystems {
		normalizeStorageFilesystem(&state.StorageFilesystems[i])
	}
	for i := range state.StorageObjectGateways {
		normalizeStorageObjectGateway(&state.StorageObjectGateways[i])
	}
	for i := range state.StorageExports {
		normalizeStorageExport(&state.StorageExports[i])
	}
	for i := range state.Playbooks {
		normalizePlaybook(&state.Playbooks[i])
	}
}

func normalizePlaybook(p *v1alpha1.Playbook) {
	if p.Spec.Run == "" {
		p.Spec.Run = v1alpha1.PlaybookRunOnChange
	}
	if p.Spec.OnFailure == "" {
		p.Spec.OnFailure = v1alpha1.PlaybookFailureFail
	}
	if p.Spec.Enabled == nil {
		enabled := true
		p.Spec.Enabled = &enabled
	}
}

func applyBareMetalBMCDefaults(state *v1alpha1.State) {
	for i := range state.Machines {
		bmc := &state.Machines[i].Spec.Hardware.Management.BMC
		if bmc.Address == "" {
			continue
		}
		provider, ok := stateview.Provider(*state, state.Machines[i].Spec.Substrate.ProviderRef.Name)
		if !ok || provider.Spec.Type != v1alpha1.ProvisionerBareMetal || provider.Spec.BareMetal == nil {
			continue
		}
		d := provider.Spec.BareMetal.Defaults.BMC
		if d == nil {
			continue
		}
		if d.TLS != nil && d.TLS.Verify != nil {
			if bmc.TLS == nil {
				bmc.TLS = &v1alpha1.BMCTLS{}
			}
			if bmc.TLS.Verify == nil {
				v := *d.TLS.Verify
				bmc.TLS.Verify = &v
			}
		}
		if bmc.VirtualMedia == nil && d.VirtualMedia != nil {
			bmc.VirtualMedia = deepCopyBMCVirtualMedia(d.VirtualMedia)
		}
	}
}

func deepCopyBMCVirtualMedia(src *v1alpha1.BMCVirtualMedia) *v1alpha1.BMCVirtualMedia {
	if src == nil {
		return nil
	}
	out := &v1alpha1.BMCVirtualMedia{}
	if src.TLS != nil {
		tls := *src.TLS
		if src.TLS.RestoreVerificationAfterBoot != nil {
			v := *src.TLS.RestoreVerificationAfterBoot
			tls.RestoreVerificationAfterBoot = &v
		}
		out.TLS = &tls
	}
	return out
}

func normalizeEnvironment(env *v1alpha1.Environment) {
	if env.Spec.SecretStorage.Mode == "" {
		env.Spec.SecretStorage.Mode = v1alpha1.SecretStorageModeSource
	}
}

func normalizeSecrets(state *v1alpha1.State) {
	for i := range state.Secrets {
		gen := state.Secrets[i].Spec.Source.Generated
		if gen == nil {
			continue
		}
		switch state.Secrets[i].Spec.Type {
		case v1alpha1.SecretTypeTLSCertificate, v1alpha1.SecretTypeCABundle:
			if gen.ValidityDays == 0 {
				gen.ValidityDays = v1alpha1.DefaultCertificateDays
			}
		case v1alpha1.SecretTypeUsernamePassword:
			if gen.Username == "" {
				gen.Username = "admin"
			}
		case v1alpha1.SecretTypeSSHKeyPair:
			if gen.KeyType == "" {
				gen.KeyType = v1alpha1.SSHKeyPairTypeEd25519
			}
		}
	}
}

func normalizeEntitlementSatellite(rhsm *v1alpha1.EntitlementRHSM) {
	if rhsm == nil || rhsm.Satellite == nil {
		return
	}
	sat := rhsm.Satellite
	sat.Hostname = strings.TrimSpace(sat.Hostname)
	if sat.ContentBaseURL == "" && sat.Hostname != "" {
		sat.ContentBaseURL = "https://" + sat.Hostname + "/pulp/content"
	}
}

func normalizeMachineFQDNs(state *v1alpha1.State) {
	env := primaryEnvironment(state)
	if env == nil {
		return
	}
	domain := env.Spec.Domains.MachinesDomain()
	if domain == "" {
		return
	}
	for i := range state.Machines {
		machine := &state.Machines[i]
		if _, ok := v1alpha1.MachineAddressByName(*machine, v1alpha1.MachineAddressFQDN); ok {
			continue
		}
		machine.Spec.Addresses = append(machine.Spec.Addresses, v1alpha1.MachineAddress{
			Name:    v1alpha1.MachineAddressFQDN,
			Address: machine.Metadata.Name + "." + domain,
		})
	}
}

func normalizeMachine(m *v1alpha1.Machine) {
	if m.Spec.Hardware.Management.BMC.Address != "" {
		normalizeBMC(&m.Spec.Hardware.Management.BMC)
	}
	config := &m.Spec.Network.Config
	if config.NetworkConfigRef.Name != "" && m.Spec.Substrate.ProviderRef.Name != "" && config.AttachmentRef.Name == "" {
		config.AttachmentRef.Name = config.NetworkConfigRef.Name
		m.DefaultedRefs.AttachmentRef = true
	}
	if ssh := m.Spec.Access.SSH; ssh != nil && ssh.AddressRef.Name == "" {
		for _, address := range m.Spec.Addresses {
			if address.Name == "ssh" {
				ssh.AddressRef.Name = "ssh"
				break
			}
		}
	}
	if m.Spec.Access.SSH != nil && m.Spec.Access.RootLogin == "" {
		m.Spec.Access.RootLogin = v1alpha1.MachineRootLoginKeep
	}
}

func normalizeProvider(p *v1alpha1.InfraProvider) {
	if p.Spec.Type == v1alpha1.ProvisionerLibvirt && p.Spec.Libvirt != nil && p.Spec.Libvirt.BMCEmulationDefaults != nil {
		normalizeBMCEmulationDefaults(p.Spec.Libvirt.BMCEmulationDefaults)
	}
	if p.Spec.Type == v1alpha1.ProvisionerKubeVirt && p.Spec.KubeVirt != nil {
		for i := range p.Spec.NetworkAttachments {
			ref := p.Spec.NetworkAttachments[i].KubeVirt
			if ref != nil && ref.NetworkRef.Namespace == "" {
				ref.NetworkRef.Namespace = p.Spec.KubeVirt.Namespace
			}
		}
	}
}

func normalizeInfraComponent(c *v1alpha1.InfraComponent) {
	if server := c.Spec.ArtifactServer; server != nil {
		if server.BindAddress == "" {
			server.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if len(server.Listeners) == 0 {
			server.Listeners = []v1alpha1.ArtifactServerListener{{
				Name:     v1alpha1.ArtifactServerProtocolHTTPS,
				Protocol: v1alpha1.ArtifactServerProtocolHTTPS,
				Port:     v1alpha1.DefaultArtifactsHTTPPort,
			}}
		}
	}
	if proxy := c.Spec.Proxy; proxy != nil {
		if proxy.BindAddress == "" {
			proxy.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if proxy.Port == 0 {
			proxy.Port = v1alpha1.DefaultSquidPort
		}
	}
	if dns := c.Spec.NameResolution; dns != nil {
		if dns.BindAddress == "" {
			dns.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if dns.Port == 0 {
			dns.Port = v1alpha1.DefaultDNSPort
		}
	}
	if ntp := c.Spec.NTP; ntp != nil {
		if ntp.BindAddress == "" {
			ntp.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if ntp.Port == 0 {
			ntp.Port = v1alpha1.DefaultNTPPort
		}
	}
	if registry := c.Spec.Registry; registry != nil {
		if registry.BindAddress == "" {
			registry.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if registry.Port == 0 {
			registry.Port = v1alpha1.DefaultMirrorRegistryPort
		}
	}
}

func normalizeBMCEmulationDefaults(b *v1alpha1.BMCEmulationDefaults) {
	if b.Enabled == nil {
		b.Enabled = v1alpha1.BoolPtr(true)
	}
	if b.Protocol == "" {
		b.Protocol = v1alpha1.DefaultBMCProtocol
	}
	if b.Emulator == "" {
		b.Emulator = v1alpha1.DefaultBMCEmulator
	}
	if b.BindAddress == "" {
		b.BindAddress = v1alpha1.DefaultBMCBindAddress
	}
	if b.Port == 0 {
		b.Port = v1alpha1.DefaultBMCEmulationStartPort
	}
	if b.VMediaPort == 0 {
		b.VMediaPort = b.Port + 1
	}
}

func normalizeBMC(b *v1alpha1.BMCSpec) {
	if b.Address != "" && b.Protocol == "" {
		b.Protocol = v1alpha1.DefaultBMCProtocol
	}
}

func normalizeClusterAddon(extension *v1alpha1.ClusterAddon) {
	if extension.Spec.Readiness.Timeout == "" {
		extension.Spec.Readiness.Timeout = v1alpha1.DefaultClusterAddonReadinessTimeout
	}
	if extension.Spec.OLM == nil {
		return
	}
	if extension.Spec.OLM.Subscription.SourceNamespace == "" {
		extension.Spec.OLM.Subscription.SourceNamespace = "openshift-marketplace"
	}
	if extension.Spec.OLM.CatalogSource != nil && extension.Spec.OLM.Subscription.Source == "" {
		extension.Spec.OLM.Subscription.Source = extension.Spec.OLM.CatalogSource.Name
	}
	if extension.Spec.OLM.Subscription.InstallPlanApproval == "" {
		extension.Spec.OLM.Subscription.InstallPlanApproval = v1alpha1.InstallPlanApprovalAutomatic
	}
}

func normalizeStorageCluster(cluster *v1alpha1.StorageCluster, state v1alpha1.State) {
	if cluster.Spec.Management == "" {
		cluster.Spec.Management = v1alpha1.StorageClusterManagementManaged
	}
	if cluster.Spec.Ceph == nil {
		return
	}
	if cluster.Spec.Ceph.Distribution == "" {
		cluster.Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionOSS
	}
	ceph := cluster.Spec.Ceph
	if ceph.Release == "" {
		ceph.Release = cephprovider.DefaultRelease(ceph.Distribution)
	}
	if release, ok := cephprovider.ResolveRelease(ceph.Distribution, ceph.Release); ok {
		ceph.Release = release.Value
	}
	if version := cephprovider.DerivedOSSImageVersion(ceph.Distribution, ceph.Release); version != "" {
		if ceph.Image == nil {
			ceph.Image = &v1alpha1.StorageCephImageSpec{}
		}
		if ceph.Image.Version == "" {
			ceph.Image.Version = version
		}
	}
	if ceph.Community != nil && ceph.Community.Checksum != "" {
		if checksum, err := media.NormalizeSHA256(ceph.Community.Checksum); err == nil {
			ceph.Community.Checksum = checksum
		}
	}
	adm := &cluster.Spec.Ceph.Cephadm
	if adm.Bootstrap.AddressRef.Name == "" {
		adm.Bootstrap.AddressRef = adm.AddressRef
	}
	if adm.ClusterSSH.User == "" {
		if v1alpha1.StorageClusterRevokesRootLogin(*cluster, state) {
			adm.ClusterSSH.User = v1alpha1.StorageCephadmDefaultSSHUser
		} else {
			adm.ClusterSSH.User = v1alpha1.RootSSHUser
		}
	}
}

func normalizeNodeNames(state *v1alpha1.State) {
	containerDomain, storageDomain := "", ""
	if env := primaryEnvironment(state); env != nil {
		containerDomain = env.Spec.Domains.ContainerClustersDomain()
		storageDomain = env.Spec.Domains.StorageClustersDomain()
	}
	for i := range state.ContainerClusters {
		cluster := &state.ContainerClusters[i]
		for j := range cluster.Spec.Nodes {
			node := &cluster.Spec.Nodes[j]
			node.Name = nodeFQDN(node.Name, cluster.Metadata.Name, containerDomain)
		}
	}
	for i := range state.StorageClusters {
		cluster := &state.StorageClusters[i]
		if cluster.Spec.Ceph == nil {
			continue
		}
		for j := range cluster.Spec.Ceph.Topology.Nodes {
			node := &cluster.Spec.Ceph.Topology.Nodes[j]
			node.Name = nodeFQDN(node.Name, cluster.Metadata.Name, storageDomain)
		}
		normalizeStorageStretch(cluster)
	}
}

func nodeFQDN(name, clusterName, baseDomain string) string {
	if name == "" {
		return ""
	}
	if strings.Contains(name, ".") || baseDomain == "" {
		return name
	}
	return stateview.ComposeFQDN(name, clusterName, baseDomain)
}

func normalizeStorageDNSAliases(state *v1alpha1.State) {
	storageDomain := ""
	if env := primaryEnvironment(state); env != nil {
		storageDomain = env.Spec.Domains.StorageClustersDomain()
	}
	if storageDomain == "" {
		return
	}
	for i := range state.StorageClusters {
		cluster := &state.StorageClusters[i]
		if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.Management == nil {
			continue
		}
		mgmt := cluster.Spec.Ceph.Management
		if mgmt.DNSName == "" {
			mgmt.DNSName = stateview.ComposeFQDN("mgr", cluster.Metadata.Name, storageDomain)
		}
	}
	clusters := indexStorageClusters(state.StorageClusters)
	for i := range state.StorageObjectGateways {
		gw := &state.StorageObjectGateways[i]
		if gw.Spec.Public.DNSName != "" {
			continue
		}
		cluster, ok := clusters[gw.Spec.StorageClusterRef.Name]
		if !ok {
			continue
		}
		gw.Spec.Public.DNSName = stateview.ComposeFQDN(gw.Metadata.Name, cluster.Metadata.Name, storageDomain)
	}
}

func normalizeStorageStretch(cluster *v1alpha1.StorageCluster) {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil {
		return
	}
	if stretch.RuleName == "" {
		stretch.RuleName = "stretch-rule"
	}
	if stretch.Tiebreaker.Site == "" && stretch.Tiebreaker.Node != "" {
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.Name == stretch.Tiebreaker.Node || stateview.NodeShortName(node.Name) == stretch.Tiebreaker.Node {
				stretch.Tiebreaker.Site = node.Site
				break
			}
		}
	}
	if len(stretch.DataSites) == 0 {
		seen := map[string]bool{}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.Site == "" || node.Site == stretch.Tiebreaker.Site || seen[node.Site] {
				continue
			}
			seen[node.Site] = true
			stretch.DataSites = append(stretch.DataSites, node.Site)
		}
		sort.Strings(stretch.DataSites)
	}
}

func applyClusterNetworkDefaults(state *v1alpha1.State) {
	networkConfigs := indexNetworkConfigs(state.NetworkConfigs)
	for i := range state.ContainerClusters {
		ocp := &state.ContainerClusters[i]
		if ocp.Spec.Networking == nil {
			ocp.Spec.Networking = &v1alpha1.OCPNetworkingSpec{}
		}
		v6 := clusterMachineNetworkIsIPv6Only(*state, *ocp, networkConfigs)
		if len(ocp.Spec.Networking.ClusterNetwork) == 0 {
			cidr, hostPrefix := v1alpha1.DefaultClusterNetworkCIDR, v1alpha1.DefaultClusterNetworkHostPrefix
			if v6 {
				cidr, hostPrefix = defaultClusterNetworkCIDRIPv6, defaultClusterNetworkHostPrefixIPv6
			}
			ocp.Spec.Networking.ClusterNetwork = []v1alpha1.ContainerClusterNetworkCIDR{{CIDR: cidr, HostPrefix: hostPrefix}}
		}
		if len(ocp.Spec.Networking.ServiceNetwork) == 0 {
			cidr := v1alpha1.DefaultServiceNetworkCIDR
			if v6 {
				cidr = defaultServiceNetworkCIDRIPv6
			}
			ocp.Spec.Networking.ServiceNetwork = []string{cidr}
		}
	}
}

func clusterMachineNetworkIsIPv6Only(state v1alpha1.State, ocp v1alpha1.ContainerCluster, networkConfigs map[string]v1alpha1.NetworkConfig) bool {
	ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
	if !ok {
		return false
	}
	var cidrs []string
	for _, name := range stateview.ClusterConsumedNetworkConfigs(ci) {
		if nc, ok := networkConfigs[name]; ok {
			for _, mn := range nc.Spec.MachineNetwork {
				cidrs = append(cidrs, mn.CIDR)
			}
		}
	}
	for _, machine := range ci.Machines {
		if machine.Network.Spec == nil {
			continue
		}
		for _, mn := range machine.Network.Spec.MachineNetwork {
			cidrs = append(cidrs, mn.CIDR)
		}
	}
	if len(cidrs) == 0 {
		return false
	}
	for _, cidr := range cidrs {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil || ip.To4() != nil {
			return false
		}
	}
	return true
}

func normalizeStoragePool(pool *v1alpha1.StoragePool) {
	if pool.Spec.Ceph.Type == "" {
		pool.Spec.Ceph.Type = v1alpha1.StoragePoolTypeReplicated
	}
}

func normalizeStorageFilesystem(fs *v1alpha1.StorageFilesystem) {
	if len(fs.Spec.CephFS.DataPoolRefs) == 1 && !fs.Spec.CephFS.DataPoolRefs[0].Default {
		fs.Spec.CephFS.DataPoolRefs[0].Default = true
	}
}

func normalizeStorageObjectGateway(gateway *v1alpha1.StorageObjectGateway) {
	if gateway.Spec.Ceph.FrontendPort == 0 {
		gateway.Spec.Ceph.FrontendPort = 8080
	}
}

func normalizeStorageExport(export *v1alpha1.StorageExport) {
	if export.Spec.Type == "" && export.Spec.DataFoundation != nil {
		export.Spec.Type = v1alpha1.StorageExportTypeDataFoundation
	}
}

func normalizeContainerCluster(ocp *v1alpha1.ContainerCluster, env *v1alpha1.Environment) {
	if ocp.Spec.Distribution.Type == "" {
		ocp.Spec.Distribution.Type = v1alpha1.DistributionOpenShift
	}
	if ocp.Spec.Install.Mode == "" {
		ocp.Spec.Install.Mode = v1alpha1.InstallModeConnected
	}
	if ocp.Spec.Install.Method == "" {
		ocp.Spec.Install.Method = v1alpha1.OCPInstallMethodAgent
	}
	if _, ok := ocp.Spec.Install.Endpoints[v1alpha1.EndpointAPIInt]; !ok {
		if api, ok := ocp.Spec.Install.Endpoints[v1alpha1.EndpointAPI]; ok {
			ocp.Spec.Install.Endpoints[v1alpha1.EndpointAPIInt] = v1alpha1.Endpoint{
				Address: api.Address,
				Source:  api.Source,
			}
		}
	}
	for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		endpoint, ok := ocp.Spec.Install.Endpoints[name]
		if !ok || endpoint.Source.Type != "" {
			continue
		}
		endpoint.Source.Type = v1alpha1.EndpointSourceOpenShift
		ocp.Spec.Install.Endpoints[name] = endpoint
	}
	if ocp.Spec.Networking == nil {
		ocp.Spec.Networking = &v1alpha1.OCPNetworkingSpec{}
	}
	applyEnvironmentInstallDefaults(ocp, env)
}

func applyClusterPlatformDefaults(state *v1alpha1.State) {
	for i := range state.ContainerClusters {
		cluster := &state.ContainerClusters[i]
		platform := &cluster.Spec.Install.Platform
		if !installPlatformOmitted(*platform) {
			continue
		}
		if stateview.IsSingleNodeCluster(*cluster) {
			platform.Type = v1alpha1.PlatformTypeNone
			continue
		}
		binding := clusterNodeProviderBinding(*state, *cluster)
		if !binding.complete || len(binding.types) != 1 {
			continue
		}
		switch binding.types[0] {
		case v1alpha1.ProvisionerLibvirt, v1alpha1.ProvisionerBareMetal:
			platform.Type = v1alpha1.PlatformTypeBareMetal
			platform.BareMetal = &v1alpha1.BareMetalInstallPlatform{
				ProvisioningNetwork: v1alpha1.ProvisioningNetworkDisabled,
			}
		case v1alpha1.ProvisionerVSphere:
			platform.Type = v1alpha1.PlatformTypeVSphere
		case v1alpha1.ProvisionerKubeVirt:
			platform.Type = v1alpha1.PlatformTypeNone
		}
	}
}

func installPlatformOmitted(platform v1alpha1.InstallPlatform) bool {
	return platform.Type == "" &&
		platform.BareMetal == nil &&
		platform.VSphere == nil &&
		platform.External == nil
}

type clusterProviderBinding struct {
	types     []string
	providers []string
	complete  bool
}

func clusterNodeProviderBinding(state v1alpha1.State, cluster v1alpha1.ContainerCluster) clusterProviderBinding {
	binding := clusterProviderBinding{complete: len(cluster.Spec.Nodes) > 0}
	types := map[string]bool{}
	providers := map[string]bool{}
	for _, node := range cluster.Spec.Nodes {
		machine, ok := stateview.Machine(state, node.MachineRef.Name)
		if !ok {
			binding.complete = false
			continue
		}
		provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
		if !ok || provider.Spec.Type == "" {
			binding.complete = false
			continue
		}
		types[provider.Spec.Type] = true
		providers["InfraProvider/"+provider.Metadata.Name+" ("+provider.Spec.Type+")"] = true
	}
	binding.types = sortedKeys(types)
	binding.providers = sortedKeys(providers)
	return binding
}

func sortedKeys[V any](set map[string]V) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func applyEnvironmentInstallDefaults(ocp *v1alpha1.ContainerCluster, env *v1alpha1.Environment) {
	if env == nil {
		return
	}
	if v1alpha1.DistributionType(*ocp) == v1alpha1.DistributionOpenShift && ocp.Spec.Install.PullSecretRef.Name == "" {
		ref := env.Spec.Defaults.Install.PullSecretRef
		if ref.Name == "" {
			ref = v1alpha1.SecretRef{Name: v1alpha1.DefaultPullSecretName}
		}
		ocp.Spec.Install.PullSecretRef = ref
		ocp.DefaultedRefs.PullSecretRef = true
	}
	if ocp.Spec.Install.NodeSSH.IsZero() {
		defaultSSH := env.Spec.Defaults.Install.NodeSSH
		if defaultSSH.IsZero() {
			defaultSSH = v1alpha1.NodeSSHSpec{
				KeyPairRef: v1alpha1.SecretRef{Name: v1alpha1.ClusterAdminSSHKeyName(ocp.Metadata.Name)},
			}
		}
		ocp.Spec.Install.NodeSSH = defaultSSH
		ocp.DefaultedRefs.NodeSSH = true
	}
}

func primaryEnvironment(state *v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}
