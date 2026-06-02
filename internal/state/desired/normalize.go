package desiredstate

import "github.com/crmarques/bootwright/api/v1alpha1"

// Normalize applies defaults in-place. Pure transformation: no
// diagnostics, no rejections; that work belongs to Validate.
func Normalize(state *v1alpha1.State) {
	for i := range state.Environments {
		normalizeEnvironment(&state.Environments[i])
	}
	for i := range state.InfraProviders {
		normalizeProvider(&state.InfraProviders[i])
	}
	for i := range state.InfraComponents {
		normalizeInfraComponent(&state.InfraComponents[i])
	}
	for i := range state.ClusterInfras {
		normalizeClusterInfra(&state.ClusterInfras[i])
	}
	env := primaryEnvironment(state)
	for i := range state.ContainerClusters {
		normalizeContainerCluster(&state.ContainerClusters[i], env)
	}
	for i := range state.ClusterAddons {
		normalizeClusterAddon(&state.ClusterAddons[i])
	}
	for i := range state.StorageClusters {
		normalizeStorageCluster(&state.StorageClusters[i])
	}
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
}

func normalizeProvider(p *v1alpha1.InfraProvider) {
	for i := range p.Spec.MachineProfiles {
		mp := &p.Spec.MachineProfiles[i]
		if mp.Libvirt != nil && mp.Libvirt.BMCEmulationDefaults != nil {
			normalizeBMCEmulationDefaults(mp.Libvirt.BMCEmulationDefaults)
		}
	}
	for i := range p.Spec.Machines {
		m := &p.Spec.Machines[i]
		if m.BareMetal != nil {
			normalizeBMC(&m.BareMetal.BMC)
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

func normalizeClusterInfra(ci *v1alpha1.ClusterInfra) {
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
	adm := &cluster.Spec.Ceph.Cephadm
	if adm.ClusterSSH.KeyPairRef.Name == "" && adm.ClusterSSH.PrivateKeyRef.Name == "" {
		adm.ClusterSSH = adm.NodeSSH
	}
	if adm.Bootstrap.MonIP.Family == "" {
		adm.Bootstrap.MonIP.Family = "ipv4"
	}
	if adm.Bootstrap.MonIP.MachineRef.ClusterInfra == "" {
		adm.Bootstrap.MonIP.MachineRef.ClusterInfra = cluster.Spec.ClusterInfraRef.Name
	}
	if adm.Bootstrap.MonIP.MachineRef.Name == "" {
		adm.Bootstrap.MonIP.MachineRef.Name = adm.Bootstrap.SeedNode
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
	applyEnvironmentInstallDefaults(ocp, env)
	for i := range ocp.Spec.Nodes {
		node := &ocp.Spec.Nodes[i]
		if node.MachineRef.Name == "" {
			node.MachineRef.Name = node.Hostname
		}
	}
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
	}
	if ocp.Spec.Install.NodeSSH.IsZero() {
		defaultSSH := env.Spec.Defaults.Install.NodeSSH
		if defaultSSH.IsZero() {
			defaultSSH = v1alpha1.NodeSSHSpec{
				KeyPairRef: v1alpha1.SecretRef{Name: v1alpha1.DefaultNodeSSHKeyName},
			}
		}
		ocp.Spec.Install.NodeSSH = defaultSSH
	}
}

func primaryEnvironment(state *v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}
