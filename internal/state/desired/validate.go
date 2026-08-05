package desiredstate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/state/view"
)

func Validate(state v1alpha1.State) error {
	findings := validateFindings(state)
	if len(findings) == 0 {
		return nil
	}
	return newValidationError(findings)
}

func validateFindings(state v1alpha1.State) []Finding {
	var errs []Finding
	errs = append(errs, notes(validateEnvironments(state))...)
	errs = append(errs, notes(validateEntitlements(state))...)
	errs = append(errs, notes(validateMachines(state))...)
	errs = append(errs, notes(validateSites(state))...)
	errs = append(errs, validateNetworkConfigs(state)...)
	errs = append(errs, notes(validateProviders(state))...)
	errs = append(errs, notes(validateInfraComponents(state))...)
	errs = append(errs, notes(validateContainerClusters(state))...)
	errs = append(errs, notes(validateClusterAddons(state))...)
	errs = append(errs, notes(validateAddonCatalogCopies(state))...)
	errs = append(errs, notes(validateClusterAddonProfiles(state))...)
	errs = append(errs, notes(validateClusterAddonBindings(state))...)
	errs = append(errs, notes(validateCustomPlaybooks(state))...)
	errs = append(errs, notes(validateStorage(state))...)
	errs = append(errs, notes(validateCrossLayer(state))...)
	errs = append(errs, notes(validateSecrets(state))...)
	errs = append(errs, notes(validateSecretReferences(state))...)
	errs = append(errs, notes(validateUniqueBMCAddresses(state))...)
	errs = append(errs, notes(validateUniqueMachineSSHAddresses(state))...)
	errs = append(errs, notes(validateUniqueMachineFQDNs(state))...)
	errs = append(errs, notes(validateClusterBoundHostnameSource(state))...)
	errs = append(errs, notes(validateAnacondaRootDeviceHintsAreUsable(state))...)
	errs = append(errs, notes(validateMachineInstallCloneSubstrate(state))...)
	errs = append(errs, notes(validateManagedOSCephNodeRootDisk(state))...)
	errs = append(errs, notes(validateOSDDevicesExcludeRootDisk(state))...)
	errs = append(errs, notes(validateDiskEncryptionHasATPM(state))...)
	errs = append(errs, duplicateNameFindings(state)...)
	return errs
}

func validateUniqueMachineFQDNs(state v1alpha1.State) []string {
	byName := map[string][]string{}
	for _, machine := range state.Machines {
		fqdn := strings.TrimSpace(v1alpha1.MachineFQDNAddress(machine))
		if fqdn == "" {
			continue
		}
		byName[fqdn] = append(byName[fqdn], machine.Metadata.Name)
	}
	entries := make([]string, 0, len(byName))
	for entry := range byName {
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	var errs []string
	for _, entry := range entries {
		names := byName[entry]
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		errs = append(errs, fmt.Sprintf("fqdn %q is used by more than one Machine (%s); the fqdn name is the machine's canonical DNS identity and connection address, so a shared value would resolve — and could drive — the wrong machine. Give each Machine a unique fqdn address", entry, strings.Join(names, ", ")))
	}
	return errs
}

func validateClusterBoundHostnameSource(state v1alpha1.State) []string {
	var errs []string
	for _, machine := range state.Machines {
		if !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
		if !ok || profile.Spec.Customizations.Hostname.Source != v1alpha1.MachineInstallHostnameMachineName {
			continue
		}
		if binding, ok := stateview.MachineCluster(state, machine.Metadata.Name); ok {
			errs = append(errs, fmt.Sprintf("Machine/%s references MachineInstallProfile/%s with customizations.hostname.source %q, but the machine is bound to cluster %q as a node; a cluster node's OS hostname must equal its node FQDN, so the machine-name hostname source is only valid for machines not bound to any cluster", machine.Metadata.Name, profile.Metadata.Name, v1alpha1.MachineInstallHostnameMachineName, binding.Cluster))
		}
	}
	return errs
}

func validateUniqueMachineSSHAddresses(state v1alpha1.State) []string {
	byAddress := map[string][]string{}
	for _, machine := range state.Machines {
		address := strings.TrimSpace(stateview.MachineSSHAddressByName(state, machine.Metadata.Name))
		if address == "" {
			continue
		}
		byAddress[address] = append(byAddress[address], machine.Metadata.Name)
	}
	addresses := make([]string, 0, len(byAddress))
	for address := range byAddress {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	var errs []string
	for _, address := range addresses {
		names := byAddress[address]
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		errs = append(errs, fmt.Sprintf("SSH address %q is used by more than one Machine (%s); an address reaches one host, so a shared value would let bootwright drive — and could reinstall — the wrong machine. Give each Machine a unique routable address", address, strings.Join(names, ", ")))
	}
	return errs
}

func validateUniqueBMCAddresses(state v1alpha1.State) []string {
	type group struct {
		display string
		names   []string
	}
	byKey := map[string]*group{}
	for _, machine := range state.Machines {
		address := strings.TrimSpace(machine.Spec.Hardware.Management.BMC.Address)
		if address == "" {
			continue
		}
		key := normalizeBMCAddressKey(address)
		g, ok := byKey[key]
		if !ok {
			g = &group{display: address}
			byKey[key] = g
		}
		g.names = append(g.names, machine.Metadata.Name)
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var errs []string
	for _, key := range keys {
		g := byKey[key]
		if len(g.names) < 2 {
			continue
		}
		sort.Strings(g.names)
		errs = append(errs, fmt.Sprintf("BMC address %q is declared by more than one Machine (%s); a BMC endpoint identifies one physical host, so a shared address would drive — and could disk-wipe — the wrong machine. Give each Machine a unique hardware.management.bmc.address", g.display, strings.Join(g.names, ", ")))
	}
	return errs
}

func normalizeBMCAddressKey(addr string) string {
	base, systemID := normalizeRedfishEndpoint(addr)
	return base + "|" + systemID
}

func normalizeRedfishEndpoint(addr string) (base, systemID string) {
	s := normalizeRedfishScheme(strings.TrimSpace(addr))
	const systemsMarker = "/redfish/v1/Systems/"
	if i := strings.Index(s, systemsMarker); i >= 0 {
		b := s[:i]
		rest := strings.TrimSuffix(s[i+len(systemsMarker):], "/")
		if rest != "" && !strings.Contains(rest, "/") {
			return b, rest
		}
		return b, ""
	}
	return strings.TrimRight(s, "/"), ""
}

func normalizeRedfishScheme(addr string) string {
	i := strings.Index(addr, "://")
	if i <= 0 {
		return addr
	}
	scheme := strings.ToLower(addr[:i])
	suffix := addr[i:]
	if j := strings.LastIndex(scheme, "+"); j >= 0 {
		switch transport := scheme[j+1:]; transport {
		case "http", "https":
			return transport + suffix
		}
	}
	switch scheme {
	case "redfish", "redfish-virtualmedia":
		return "https" + suffix
	default:
		return addr
	}
}

func duplicateNameFindings(state v1alpha1.State) []Finding {
	var out []Finding
	for _, accessor := range v1alpha1.AuthoredKindAccessors() {
		switch accessor.Kind {
		case v1alpha1.KindEnvironment, v1alpha1.KindNetworkConfig:
			continue
		}
		out = append(out, duplicateFindings(accessor.Kind, accessor.Names(state))...)
	}
	return out
}

func duplicateFindings(kind string, names []string) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, name := range names {
		if validateName(kind, name) != "" {
			continue
		}
		if seen[name] {
			out = append(out, diagValue(kind+"/"+name, "", name, fmt.Sprintf("duplicate %s %q", kind, name)))
		}
		seen[name] = true
	}
	return out
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var dnsSubdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
var labelName = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?$`)
var labelValue = regexp.MustCompile(`^([A-Za-z0-9]([-A-Za-z0-9_.]*[A-Za-z0-9])?)?$`)

func IsDNSLabel(s string) bool { return dnsLabel.MatchString(s) }

func isDNSSubdomainName(s string) bool {
	if s == "" || len(s) > 253 || !dnsSubdomain.MatchString(s) {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if len(part) > 63 {
			return false
		}
	}
	return true
}

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

func validateCrossLayer(state v1alpha1.State) []string {
	var errs []string
	errs = append(errs, validateClusterRootNameCollisions(state)...)
	errs = append(errs, validateReservedClusterRootNames(state)...)
	errs = append(errs, validateDisconnectedRequiresRegistry(state)...)
	errs = append(errs, validateArtifactServerRequirements(state)...)
	errs = append(errs, validateSharedMachineServices(state)...)
	errs = append(errs, validateMachineNodeBindings(state)...)
	errs = append(errs, validateKubeVirtHostClusterDependencies(state)...)
	errs = append(errs, validateMachineInstallProfileEntitlements(state)...)
	return errs
}

func validateClusterRootNameCollisions(state v1alpha1.State) []string {
	container := map[string]bool{}
	for _, ocp := range state.ContainerClusters {
		container[ocp.Metadata.Name] = true
	}
	seen := map[string]bool{}
	var errs []string
	for _, sc := range state.StorageClusters {
		name := sc.Metadata.Name
		if !container[name] || seen[name] {
			continue
		}
		seen[name] = true
		errs = append(errs, fmt.Sprintf("StorageCluster/%s metadata.name %q is already used by ContainerCluster/%s; ContainerCluster and StorageCluster names share one cluster selection namespace (--clusters, Environment cluster lists)", name, name, name))
	}
	return errs
}

const reservedClusterRootName = "artifact-server"

func validateReservedClusterRootNames(state v1alpha1.State) []string {
	var errs []string
	for _, c := range state.ContainerClusters {
		if c.Metadata.Name == reservedClusterRootName {
			errs = append(errs, fmt.Sprintf("ContainerCluster/%s metadata.name %q is reserved: it collides with the `destroy --stage infra --clusters artifact-server` literal; rename the cluster", reservedClusterRootName, reservedClusterRootName))
		}
	}
	for _, c := range state.StorageClusters {
		if c.Metadata.Name == reservedClusterRootName {
			errs = append(errs, fmt.Sprintf("StorageCluster/%s metadata.name %q is reserved: it collides with the `destroy --stage infra --clusters artifact-server` literal; rename the cluster", reservedClusterRootName, reservedClusterRootName))
		}
	}
	return errs
}

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
		for i, node := range ocp.Spec.Nodes {
			bind(node.MachineRef.Name, cluster, fmt.Sprintf("spec.nodes[%d]", i))
		}
	}
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		cluster := fmt.Sprintf("StorageCluster/%s", sc.Metadata.Name)
		for i, node := range sc.Spec.Ceph.Topology.Nodes {
			bind(node.MachineRef.Name, cluster, fmt.Sprintf("spec.ceph.topology.nodes[%d]", i))
		}
	}
	return errs
}

func validateMachineInstallProfileEntitlements(state v1alpha1.State) []string {
	var errs []string
	for _, profile := range state.MachineInstallProfiles {
		if profile.Spec.Installer.Anaconda == nil {
			continue
		}
		cdn := profile.Spec.Installer.Anaconda.PackageSource.GetFromSubscription()
		if cdn == nil {
			continue
		}
		ref := cdn.EntitlementRef.Name
		if ref == "" {
			continue
		}
		field := fmt.Sprintf("MachineInstallProfile/%s spec.installer.anaconda.packageSource.fromSubscription.entitlementRef %q", profile.Metadata.Name, ref)
		entitlement, ok := v1alpha1.EntitlementByName(state.Entitlements, ref)
		if !ok {
			errs = append(errs, field+" does not match any Entitlement")
			continue
		}
		if entitlement.Spec.Type != v1alpha1.EntitlementTypeRedHatRHEL {
			errs = append(errs, fmt.Sprintf("%s resolves to type %q, want %q", field, entitlement.Spec.Type, v1alpha1.EntitlementTypeRedHatRHEL))
			continue
		}
		if entitlement.Spec.RHSM != nil && v1alpha1.EntitlementRHSMManagement(entitlement.Spec.RHSM) == v1alpha1.EntitlementRHSMManagementExternal {
			errs = append(errs, field+" has rhsm.management external; the fromSubscription package source registers during Anaconda and cannot be delegated to a provisioning playbook — use a managed entitlement or a mirror/hostedTree package source")
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
		for _, node := range ocp.Spec.Nodes {
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
		if entry.Management == v1alpha1.EnvironmentComponentManaged && entry.Default {
			return entry
		}
	}
	if len(env.Spec.InfraComponents.Registries) == 1 && env.Spec.InfraComponents.Registries[0].Management == v1alpha1.EnvironmentComponentManaged {
		return &env.Spec.InfraComponents.Registries[0]
	}
	return nil
}

func validateArtifactServerRequirements(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	components := indexInfraComponents(state.InfraComponents)
	var errs []string
	for _, cluster := range state.StorageClusters {
		ci, ok := stateview.StorageClusterArtifactInstall(state, cluster)
		if !ok {
			continue
		}
		prefix := fmt.Sprintf("StorageCluster/%s", cluster.Metadata.Name)
		if env == nil || len(env.Spec.InfraComponents.ArtifactServers) == 0 {
			errs = append(errs, fmt.Sprintf("%s installs a bare-metal node's OS over the BMC, which needs generated artifact publication; set Environment.spec.infraComponents.artifactServers and MachineInstallProfile.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint", prefix))
			continue
		}
		errs = append(errs, validateStorageManagedOSArtifactServerEndpoints(state, ci, env, components)...)
	}
	return errs
}

func validateStorageManagedOSArtifactServerEndpoints(state v1alpha1.State, ci v1alpha1.ClusterInstall, env *v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	for _, m := range ci.Machines {
		machine, ok := stateview.Machine(state, m.Source.MachineRef.Name)
		if !ok || !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
		if !ok || profile.Spec.Installer.Anaconda == nil {
			continue
		}
		prefix := fmt.Sprintf("MachineInstallProfile/%s spec.installer.anaconda", profile.Metadata.Name)
		errs = append(errs, validateArtifactServerEndpointRef(
			prefix+".redfishVirtualMedia.artifactServerEndpoint",
			profile.Spec.Installer.Anaconda.RedfishVirtualMedia.ArtifactServerEndpoint,
			env,
			components,
			artifactServerEndpointValidation{Required: true, RequireManaged: true},
		)...)
	}
	return errs
}

func validateSecretReferences(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	if env == nil {
		return nil
	}
	declared := map[string]bool{}
	secretType := map[string]string{}
	for _, s := range state.Secrets {
		declared[s.Metadata.Name] = true
		secretType[s.Metadata.Name] = s.Spec.Type
	}
	var errs []string
	requireNoted := func(owner string, ref v1alpha1.SecretRef, note string) {
		if ref.Name == "" {
			return
		}
		if !dnsLabel.MatchString(ref.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q is not a DNS label", owner, ref.Name))
			return
		}
		if !declared[ref.Name] {
			msg := fmt.Sprintf("%s %q is not a declared Secret", owner, ref.Name)
			if note != "" {
				msg += " " + note
			}
			errs = append(errs, msg)
		}
	}
	require := func(owner string, ref v1alpha1.SecretRef) {
		requireNoted(owner, ref, "")
	}
	requireTypeNoted := func(owner string, ref v1alpha1.SecretRef, wantType, note string) {
		requireNoted(owner, ref, note)
		if ref.Name == "" || !declared[ref.Name] {
			return
		}
		if got := secretType[ref.Name]; got != wantType {
			errs = append(errs, fmt.Sprintf("%s %q is a %s Secret but a %s Secret is required", owner, ref.Name, got, wantType))
		}
	}
	requireTLS := func(owner string, ref v1alpha1.SecretRef) {
		requireTypeNoted(owner, ref, v1alpha1.SecretTypeTLSCertificate, "")
	}
	requireSSHKeyNoted := func(owner string, ref v1alpha1.SecretRef, note string) {
		requireTypeNoted(owner, ref, v1alpha1.SecretTypeSSHKeyPair, note)
	}
	requireSSHKey := func(owner string, ref v1alpha1.SecretRef) {
		requireSSHKeyNoted(owner, ref, "")
	}
	requireUsernamePassword := func(owner string, ref v1alpha1.SecretRef) {
		requireTypeNoted(owner, ref, v1alpha1.SecretTypeUsernamePassword, "")
	}
	for i, entry := range env.Spec.InfraComponents.Proxies {
		if entry.Connection != nil && entry.Connection.Auth != nil {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.proxies[%d].connection.auth.proxyAuthRef", env.Metadata.Name, i), entry.Connection.Auth.ProxyAuthRef)
		}
		if entry.Connection != nil && entry.Connection.TrustBundleRef.Name != "" {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.proxies[%d].connection.trustBundleRef", env.Metadata.Name, i), entry.Connection.TrustBundleRef)
		}
	}
	if env.Spec.InstallTrust != nil {
		for i, ref := range env.Spec.InstallTrust.CABundleRefs {
			require(fmt.Sprintf("Environment/%s spec.installTrust.caBundleRefs[%d]", env.Metadata.Name, i), ref)
		}
	}
	require(fmt.Sprintf("Environment/%s spec.defaults.install.pullSecretRef", env.Metadata.Name), env.Spec.Defaults.Install.PullSecretRef)
	requireNodeSSH(fmt.Sprintf("Environment/%s spec.defaults.install.nodeSSH", env.Metadata.Name), env.Spec.Defaults.Install.NodeSSH, "", requireSSHKeyNoted)
	if env.Spec.MachineAccess != nil {
		requireSSHKey(fmt.Sprintf("Environment/%s spec.machineAccess.keyRef", env.Metadata.Name), env.Spec.MachineAccess.KeyRef)
	}
	if registries := env.Spec.Registries; registries != nil && registries.Mirror != nil {
		owner := fmt.Sprintf("Environment/%s spec.registries.mirror", env.Metadata.Name)
		require(owner+".credentialsRef", registries.Mirror.CredentialsRef)
		require(owner+".trustBundleRef", registries.Mirror.TrustBundleRef)
	}
	for _, entitlement := range state.Entitlements {
		owner := fmt.Sprintf("Entitlement/%s spec", entitlement.Metadata.Name)
		if entitlement.Spec.RHSM != nil {
			require(owner+".rhsm.organizationRef", entitlement.Spec.RHSM.OrganizationRef)
			require(owner+".rhsm.activationKeyRef", entitlement.Spec.RHSM.ActivationKeyRef)
			if sat := entitlement.Spec.RHSM.Satellite; sat != nil {
				require(owner+".rhsm.satellite.trustBundleRef", sat.TrustBundleRef)
			}
		}
		if entitlement.Spec.Registry != nil {
			require(owner+".registry.credentialsRef", entitlement.Spec.Registry.CredentialsRef)
			require(owner+".registry.trustBundleRef", entitlement.Spec.Registry.TrustBundleRef)
		}
	}
	for _, machine := range state.Machines {
		if machine.Spec.Access.SSH != nil {
			if keyRef := v1alpha1.MachineSSHKeyRef(machine); keyRef.Name != "" {
				requireSSHKey(fmt.Sprintf("Machine/%s %s", machine.Metadata.Name, machineSSHKeyField(machine)), keyRef)
			}
			if passwordRef := v1alpha1.MachineSSHPasswordRef(machine); passwordRef.Name != "" {
				requireUsernamePassword(fmt.Sprintf("Machine/%s spec.access.ssh.auth.passwordRef", machine.Metadata.Name), passwordRef)
			}
			if machine.Spec.Access.SSH.SudoPasswordRef.Name != "" {
				requireUsernamePassword(fmt.Sprintf("Machine/%s spec.access.ssh.sudoPasswordRef", machine.Metadata.Name), machine.Spec.Access.SSH.SudoPasswordRef)
			}
			if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
				require(fmt.Sprintf("Machine/%s spec.access.ssh.knownHostsRef", machine.Metadata.Name), machine.Spec.Access.SSH.KnownHostsRef)
			}
		}
		if machine.Spec.Hardware.Management.BMC.CredentialsRef.Name != "" {
			require(fmt.Sprintf("Machine/%s spec.hardware.management.bmc.credentialsRef", machine.Metadata.Name), machine.Spec.Hardware.Management.BMC.CredentialsRef)
		}
	}
	for _, profile := range state.MachineInstallProfiles {
		if password := profile.Spec.Customizations.SSH.InitialPassword; password != nil {
			requireUsernamePassword(fmt.Sprintf("MachineInstallProfile/%s spec.customizations.ssh.initialPassword.secretRef", profile.Metadata.Name), password.SecretRef)
		}
		if encryption := profile.Spec.Customizations.Security.DiskEncryption; encryption != nil && encryption.RecoveryPassphraseRef.Name != "" {
			require(fmt.Sprintf("MachineInstallProfile/%s spec.customizations.security.diskEncryption.recoveryPassphraseRef", profile.Metadata.Name), encryption.RecoveryPassphraseRef)
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
	for _, p := range state.InfraProviders {
		if p.Spec.Libvirt != nil && p.Spec.Libvirt.BMCEmulationDefaults != nil && p.Spec.Libvirt.BMCEmulationDefaults.Auth != nil {
			require(fmt.Sprintf("InfraProvider/%s spec.libvirt.bmcEmulationDefaults.auth.credentialsRef",
				p.Metadata.Name), p.Spec.Libvirt.BMCEmulationDefaults.Auth.CredentialsRef)
		}
		if p.Spec.BareMetal != nil && p.Spec.BareMetal.Defaults.BMC != nil && p.Spec.BareMetal.Defaults.BMC.CredentialsRef.Name != "" {
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
		pullSecretNote := ""
		if ocp.DefaultedRefs.PullSecretRef {
			pullSecretNote = "(defaulted; declare the secret or set spec.install.pullSecretRef)"
		}
		nodeSSHNote := ""
		if ocp.DefaultedRefs.NodeSSH {
			nodeSSHNote = "(defaulted; declare the secret or set spec.install.nodeSSH)"
		}
		requireNoted(fmt.Sprintf("ContainerCluster/%s install.pullSecretRef", ocp.Metadata.Name), ocp.Spec.Install.PullSecretRef, pullSecretNote)
		requireNodeSSH(fmt.Sprintf("ContainerCluster/%s install.nodeSSH", ocp.Metadata.Name), ocp.Spec.Install.NodeSSH, nodeSSHNote, requireSSHKeyNoted)
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
	for _, export := range state.StorageExports {
		if details := export.Spec.ExternalDetails; details != nil {
			require(fmt.Sprintf("StorageExport/%s spec.externalDetails.fromSecretRef", export.Metadata.Name), details.FromSecretRef)
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
			if acceptedInput.SecretRef == nil {
				continue
			}
			if ref := addoninputs.SecretRefValue(input); ref.Name != "" {
				require(fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s].value", effective.Binding.Metadata.Name, effective.Addon.AddonRef.Name, input.Name), ref)
			}
		}
	}
	for _, playbook := range state.CustomPlaybooks {
		for i, ref := range playbook.Spec.SecretRefs {
			require(fmt.Sprintf("CustomPlaybook/%s spec.secretRefs[%d]", playbook.Metadata.Name, i), ref)
		}
	}
	for _, addon := range state.ClusterAddons {
		for i, hook := range addon.Spec.Steps {
			for j, ref := range hook.SecretRefs {
				require(fmt.Sprintf("ClusterAddon/%s spec.steps[%d].secretRefs[%d]", addon.Metadata.Name, i, j), ref)
			}
		}
	}
	return errs
}

func requireNodeSSH(owner string, spec v1alpha1.NodeSSHSpec, note string, requireSSHKey func(string, v1alpha1.SecretRef, string)) {
	if spec.KeyPairRef.Name != "" {
		requireSSHKey(owner+".keyPairRef", spec.KeyPairRef, note)
	}
	if spec.PublicKeyRef.Name != "" {
		requireSSHKey(owner+".publicKeyRef", spec.PublicKeyRef, note)
	}
	if spec.PrivateKeyRef.Name != "" {
		requireSSHKey(owner+".privateKeyRef", spec.PrivateKeyRef, note)
	}
}
