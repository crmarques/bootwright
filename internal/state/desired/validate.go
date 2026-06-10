package desiredstate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Validate is the single entry point. Every rule in
// `specs/state-model.md § Validation Rules` lives behind one of the
// per-kind validators called below. Each rule emits a precise
// diagnostic naming the field and the conflicting value.
func Validate(state v1alpha1.State) error {
	var errs []string
	errs = append(errs, validateEnvironments(state)...)
	errs = append(errs, validateMachines(state)...)
	errs = append(errs, validateNetworkConfigs(state)...)
	errs = append(errs, validateProviders(state)...)
	errs = append(errs, validateInfraComponents(state)...)
	errs = append(errs, validateContainerClusters(state)...)
	errs = append(errs, validateClusterAddons(state)...)
	errs = append(errs, validateClusterAddonProfiles(state)...)
	errs = append(errs, validateClusterAddonBindings(state)...)
	errs = append(errs, validateStorage(state)...)
	errs = append(errs, validateCrossLayer(state)...)
	errs = append(errs, validateSecretReferences(state)...)
	if len(errs) == 0 {
		return nil
	}
	return newValidationError(errs)
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var dnsSubdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
var labelName = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
var labelValue = regexp.MustCompile(`^([A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?)?$`)

// IsDNSLabel returns whether s matches the canonical
// `[a-z0-9]([-a-z0-9]*[a-z0-9])?` DNS-label form.
func IsDNSLabel(s string) bool { return dnsLabel.MatchString(s) }

func isLabelKey(s string) bool {
	prefix, name, ok := strings.Cut(s, "/")
	if !ok {
		name = prefix
		prefix = ""
	}
	if name == "" || len(name) > 63 || !labelName.MatchString(name) {
		return false
	}
	if prefix == "" {
		return true
	}
	if len(prefix) > 253 || !dnsSubdomain.MatchString(prefix) {
		return false
	}
	for _, part := range strings.Split(prefix, ".") {
		if len(part) > 63 {
			return false
		}
	}
	return true
}

func isLabelValue(s string) bool {
	return len(s) <= 63 && labelValue.MatchString(s)
}

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
// one ContainerCluster owner per ClusterInstall in v1, the proxy /
// registry single-source-of-truth rule, the disconnected-install
// requirement, and fleet-wide machine node-binding exclusivity.
func validateCrossLayer(state v1alpha1.State) []string {
	var errs []string
	errs = append(errs, validateDisconnectedRequiresRegistry(state)...)
	errs = append(errs, validateArtifactServerRequirements(state)...)
	errs = append(errs, validateSharedMachineServices(state)...)
	errs = append(errs, validateMachineNodeBindings(state)...)
	errs = append(errs, validateKubeVirtHostClusterDependencies(state)...)
	errs = append(errs, validateMachineImageEntitlements(state)...)
	return errs
}

// validateMachineNodeBindings enforces node-binding exclusivity across the
// whole fleet: a Machine backs at most one ContainerCluster spec.hosts[] or
// StorageCluster spec.ceph.topology.hosts[] entry. Normalize defaults an
// omitted container host machineRef to the hostname, so two clusters reusing
// a hostname would otherwise silently capture (and re-install) the same
// Machine.
func validateMachineNodeBindings(state v1alpha1.State) []string {
	type binding struct {
		cluster string
		field   string
	}
	bindings := map[string]binding{}
	var errs []string
	bind := func(machine, cluster, field string) {
		if machine == "" {
			return
		}
		existing, ok := bindings[machine]
		if !ok {
			bindings[machine] = binding{cluster: cluster, field: field}
			return
		}
		if existing.cluster == cluster {
			errs = append(errs, fmt.Sprintf("%s %s.machineRef %q is already node-bound by %s in the same cluster", cluster, field, machine, existing.field))
			return
		}
		errs = append(errs, fmt.Sprintf("%s %s.machineRef %q is already node-bound by %s %s; a Machine may be node-bound by at most one cluster", cluster, field, machine, existing.cluster, existing.field))
	}
	for _, ocp := range state.ContainerClusters {
		cluster := fmt.Sprintf("ContainerCluster/%s", ocp.Metadata.Name)
		for i, node := range ocp.Spec.Hosts {
			bind(node.MachineRef.Name, cluster, fmt.Sprintf("spec.hosts[%d]", i))
		}
	}
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		cluster := fmt.Sprintf("StorageCluster/%s", sc.Metadata.Name)
		for i, node := range sc.Spec.Ceph.Topology.Hosts {
			bind(node.MachineRef.Name, cluster, fmt.Sprintf("spec.ceph.topology.hosts[%d]", i))
		}
	}
	return errs
}

func validateMachineImageEntitlements(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	var errs []string
	for _, image := range state.MachineImages {
		source := image.Spec.InstallSource
		if machineImageInstallSourceType(source) != v1alpha1.MachineImageInstallSourceTypeRHSM {
			continue
		}
		ref := source.EntitlementRef.Name
		if ref == "" {
			continue
		}
		entitlement, ok := entitlements.Find(env, ref)
		if !ok {
			errs = append(errs, fmt.Sprintf("MachineImage/%s spec.installSource.entitlementRef %q does not match any Environment.spec.entitlements[].name", image.Metadata.Name, ref))
			continue
		}
		if entitlement.Provider != v1alpha1.EntitlementProviderRedHat {
			errs = append(errs, fmt.Sprintf("MachineImage/%s spec.installSource.entitlementRef %q resolves to provider %q, want %q", image.Metadata.Name, ref, entitlement.Provider, v1alpha1.EntitlementProviderRedHat))
		}
		if entitlement.Product != v1alpha1.EntitlementProductRHEL {
			errs = append(errs, fmt.Sprintf("MachineImage/%s spec.installSource.entitlementRef %q resolves to product %q, want %q", image.Metadata.Name, ref, entitlement.Product, v1alpha1.EntitlementProductRHEL))
		}
	}
	return errs
}

func validateKubeVirtHostClusterDependencies(state v1alpha1.State) []string {
	providers := indexProviders(state.InfraProviders)
	machines := indexMachines(state.Machines)
	clusters := indexContainerClusters(state.ContainerClusters)
	provided := providedClusterCapabilities(state)
	deps := map[string][]string{}
	var errs []string
	for _, ocp := range state.ContainerClusters {
		for _, node := range ocp.Spec.Hosts {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
			if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil || provider.Spec.KubeVirt.HostClusterRef.Name == "" {
				continue
			}
			parent := provider.Spec.KubeVirt.HostClusterRef.Name
			deps[ocp.Metadata.Name] = appendUnique(deps[ocp.Metadata.Name], parent)
			if _, ok := clusters[parent]; !ok {
				continue
			}
			if !provided[parent][v1alpha1.ClusterAddonProvidesKubeVirt] {
				errs = append(errs, fmt.Sprintf("InfraProvider/%s spec.kubevirt.hostClusterRef %q requires a ClusterAddonBinding that applies a ClusterAddon providing %q to ContainerCluster/%s",
					provider.Metadata.Name, parent, v1alpha1.ClusterAddonProvidesKubeVirt, parent))
			}
		}
	}
	errs = append(errs, validateClusterDependencyCycles(deps)...)
	return errs
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func validateClusterDependencyCycles(deps map[string][]string) []string {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var errs []string
	var visit func(string, []string)
	visit = func(name string, stack []string) {
		if visited[name] {
			return
		}
		if visiting[name] {
			cycle := append(stack, name)
			errs = append(errs, fmt.Sprintf("KubeVirt hostClusterRef creates ContainerCluster dependency cycle: %s", strings.Join(cycle, " -> ")))
			return
		}
		visiting[name] = true
		stack = append(stack, name)
		for _, dep := range deps[name] {
			visit(dep, stack)
		}
		visiting[name] = false
		visited[name] = true
	}
	for name := range deps {
		visit(name, nil)
	}
	return errs
}

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
			"ContainerCluster/%s install.mode=disconnected requires Environment/%s spec.registries.mirror with trust material and either a url (external) or a managed registry infra component",
			strings.Join(disconnected, ","), env.Metadata.Name)}
	}
	mirror := env.Spec.Registries.Mirror
	if mirror.TrustBundleRef.Name == "" {
		return []string{fmt.Sprintf(
			"ContainerCluster/%s install.mode=disconnected requires Environment/%s spec.registries.mirror.trustBundleRef",
			strings.Join(disconnected, ","), env.Metadata.Name)}
	}
	hasExternalMirror := mirror.URL != ""
	hasManagedRegistry := selectedManagedRegistry(env) != nil
	if hasExternalMirror || hasManagedRegistry {
		return nil
	}
	return []string{fmt.Sprintf(
		"ContainerCluster/%s install.mode=disconnected requires one of: Environment.spec.registries.mirror.url (external) OR a managed Environment.spec.infraComponents.registries entry",
		strings.Join(disconnected, ","))}
}

func selectedManagedRegistry(env *v1alpha1.Environment) *v1alpha1.EnvironmentRegistryComponent {
	if env == nil {
		return nil
	}
	for i := range env.Spec.InfraComponents.Registries {
		entry := &env.Spec.InfraComponents.Registries[i]
		if entry.Type == v1alpha1.EnvironmentComponentManaged && entry.Default {
			return entry
		}
	}
	if len(env.Spec.InfraComponents.Registries) == 1 && env.Spec.InfraComponents.Registries[0].Type == v1alpha1.EnvironmentComponentManaged {
		return &env.Spec.InfraComponents.Registries[0]
	}
	return nil
}

func validateArtifactServerRequirements(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	var errs []string
	for _, ocp := range state.ContainerClusters {
		ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
		if !ok || !artifacts.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		prefix := fmt.Sprintf("ContainerCluster/%s", ocp.Metadata.Name)
		if env == nil || len(env.Spec.InfraComponents.ArtifactServers) == 0 {
			errs = append(errs, fmt.Sprintf("%s requires generated artifact publication; set Environment.spec.infraComponents.artifactServers", prefix))
			continue
		}
		if ci.ArtifactAccess.ServerRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s requires generated artifact publication; set spec.install.artifactAccess.serverRef", prefix))
			continue
		}
		if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
			if _, _, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerContainerClusterInstall); !ok {
				errs = append(errs, fmt.Sprintf("%s install.mode=disconnected requires spec.install.artifactAccess.containerClusterInstall.endpointRef to resolve on the selected artifact server",
					prefix))
			}
		}
		if artifacts.ClusterUsesBareMetalMachine(state, ci) {
			if _, _, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerRedfishVirtualMedia); !ok {
				errs = append(errs, fmt.Sprintf("%s bare-metal Redfish boot requires spec.install.artifactAccess.redfishVirtualMedia.endpointRef to resolve on the selected artifact server",
					prefix))
			}
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
	requireSSHKey := func(owner string, ref v1alpha1.SecretRef) {
		require(owner, ref)
		if ref.Name == "" || !declared[ref.Name] {
			return
		}
		spec := env.Spec.Secrets[ref.Name]
		if spec.Generated != nil && spec.Generated.SSHKeyPair == nil {
			errs = append(errs, fmt.Sprintf("%s %q uses generated material but Environment/%s spec.secrets[%s].generated is not sshKeyPair", owner, ref.Name, env.Metadata.Name, ref.Name))
		}
	}
	for i, entry := range env.Spec.InfraComponents.Proxies {
		if entry.Connection != nil && entry.Connection.Auth != nil {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.proxies[%d].connection.auth.proxyAuthRef", env.Metadata.Name, i), entry.Connection.Auth.ProxyAuthRef)
		}
	}
	if env.Spec.InstallTrust != nil {
		for i, ref := range env.Spec.InstallTrust.CABundleRefs {
			require(fmt.Sprintf("Environment/%s spec.installTrust.caBundleRefs[%d]", env.Metadata.Name, i), ref)
		}
	}
	require(fmt.Sprintf("Environment/%s spec.defaults.install.pullSecretRef", env.Metadata.Name), env.Spec.Defaults.Install.PullSecretRef)
	requireNodeSSH(fmt.Sprintf("Environment/%s spec.defaults.install.nodeSSH", env.Metadata.Name), env.Spec.Defaults.Install.NodeSSH, requireSSHKey)
	if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil {
		owner := fmt.Sprintf("Environment/%s spec.registries.mirror", env.Metadata.Name)
		require(owner+".credentialsRef", registries.Mirror.CredentialsRef)
		require(owner+".trustBundleRef", registries.Mirror.TrustBundleRef)
	}
	for i, entitlement := range env.Spec.Entitlements {
		owner := fmt.Sprintf("Environment/%s spec.entitlements[%d]", env.Metadata.Name, i)
		if entitlement.RHSM != nil {
			require(owner+".rhsm.organizationRef", entitlement.RHSM.OrganizationRef)
			require(owner+".rhsm.activationKeyRef", entitlement.RHSM.ActivationKeyRef)
		}
		if entitlement.Registry != nil {
			require(owner+".registry.credentialsRef", entitlement.Registry.CredentialsRef)
			require(owner+".registry.trustBundleRef", entitlement.Registry.TrustBundleRef)
		}
	}
	for _, machine := range state.Machines {
		if machine.Spec.Access.SSH != nil {
			requireSSHKey(fmt.Sprintf("Machine/%s spec.access.ssh.keyRef", machine.Metadata.Name), machine.Spec.Access.SSH.KeyRef)
			if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
				require(fmt.Sprintf("Machine/%s spec.access.ssh.knownHostsRef", machine.Metadata.Name), machine.Spec.Access.SSH.KnownHostsRef)
			}
		}
		if machine.Spec.Hardware.Management.BMC.CredentialsRef.Name != "" {
			require(fmt.Sprintf("Machine/%s spec.hardware.management.bmc.credentialsRef", machine.Metadata.Name), machine.Spec.Hardware.Management.BMC.CredentialsRef)
		}
	}
	for _, image := range state.MachineImages {
		for i, ref := range image.Spec.TrustRefs {
			require(fmt.Sprintf("MachineImage/%s spec.trustRefs[%d]", image.Metadata.Name, i), ref)
		}
		for i, ref := range image.Spec.HeadersRefs {
			require(fmt.Sprintf("MachineImage/%s spec.headersRefs[%d]", image.Metadata.Name, i), ref)
		}
	}
	for _, profile := range state.MachineInstallProfiles {
		if profile.Spec.Installer.Anaconda == nil {
			continue
		}
		for i, repo := range profile.Spec.Installer.Anaconda.Repositories {
			if strings.HasPrefix(repo.BaseURL, "bootwright-secret-ref:") {
				require(fmt.Sprintf("MachineInstallProfile/%s spec.installer.anaconda.repositories[%d].baseURL", profile.Metadata.Name, i), v1alpha1.SecretRef{Name: strings.TrimPrefix(repo.BaseURL, "bootwright-secret-ref:")})
			}
		}
	}
	for _, p := range state.InfraProviders {
		if p.Spec.Libvirt != nil && p.Spec.Libvirt.BMCEmulationDefaults != nil && p.Spec.Libvirt.BMCEmulationDefaults.Auth != nil {
			require(fmt.Sprintf("InfraProvider/%s spec.libvirt.bmcEmulationDefaults.auth.credentialsRef",
				p.Metadata.Name), p.Spec.Libvirt.BMCEmulationDefaults.Auth.CredentialsRef)
		}
		if p.Spec.BareMetal != nil && p.Spec.BareMetal.Defaults.BMC != nil {
			require(fmt.Sprintf("InfraProvider/%s spec.bareMetal.defaults.bmc.credentialsRef",
				p.Metadata.Name), p.Spec.BareMetal.Defaults.BMC.CredentialsRef)
		}
		if p.Spec.VSphere != nil {
			for i, vc := range p.Spec.VSphere.VCenters {
				require(fmt.Sprintf("InfraProvider/%s spec.vsphere.vcenters[%d].credentialsRef",
					p.Metadata.Name, i), vc.CredentialsRef)
			}
		}
		if p.Spec.KubeVirt != nil && p.Spec.KubeVirt.KubeconfigRef != nil {
			require(fmt.Sprintf("InfraProvider/%s spec.kubevirt.kubeconfigRef",
				p.Metadata.Name), *p.Spec.KubeVirt.KubeconfigRef)
		}
	}
	for _, ocp := range state.ContainerClusters {
		require(fmt.Sprintf("ContainerCluster/%s install.pullSecretRef", ocp.Metadata.Name), ocp.Spec.Install.PullSecretRef)
		requireNodeSSH(fmt.Sprintf("ContainerCluster/%s install.nodeSSH", ocp.Metadata.Name), ocp.Spec.Install.NodeSSH, requireSSHKey)
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
	for _, effective := range addoninputs.EffectiveAddons(state) {
		accepted := map[string]v1alpha1.ClusterAddonAcceptedInput{}
		for _, input := range effective.Extension.Spec.Accepts.Inputs {
			accepted[input.Name] = input
		}
		for _, input := range effective.Addon.Inputs {
			acceptedInput, ok := accepted[input.Name]
			if !ok {
				continue
			}
			for name, property := range acceptedInput.Schema.Properties {
				if !property.SecretRef {
					continue
				}
				if ref := addoninputs.SecretRefValue(input.Values, name); ref.Name != "" {
					require(fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s].values.%s", effective.Binding.Metadata.Name, effective.Addon.Name, input.Name, name), ref)
				}
			}
		}
	}
	return errs
}

func requireNodeSSH(owner string, spec v1alpha1.NodeSSHSpec, requireSSHKey func(string, v1alpha1.SecretRef)) {
	if spec.KeyPairRef.Name != "" {
		requireSSHKey(owner+".keyPairRef", spec.KeyPairRef)
	}
	if spec.PublicKeyRef.Name != "" {
		requireSSHKey(owner+".publicKeyRef", spec.PublicKeyRef)
	}
	if spec.PrivateKeyRef.Name != "" {
		requireSSHKey(owner+".privateKeyRef", spec.PrivateKeyRef)
	}
}
