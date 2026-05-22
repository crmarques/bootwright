package desiredstate

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// Normalize applies defaults in-place. Pure transformation: no
// diagnostics, no rejections; that work belongs to Validate.
func Normalize(state *v1alpha1.State) {
	for i := range state.Environments {
		normalizeEnvironment(&state.Environments[i])
	}
	for i := range state.InfraProviders {
		normalizeProvider(&state.InfraProviders[i])
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
	for i := range p.Spec.ArtifactPublishers {
		if http := p.Spec.ArtifactPublishers[i].HTTP; http != nil && http.Port == 0 {
			http.Port = v1alpha1.DefaultArtifactsHTTPPort
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
	c := &ci.Spec.Components
	if c.Proxy != nil {
		if c.Proxy.BindAddress == "" {
			c.Proxy.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if c.Proxy.Port == 0 {
			c.Proxy.Port = v1alpha1.DefaultSquidPort
		}
	}
	if c.NameResolution != nil {
		if c.NameResolution.BindAddress == "" {
			c.NameResolution.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if c.NameResolution.Port == 0 {
			c.NameResolution.Port = v1alpha1.DefaultDNSPort
		}
	}
	if c.Registry != nil {
		if c.Registry.BindAddress == "" {
			c.Registry.BindAddress = v1alpha1.DefaultServiceBindAddress
		}
		if c.Registry.Port == 0 {
			c.Registry.Port = v1alpha1.DefaultMirrorRegistryPort
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
	applyEnvironmentInstallDefaults(ocp, env)
	if env != nil {
		applyDisconnectedDigestSources(ocp, env)
	}
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
	if ocp.Spec.Install.BaseDomain == "" {
		ocp.Spec.Install.BaseDomain = env.Spec.BaseDomain
	}
	if v1alpha1.DistributionType(*ocp) == v1alpha1.DistributionOpenShift && ocp.Spec.Install.PullSecretRef.Name == "" {
		ocp.Spec.Install.PullSecretRef = v1alpha1.SecretRef{Name: v1alpha1.DefaultPullSecretName}
	}
	if ocp.Spec.Install.SSHKeyRef.Name == "" {
		ocp.Spec.Install.SSHKeyRef = v1alpha1.SecretRef{Name: v1alpha1.DefaultClusterSSHKeyName}
	}
	if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil &&
		registries.Mirror.TrustBundleRef.Name != "" &&
		ocp.Spec.Install.AdditionalTrustBundleRef.Name == "" {
		ocp.Spec.Install.AdditionalTrustBundleRef = registries.Mirror.TrustBundleRef
	}
}

func applyDisconnectedDigestSources(ocp *v1alpha1.ContainerCluster, env *v1alpha1.Environment) {
	if env == nil || v1alpha1.InstallMode(*ocp) != v1alpha1.InstallModeDisconnected {
		return
	}
	registries := env.Spec.Registries
	if registries == nil {
		return
	}
	ocp.Spec.Install.ImageDigestSources = mergeImageDigestSources(
		ocp.Spec.Install.ImageDigestSources,
		deriveDisconnectedReleaseSources(*ocp, registries),
	)
	for i := range ocp.Spec.Install.ImageDigestSources {
		if ocp.Spec.Install.ImageDigestSources[i].SourcePolicy == "" {
			ocp.Spec.Install.ImageDigestSources[i].SourcePolicy = v1alpha1.ImageSourcePolicyNever
		}
	}
}

func deriveDisconnectedReleaseSources(ocp v1alpha1.ContainerCluster, registries *v1alpha1.EnvironmentRegistriesSpec) []v1alpha1.ImageDigestSource {
	out := append([]v1alpha1.ImageDigestSource(nil), registries.ImageDigestSources...)
	known := map[string]bool{}
	for _, src := range out {
		known[src.Source] = true
	}
	mirrorURL := ""
	if registries.Mirror != nil {
		mirrorURL = strings.TrimRight(registries.Mirror.URL, "/")
	}
	if mirrorURL == "" {
		return out
	}
	for _, src := range v1alpha1.DefaultReleaseImageDigestSources(ocp, mirrorURL) {
		if known[src.Source] {
			continue
		}
		out = append(out, src)
	}
	return out
}

func mergeImageDigestSources(existing, derived []v1alpha1.ImageDigestSource) []v1alpha1.ImageDigestSource {
	out := append([]v1alpha1.ImageDigestSource(nil), existing...)
	known := map[string]bool{}
	for _, src := range out {
		known[src.Source] = true
	}
	for _, src := range derived {
		if known[src.Source] {
			continue
		}
		out = append(out, src)
		known[src.Source] = true
	}
	return out
}

func primaryEnvironment(state *v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}
