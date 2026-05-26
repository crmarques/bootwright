package desiredstate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
)

// Validate is the single entry point. Every rule in
// `specs/state-model.md § Validation Rules` lives behind one of the
// per-kind validators called below. Each rule emits a precise
// diagnostic naming the field and the conflicting value.
func Validate(state v1alpha1.State) error {
	var errs []string
	errs = append(errs, validateEnvironments(state)...)
	errs = append(errs, validateHosts(state)...)
	errs = append(errs, validateNetworkConfigs(state)...)
	errs = append(errs, validateProviders(state)...)
	errs = append(errs, validateInfraComponents(state)...)
	errs = append(errs, validateClusterInfras(state)...)
	errs = append(errs, validateContainerClusters(state)...)
	errs = append(errs, validateCrossLayer(state)...)
	errs = append(errs, validateSecretReferences(state)...)
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// IsDNSLabel returns whether s matches the canonical
// `[a-z0-9]([-a-z0-9]*[a-z0-9])?` DNS-label form.
func IsDNSLabel(s string) bool { return dnsLabel.MatchString(s) }

func validateName(kind, name string) string {
	if name == "" {
		return fmt.Sprintf("%s.metadata.name is required", kind)
	}
	if !dnsLabel.MatchString(name) {
		return fmt.Sprintf("%s.metadata.name %q is not a DNS label", kind, name)
	}
	return ""
}

// validateCrossLayer enforces the few rules that span multiple kinds:
// one ContainerCluster owner per ClusterInfra in v1, the proxy /
// registry single-source-of-truth rule, and the disconnected-install
// requirement.
func validateCrossLayer(state v1alpha1.State) []string {
	var errs []string
	infraToOCP := map[string][]string{}
	for _, ocp := range state.ContainerClusters {
		for _, node := range ocp.Spec.Nodes {
			name := node.MachineRef.ClusterInfra
			if name == "" {
				continue
			}
			owners := infraToOCP[name]
			found := false
			for _, owner := range owners {
				if owner == ocp.Metadata.Name {
					found = true
					break
				}
			}
			if !found {
				infraToOCP[name] = append(infraToOCP[name], ocp.Metadata.Name)
			}
		}
	}
	for _, ci := range state.ClusterInfras {
		owners := infraToOCP[ci.Metadata.Name]
		if len(owners) > 1 {
			errs = append(errs, fmt.Sprintf("ClusterInfra/%s is referenced by multiple ContainerClusters: %s", ci.Metadata.Name, strings.Join(owners, ", ")))
		}
	}
	errs = append(errs, validateProxySourceConsistency(state)...)
	errs = append(errs, validateRegistrySourceConsistency(state)...)
	errs = append(errs, validateDisconnectedRequiresRegistry(state)...)
	errs = append(errs, validateArtifactServerRequirements(state)...)
	errs = append(errs, validateSharedProviderServices(state)...)
	return errs
}

// validateProxySourceConsistency rejects the case where Environment
// carries an external proxy URL and any cluster carries a managed
// `components.proxy`. Managed proxy URL is derived from
// `(hostRef, port)`; the operator MUST NOT also declare an external one.
func validateProxySourceConsistency(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	if env == nil || env.Spec.Proxy == nil {
		return nil
	}
	hasExternalURL := env.Spec.Proxy.HTTPProxy != "" || env.Spec.Proxy.HTTPSProxy != ""
	if !hasExternalURL {
		return nil
	}
	var managed []string
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.Proxy != nil {
			managed = append(managed, ci.Metadata.Name)
		}
	}
	if len(managed) == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"Environment/%s spec.proxy.{httpProxy,httpsProxy} set AND ClusterInfra/%s has spec.components.proxy: managed proxy URL is derived from (hostRef, port); external URL on Environment is forbidden when any cluster declares a managed proxy",
		env.Metadata.Name, strings.Join(managed, ","))}
}

func validateRegistrySourceConsistency(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	if env == nil || env.Spec.Registries == nil || env.Spec.Registries.Mirror == nil || env.Spec.Registries.Mirror.URL == "" {
		return nil
	}
	var managed []string
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.Registry != nil {
			managed = append(managed, ci.Metadata.Name)
		}
	}
	if len(managed) == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"Environment/%s spec.registries.mirror.url set AND ClusterInfra/%s has spec.components.registry: managed mirror URL is derived from (hostRef, port); external URL on Environment is forbidden when any cluster declares a managed registry",
		env.Metadata.Name, strings.Join(managed, ","))}
}

// validateDisconnectedRequiresRegistry checks ContainerCluster
// `install.mode: disconnected`: the environment must declare mirror
// metadata/trust material, and there must be one endpoint source:
// either external (Environment URL) OR managed (any cluster's
// components.registry).
func validateDisconnectedRequiresRegistry(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	if env == nil {
		return nil
	}
	var disconnected []string
	for _, ocp := range state.ContainerClusters {
		if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
			disconnected = append(disconnected, ocp.Metadata.Name)
		}
	}
	if len(disconnected) == 0 {
		return nil
	}
	if env.Spec.Registries == nil || env.Spec.Registries.Mirror == nil {
		return []string{fmt.Sprintf(
			"ContainerCluster/%s install.mode=disconnected requires Environment/%s spec.registries.mirror with trust material and either a url (external) or a managed ClusterInfra.spec.components.registry",
			strings.Join(disconnected, ","), env.Metadata.Name)}
	}
	mirror := env.Spec.Registries.Mirror
	if mirror.TrustBundleRef.Name == "" {
		return []string{fmt.Sprintf(
			"ContainerCluster/%s install.mode=disconnected requires Environment/%s spec.registries.mirror.trustBundleRef.name",
			strings.Join(disconnected, ","), env.Metadata.Name)}
	}
	hasExternalMirror := mirror.URL != ""
	hasManagedRegistry := false
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.Registry != nil {
			hasManagedRegistry = true
			break
		}
	}
	if hasExternalMirror || hasManagedRegistry {
		return nil
	}
	return []string{fmt.Sprintf(
		"ContainerCluster/%s install.mode=disconnected requires one of: Environment.spec.registries.mirror.url (external) OR at least one ClusterInfra.spec.components.registry (managed)",
		strings.Join(disconnected, ","))}
}

func validateArtifactServerRequirements(state v1alpha1.State) []string {
	infraIndex := indexClusterInfras(state.ClusterInfras)
	env := primaryEnvironment(&state)
	server, hasServer := artifactpub.Select(state)
	var errs []string
	for _, ocp := range state.ContainerClusters {
		ci, ok, _ := resolveContainerClusterInfra(ocp, infraIndex)
		if !ok || !artifactpub.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		prefix := fmt.Sprintf("ContainerCluster/%s", ocp.Metadata.Name)
		if env == nil || env.Spec.ArtifactServer == nil || env.Spec.ArtifactServer.ComponentRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s requires generated artifact publication; set Environment.spec.artifactServer.componentRef.name", prefix))
			continue
		}
		if !hasServer {
			errs = append(errs, fmt.Sprintf("%s requires generated artifact publication; Environment/%s spec.artifactServer.componentRef.name %q does not resolve to an InfraComponent artifact server", prefix, env.Metadata.Name, env.Spec.ArtifactServer.ComponentRef.Name))
			continue
		}
		if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected && !artifactpub.RouteAvailable(server, env.Spec.ArtifactServer.Routes.ClusterInstall.Endpoint) {
			errs = append(errs, fmt.Sprintf("%s install.mode=disconnected requires Environment/%s spec.artifactServer.routes.clusterInstall.endpoint to resolve on InfraComponent/%s spec.artifactServer.endpoints",
				prefix, env.Metadata.Name, server.Component.Metadata.Name))
		}
		if artifactpub.ClusterUsesBareMetalMachine(state, ci) && !artifactpub.RouteAvailable(server, env.Spec.ArtifactServer.Routes.RedfishVirtualMedia.Endpoint) {
			errs = append(errs, fmt.Sprintf("%s bare-metal Redfish boot requires Environment/%s spec.artifactServer.routes.redfishVirtualMedia.endpoint to resolve on InfraComponent/%s spec.artifactServer.endpoints",
				prefix, env.Metadata.Name, server.Component.Metadata.Name))
		}
	}
	return errs
}

// validateSecretReferences walks every SecretRef in the loaded state
// and rejects those that point at undeclared names. Validation runs
// after the per-kind validators so the `Environment` cardinality rule
// has already fired if it would; this loop tolerates a missing env.
func validateSecretReferences(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	if env == nil {
		return nil
	}
	declared := map[string]bool{}
	for name := range env.Spec.Secrets {
		declared[name] = true
	}
	var errs []string
	require := func(owner string, ref v1alpha1.SecretRef) {
		if ref.Name == "" {
			return
		}
		if !dnsLabel.MatchString(ref.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q is not a DNS label", owner, ref.Name))
			return
		}
		if !declared[ref.Name] {
			errs = append(errs, fmt.Sprintf("%s %q is not declared in Environment/%s spec.secrets", owner, ref.Name, env.Metadata.Name))
		}
	}
	requireTLS := func(owner string, ref v1alpha1.SecretRef) {
		require(owner, ref)
		if ref.Name == "" || !declared[ref.Name] {
			return
		}
		spec := env.Spec.Secrets[ref.Name]
		if spec.File != "" && spec.KeyFile == "" && spec.Generated == nil {
			errs = append(errs, fmt.Sprintf("%s %q uses file-sourced TLS material but Environment/%s spec.secrets[%s].keyFile is empty", owner, ref.Name, env.Metadata.Name, ref.Name))
		}
	}
	if env.Spec.Proxy != nil && env.Spec.Proxy.Auth != nil {
		require(fmt.Sprintf("Environment/%s spec.proxy.auth.proxyAuthRef", env.Metadata.Name), env.Spec.Proxy.Auth.ProxyAuthRef)
	}
	if env.Spec.ClusterTrust != nil {
		for i, ref := range env.Spec.ClusterTrust.CABundleRefs {
			require(fmt.Sprintf("Environment/%s spec.clusterTrust.caBundleRefs[%d]", env.Metadata.Name, i), ref)
		}
	}
	if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil {
		owner := fmt.Sprintf("Environment/%s spec.registries.mirror", env.Metadata.Name)
		require(owner+".credentialsRef", registries.Mirror.CredentialsRef)
		require(owner+".trustBundleRef", registries.Mirror.TrustBundleRef)
	}
	for _, h := range state.Hosts {
		if h.Spec.SSH != nil {
			require(fmt.Sprintf("Host/%s spec.ssh.keyRef", h.Metadata.Name), h.Spec.SSH.KeyRef)
		}
	}
	for _, p := range state.InfraProviders {
		for _, mp := range p.Spec.MachineProfiles {
			if mp.Libvirt != nil && mp.Libvirt.BMCEmulationDefaults != nil &&
				mp.Libvirt.BMCEmulationDefaults.Auth != nil {
				require(fmt.Sprintf("InfraProvider/%s spec.machineProfiles[%s].libvirt.bmcEmulationDefaults.auth.credentialRef",
					p.Metadata.Name, mp.Name), mp.Libvirt.BMCEmulationDefaults.Auth.CredentialRef)
			}
			if mp.VSphere != nil {
				for i, vc := range mp.VSphere.VCenters {
					require(fmt.Sprintf("InfraProvider/%s spec.machineProfiles[%s].vsphere.vcenters[%d].credentialsRef",
						p.Metadata.Name, mp.Name, i), vc.CredentialsRef)
				}
			}
		}
		for _, m := range p.Spec.Machines {
			if m.BareMetal != nil {
				require(fmt.Sprintf("InfraProvider/%s spec.machines[%s].baremetal.bmc.credentialsRef",
					p.Metadata.Name, m.Name), m.BareMetal.BMC.CredentialsRef)
			}
		}
	}
	for _, ocp := range state.ContainerClusters {
		require(fmt.Sprintf("ContainerCluster/%s install.pullSecretRef", ocp.Metadata.Name), ocp.Spec.Install.PullSecretRef)
		require(fmt.Sprintf("ContainerCluster/%s install.sshKeyRef", ocp.Metadata.Name), ocp.Spec.Install.SSHKeyRef)
		for i, ref := range ocp.Spec.Install.AdditionalTrustBundleRefs {
			require(fmt.Sprintf("ContainerCluster/%s install.additionalTrustBundleRefs[%d]", ocp.Metadata.Name, i), ref)
		}
		if serving := ocp.Spec.Install.ServingCertificates; serving != nil {
			if api := serving.APIServer; api != nil {
				for i, cert := range api.NamedCertificates {
					requireTLS(fmt.Sprintf("ContainerCluster/%s install.servingCertificates.apiServer.namedCertificates[%d].secretRef", ocp.Metadata.Name, i), cert.SecretRef)
				}
			}
			if ingress := serving.Ingress; ingress != nil {
				requireTLS(fmt.Sprintf("ContainerCluster/%s install.servingCertificates.ingress.defaultCertificateRef", ocp.Metadata.Name), ingress.DefaultCertificateRef)
			}
		}
	}
	return errs
}
