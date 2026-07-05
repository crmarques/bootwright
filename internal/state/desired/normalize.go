package desiredstate

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Normalize applies defaults in-place. Pure transformation: no
// diagnostics, no rejections; that work belongs to Validate.
func Normalize(state *v1alpha1.State) {
	for i := range state.Environments {
		normalizeEnvironment(&state.Environments[i])
	}
	for i := range state.Machines {
		normalizeMachine(&state.Machines[i])
	}
	for i := range state.MachineImages {
		normalizeMachineImage(&state.MachineImages[i])
	}
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
	applyEnvironmentArtifactAccessDefaults(state, env)
	applyBareMetalBMCDefaults(state)
	for i := range state.ClusterAddons {
		normalizeClusterAddon(&state.ClusterAddons[i])
	}
	for i := range state.StorageClusters {
		normalizeStorageCluster(&state.StorageClusters[i])
	}
	normalizeNodeHostnames(state)
	for i := range state.StoragePools {
		normalizeStoragePool(&state.StoragePools[i])
	}
	for i := range state.StorageFilesystems {
		normalizeStorageFilesystem(&state.StorageFilesystems[i])
	}
	for i := range state.StorageObjectGateways {
		normalizeStorageObjectGateway(&state.StorageObjectGateways[i])
	}
	storageClusters := indexStorageClusters(state.StorageClusters)
	for i := range state.StorageExports {
		normalizeStorageExport(&state.StorageExports[i], storageClusters)
	}
	for i := range state.ProvisioningPlaybooks {
		normalizeProvisioningPlaybook(&state.ProvisioningPlaybooks[i])
	}
}

// normalizeProvisioningPlaybook materializes the optional-with-default fields so
// every downstream consumer (validation, planner, dry-run) reads one value
// rather than re-deriving the default.
func normalizeProvisioningPlaybook(p *v1alpha1.ProvisioningPlaybook) {
	if p.Spec.Timing == "" {
		p.Spec.Timing = v1alpha1.ProvisioningPlaybookTimingAfter
	}
	if p.Spec.Run == "" {
		p.Spec.Run = v1alpha1.ProvisioningPlaybookRunOnChange
	}
	if p.Spec.FailureMode == "" {
		p.Spec.FailureMode = v1alpha1.ProvisioningPlaybookFailureFail
	}
	if p.Spec.Enabled == nil {
		enabled := true
		p.Spec.Enabled = &enabled
	}
}

// applyBareMetalBMCDefaults lets a bare-metal InfraProvider set BMC TLS behavior
// once (spec.baremetal.defaults.bmc) and have every Machine bound to it inherit
// what it omits. credentialsRef is never defaulted (it stays per-machine). The
// merge mutates the effective machine spec so every consumer — boot vars, the
// substrate manifest, validation, and render effective — reads one value.
func applyBareMetalBMCDefaults(state *v1alpha1.State) {
	for i := range state.Machines {
		bmc := &state.Machines[i].Spec.Hardware.Management.BMC
		// Never invent a BMC on a machine that declares none.
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
		// tls.verify: tri-state; inherit field-level only when the machine left it unset.
		if d.TLS != nil && d.TLS.Verify != nil {
			if bmc.TLS == nil {
				bmc.TLS = &v1alpha1.BMCTLS{}
			}
			if bmc.TLS.Verify == nil {
				v := *d.TLS.Verify
				bmc.TLS.Verify = &v
			}
		}
		// virtualMedia: an authored node block wins whole; otherwise deep-copy the default.
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
		if src.TLS.Verify != nil {
			v := *src.TLS.Verify
			tls.Verify = &v
		}
		out.TLS = &tls
	}
	return out
}

func normalizeEnvironment(env *v1alpha1.Environment) {
	if env.Spec.SecretStorage.Mode == "" {
		env.Spec.SecretStorage.Mode = v1alpha1.SecretStorageModeSource
	}
	for name, secret := range env.Spec.Secrets {
		if secret.Generated == nil {
			continue
		}
		if cert := secret.Generated.SelfSignedCertificate; cert != nil && cert.ValidityDays == 0 {
			cert.ValidityDays = v1alpha1.DefaultCertificateDays
		}
		if creds := secret.Generated.Credentials; creds != nil && creds.Username == "" {
			creds.Username = "admin"
		}
		if keyPair := secret.Generated.SSHKeyPair; keyPair != nil && keyPair.Type == "" {
			keyPair.Type = v1alpha1.SSHKeyPairTypeEd25519
		}
		env.Spec.Secrets[name] = secret
	}
	for i := range env.Spec.Entitlements {
		normalizeEntitlementSatellite(env.Spec.Entitlements[i].RHSM)
	}
}

// normalizeEntitlementSatellite trims the Satellite hostname and, when the
// operator left contentBaseURL unset, derives the canonical Satellite 6 content
// path (https://<hostname>/pulp/content) so normalized state shows the effective
// value the renderer will use.
func normalizeEntitlementSatellite(rhsm *v1alpha1.EnvironmentEntitlementRHSM) {
	if rhsm == nil || rhsm.Satellite == nil {
		return
	}
	sat := rhsm.Satellite
	sat.Hostname = strings.TrimSpace(sat.Hostname)
	if sat.ContentBaseURL == "" && sat.Hostname != "" {
		sat.ContentBaseURL = "https://" + sat.Hostname + "/pulp/content"
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
	// Documented convention: access.ssh.addressRef defaults to the address
	// named "ssh" when one exists. No only-address fallback — adding a second
	// address must never silently change behavior.
	if ssh := m.Spec.Access.SSH; ssh != nil && ssh.AddressRef.Name == "" {
		for _, address := range m.Spec.Addresses {
			if address.Name == "ssh" {
				ssh.AddressRef.Name = "ssh"
				break
			}
		}
	}
}

// normalizeMachineImage materializes the install-media derivations so they
// land in effective state: mediaType defaults to dvd, an omitted
// installSource.type derives from which fields are present, and a url install
// source without a url promotes repositories[0].baseURL to the primary install
// tree. Validators and renderers read the materialized values instead of
// recomputing them. Authored values always win; invalid ones are left for
// Validate to reject.
func normalizeMachineImage(image *v1alpha1.MachineImage) {
	spec := &image.Spec
	// mediaType defaults to dvd unconditionally. A filename suffix must not select
	// the install mode: deriving boot from a "boot.iso" URL let a pure rename flip
	// dvd<->boot (and whether installSource is required). A netinstall image
	// authors mediaType: boot explicitly.
	if spec.MediaType == "" {
		spec.MediaType = v1alpha1.MachineImageMediaTypeDVD
	}
	source := &spec.InstallSource
	if source.Type == "" {
		switch {
		case source.EntitlementRef.Name != "":
			source.Type = v1alpha1.MachineImageInstallSourceTypeRHSM
		case source.FromMedia != "":
			source.Type = v1alpha1.MachineImageInstallSourceTypeHostedTree
		case source.URL != "" || len(source.Repositories) > 0:
			source.Type = v1alpha1.MachineImageInstallSourceTypeURL
		}
	}
	if source.Type == v1alpha1.MachineImageInstallSourceTypeURL &&
		source.URL == "" && len(source.Repositories) > 0 && source.Repositories[0].BaseURL != "" {
		source.URL = source.Repositories[0].BaseURL
		source.Repositories = source.Repositories[1:]
	}
}

func normalizeProvider(p *v1alpha1.InfraProvider) {
	if p.Spec.Type == v1alpha1.ProvisionerLibvirt && p.Spec.Libvirt != nil && p.Spec.Libvirt.BMCEmulationDefaults != nil {
		normalizeBMCEmulationDefaults(p.Spec.Libvirt.BMCEmulationDefaults)
	}
	if p.Spec.Type == v1alpha1.ProvisionerKubeVirt && p.Spec.KubeVirt != nil {
		// A kubevirt networkRef.namespace defaults to the provider's VM
		// namespace: the OVN-derived NAD (for a CUDN) lands in the selected VM
		// namespace, and a referenced NAD/UDN is co-located with the VMs.
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

func applyEnvironmentArtifactAccessDefaults(state *v1alpha1.State, env *v1alpha1.Environment) {
	if env == nil {
		return
	}
	defaults := env.Spec.Defaults.ArtifactAccess
	consumers := clusterInstallArtifactAccessConsumers(*state)
	for i := range state.ContainerClusters {
		cluster := &state.ContainerClusters[i]
		consumer := consumers[cluster.Metadata.Name]
		access := &cluster.Spec.Install.ArtifactAccess
		if consumer.RedfishVirtualMedia && access.RedfishVirtualMedia.EndpointRef.Name == "" {
			access.RedfishVirtualMedia = defaults.RedfishVirtualMedia
			cluster.DefaultedRefs.ArtifactAccessRedfishVirtualMedia = access.RedfishVirtualMedia.EndpointRef.Name != ""
		}
		if consumer.ContainerClusterInstall && access.ContainerClusterInstall.EndpointRef.Name == "" {
			access.ContainerClusterInstall = defaults.ContainerClusterInstall
			cluster.DefaultedRefs.ArtifactAccessContainerClusterInstall = access.ContainerClusterInstall.EndpointRef.Name != ""
		}
		if access.ServerRef.Name == "" && clusterArtifactAccessHasEndpoint(*access) {
			access.ServerRef = defaults.ServerRef
			cluster.DefaultedRefs.ArtifactAccessServerRef = access.ServerRef.Name != ""
		}
	}
}

type artifactAccessConsumers struct {
	RedfishVirtualMedia     bool
	ContainerClusterInstall bool
}

func clusterInstallArtifactAccessConsumers(state v1alpha1.State) map[string]artifactAccessConsumers {
	out := map[string]artifactAccessConsumers{}
	for _, ocp := range state.ContainerClusters {
		ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
		if !ok {
			continue
		}
		consumer := out[ocp.Metadata.Name]
		if artifacts.ClusterUsesBareMetalMachine(state, ci) {
			consumer.RedfishVirtualMedia = true
		}
		if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
			consumer.ContainerClusterInstall = true
		}
		out[ocp.Metadata.Name] = consumer
	}
	return out
}

func clusterArtifactAccessHasEndpoint(access v1alpha1.ClusterArtifactAccess) bool {
	return access.RedfishVirtualMedia.EndpointRef.Name != "" ||
		access.ContainerClusterInstall.EndpointRef.Name != ""
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
	if extension.Spec.OLM.Subscription.InstallPlanApproval == "" {
		extension.Spec.OLM.Subscription.InstallPlanApproval = v1alpha1.InstallPlanApprovalAutomatic
	}
}

func normalizeStorageCluster(cluster *v1alpha1.StorageCluster) {
	if cluster.Spec.Management == "" {
		cluster.Spec.Management = v1alpha1.StorageClusterManagementManaged
	}
	if cluster.Spec.Ceph == nil {
		return
	}
	if cluster.Spec.Ceph.Distribution == "" {
		cluster.Spec.Ceph.Distribution = v1alpha1.StorageCephDistributionOSS
	}
	adm := &cluster.Spec.Ceph.Cephadm
	if adm.Bootstrap.AddressRef.Name == "" {
		adm.Bootstrap.AddressRef = adm.AddressRef
	}
	// An explicit cephadm cluster SSH key carries an explicit manage user;
	// default it to root. Leaving it empty when no clusterSSHKeyRef is set keeps
	// the legacy path, where the user is borrowed from the first host's access.
	if adm.ClusterSSHKeyRef.Name != "" && adm.ClusterSSHUser == "" {
		adm.ClusterSSHUser = "root"
	}
	// A topology host's cephadm hostname defaults to the fully-qualified node
	// name; normalizeNodeHostnames fills it once baseDomain and the backing
	// machine's install profile are in scope. The explicit field is a signal
	// that the Ceph hostname genuinely differs and is kept verbatim.
	normalizeStorageStretch(cluster)
}

// normalizeNodeHostnames defaults every cluster node's hostname to the
// fully-qualified <machine>.<cluster>.<baseDomain> form — the OpenShift node
// convention, applied uniformly to Ceph and OpenShift nodes so the name
// cephadm/the installer registers always resolves. An explicit hostname is kept
// verbatim. A node opts back out to the bare machine name with
// hostname.source: machineName on its install profile, and every node falls
// back to the bare name when no baseDomain is declared.
func normalizeNodeHostnames(state *v1alpha1.State) {
	baseDomain := ""
	if env := primaryEnvironment(state); env != nil {
		baseDomain = env.Spec.BaseDomain
	}
	for i := range state.ContainerClusters {
		cluster := &state.ContainerClusters[i]
		for j := range cluster.Spec.Hosts {
			if host := &cluster.Spec.Hosts[j]; host.Hostname == "" {
				host.Hostname = defaultNodeHostname(state, host.MachineRef.Name, cluster.Metadata.Name, baseDomain)
			}
		}
	}
	for i := range state.StorageClusters {
		cluster := &state.StorageClusters[i]
		if cluster.Spec.Ceph == nil {
			continue
		}
		for j := range cluster.Spec.Ceph.Topology.Hosts {
			if host := &cluster.Spec.Ceph.Topology.Hosts[j]; host.Hostname == "" {
				host.Hostname = defaultNodeHostname(state, host.MachineRef.Name, cluster.Metadata.Name, baseDomain)
			}
		}
	}
}

// defaultNodeHostname derives a node's default hostname: the fully-qualified
// <machine>.<cluster>.<baseDomain>, or the bare machine name when the node's
// install profile opts out or no baseDomain qualifies it.
func defaultNodeHostname(state *v1alpha1.State, machineName, clusterName, baseDomain string) string {
	if machineName == "" {
		return ""
	}
	if baseDomain == "" || nodeOptsOutOfFQDN(state, machineName) {
		return machineName
	}
	return stateview.ComposeFQDN(machineName, clusterName, baseDomain)
}

// nodeOptsOutOfFQDN reports whether a machine's install profile pins the OS
// hostname to the bare machine name (hostname.source: machineName), opting the
// node out of FQDN naming.
func nodeOptsOutOfFQDN(state *v1alpha1.State, machineName string) bool {
	machine, ok := stateview.Machine(*state, machineName)
	if !ok {
		return false
	}
	profile, ok := stateview.MachineInstallProfile(*state, machine.Spec.OS.InstallProfileRef.Name)
	if !ok {
		return false
	}
	return profile.Spec.Customizations.Hostname.Source == v1alpha1.MachineInstallHostnameMachineName
}

// normalizeStorageStretch fills the derivable stretch fields: presence of the
// stretch block is the enablement signal, and only failureDomain plus the
// tiebreaker host are facts the operator alone knows. ruleName takes any
// authored value; dataSites and tiebreaker.site are echoes of the topology
// that validation cross-checks post-normalize, so authoring them only
// narrows (dataSites with OSD-only extra sites) or restates the derivation.
func normalizeStorageStretch(cluster *v1alpha1.StorageCluster) {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil {
		return
	}
	if stretch.RuleName == "" {
		stretch.RuleName = "stretch-rule"
	}
	if stretch.Tiebreaker.Site == "" && stretch.Tiebreaker.Host != "" {
		// The tiebreaker host is authored as a machine name or a hostname; match
		// either. This runs before hostnames are qualified, so the machine name
		// is the reliable key.
		for _, host := range cluster.Spec.Ceph.Topology.Hosts {
			if host.Hostname == stretch.Tiebreaker.Host || host.MachineRef.Name == stretch.Tiebreaker.Host {
				stretch.Tiebreaker.Site = host.Site
				break
			}
		}
	}
	if len(stretch.DataSites) == 0 {
		seen := map[string]bool{}
		for _, host := range cluster.Spec.Ceph.Topology.Hosts {
			if host.Site == "" || host.Site == stretch.Tiebreaker.Site || seen[host.Site] {
				continue
			}
			seen[host.Site] = true
			stretch.DataSites = append(stretch.DataSites, host.Site)
		}
		sort.Strings(stretch.DataSites)
	}
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

func normalizeStorageExport(export *v1alpha1.StorageExport, clusters map[string]v1alpha1.StorageCluster) {
	if export.Spec.Type == "" && export.Spec.DataFoundation != nil {
		export.Spec.Type = v1alpha1.StorageExportTypeDataFoundation
	}
	cluster, ok := clusters[export.Spec.StorageClusterRef.Name]
	if !ok {
		return
	}
	switch storageClusterManagement(cluster) {
	case v1alpha1.StorageClusterManagementManaged:
		if export.Spec.ExternalDetails == nil {
			export.Spec.ExternalDetails = &v1alpha1.StorageExportExternalDetailsSpec{
				Generated: &v1alpha1.StorageExportExternalDetailsGenerated{},
			}
		}
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
	if len(ocp.Spec.Networking.ClusterNetwork) == 0 {
		ocp.Spec.Networking.ClusterNetwork = []v1alpha1.ContainerClusterNetworkCIDR{{
			CIDR:       v1alpha1.DefaultClusterNetworkCIDR,
			HostPrefix: v1alpha1.DefaultClusterNetworkHostPrefix,
		}}
	}
	if len(ocp.Spec.Networking.ServiceNetwork) == 0 {
		ocp.Spec.Networking.ServiceNetwork = []string{v1alpha1.DefaultServiceNetworkCIDR}
	}
	applyEnvironmentInstallDefaults(ocp, env)
}

// applyClusterPlatformDefaults derives spec.install.platform from the single
// provider type behind a cluster's node machines when the platform block is
// fully omitted. The mapping mirrors what every shipped example authors:
// libvirt and baremetal providers install with platform bareMetal and the
// provisioning network disabled; vsphere providers install with platform
// vsphere; kubevirt-hosted clusters install with platform none. Ambiguous
// bindings (multiple provider types) are left for Validate to diagnose;
// authored platforms always win.
func applyClusterPlatformDefaults(state *v1alpha1.State) {
	for i := range state.ContainerClusters {
		cluster := &state.ContainerClusters[i]
		platform := &cluster.Spec.Install.Platform
		if !installPlatformOmitted(*platform) {
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

// clusterProviderBinding summarizes the InfraProviders behind a cluster's
// node machines. types holds the sorted unique provider types; providers
// holds "InfraProvider/<name> (<type>)" descriptions for diagnostics;
// complete reports whether every node resolved to a typed provider.
type clusterProviderBinding struct {
	types     []string
	providers []string
	complete  bool
}

func clusterNodeProviderBinding(state v1alpha1.State, cluster v1alpha1.ContainerCluster) clusterProviderBinding {
	binding := clusterProviderBinding{complete: len(cluster.Spec.Hosts) > 0}
	types := map[string]bool{}
	providers := map[string]bool{}
	for _, node := range cluster.Spec.Hosts {
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
