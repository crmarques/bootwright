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
	if b.Protocol == "" {
		b.Protocol = v1alpha1.DefaultBMCProtocol
	}
}

func normalizeClusterInfra(ci *v1alpha1.ClusterInfra) {
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
		ocp.Spec.Install.PullSecretRef = v1alpha1.SecretRef{Name: v1alpha1.DefaultPullSecretName}
	}
	if ocp.Spec.Install.SSHKeyRef.Name == "" {
		ocp.Spec.Install.SSHKeyRef = v1alpha1.SecretRef{Name: v1alpha1.DefaultClusterSSHKeyName}
	}
}

func primaryEnvironment(state *v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}
