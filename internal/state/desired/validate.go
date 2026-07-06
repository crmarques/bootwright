package desiredstate

import (
	"fmt"
	"regexp"
	"sort"
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
	findings := validateFindings(state)
	if len(findings) == 0 {
		return nil
	}
	return newValidationError(findings)
}

// validateFindings runs every per-kind validator and returns the raw findings.
// Validate wraps them in a ValidationError. Keep the validator call order here as
// the single source of truth.
func validateFindings(state v1alpha1.State) []Finding {
	var errs []Finding
	errs = append(errs, notes(validateEnvironments(state))...)
	errs = append(errs, notes(validateMachines(state))...)
	errs = append(errs, validateNetworkConfigs(state)...)
	errs = append(errs, notes(validateProviders(state))...)
	errs = append(errs, notes(validateInfraComponents(state))...)
	errs = append(errs, notes(validateContainerClusters(state))...)
	errs = append(errs, notes(validateClusterAddons(state))...)
	errs = append(errs, notes(validateClusterAddonProfiles(state))...)
	errs = append(errs, notes(validateClusterAddonBindings(state))...)
	errs = append(errs, notes(validateProvisioningPlaybooks(state))...)
	errs = append(errs, notes(validateStorage(state))...)
	errs = append(errs, notes(validateCrossLayer(state))...)
	errs = append(errs, notes(validateSecretReferences(state))...)
	errs = append(errs, notes(validateUniqueBMCAddresses(state))...)
	errs = append(errs, notes(validateUniqueMachineSSHAddresses(state))...)
	errs = append(errs, notes(validateManagedOSCephNodeRootDisk(state))...)
	errs = append(errs, notes(validateOSDDevicesExcludeRootDisk(state))...)
	errs = append(errs, duplicateNameFindings(state)...)
	return errs
}

// validateUniqueMachineSSHAddresses fails closed when two different Machines resolve
// to the same SSH address. bootwright reaches a node over that address to probe
// ownership, apply day-2 network state, and verify readiness, so a shared address
// means two logical Machines drive — and could reinstall or reconfigure — a single
// physical host, and the pre-install ownership probe of one would see the other's OS.
// A Machine with no SSH address (a VM that pre-provisions its OS) is skipped.
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

// validateUniqueBMCAddresses fails closed when two different Machines declare the
// same BMC (Redfish) endpoint. A BMC address identifies one physical host's
// management controller, so a shared address is a copy-paste error that points two
// logical Machines at a single physical host — and bootwright's boot/install path
// would drive the SAME host for both, disk-wiping the wrong machine. Catching it at
// validation, before any apply, is the cheapest guard against that fat-finger; VM
// substrates (KubeVirt/vSphere) declare no BMC address and are unaffected.
func validateUniqueBMCAddresses(state v1alpha1.State) []string {
	// Key on the normalized endpoint, not the raw string: the boot renderer
	// collapses equivalent Redfish spellings (redfish+https / redfish-virtualmedia
	// / redfish schemes, a trailing /redfish/v1/Systems/<id> suffix, trailing
	// slashes) to one baseUrl, so two Machines that spell the SAME endpoint
	// differently drive one physical host at apply — the exact wrong-host disk-wipe
	// this guard exists to prevent. Comparing raw strings would let those variants
	// bypass it.
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

// normalizeBMCAddressKey mirrors the boot renderer's normalizeRedfishURL /
// normalizeRedfishTransport (internal/render/inventory/vars_boot.go) so the
// duplicate-BMC guard compares endpoints exactly as apply drives them. Kept in
// sync by hand: the renderer package is not importable here. It folds the
// transport scheme, a /redfish/v1/Systems/<id> suffix, and trailing slashes into
// a single comparison key; it deliberately does NOT normalize ports, matching the
// renderer.
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

// duplicateNameFindings reports every metadata.name that appears more than once
// within a kind, as a structured finding naming the owning object. It replaces
// the per-validator inline dedup loops so the duplicate diagnostics are routed
// at the source (with a real Object) instead of the CLI reconstructing them
// from the message text. NetworkConfig keeps its own structured check; an
// Environment count is reported by validateEnvironments.
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

// duplicateFindings reports each name that appears more than once. The dedup
// mirrors the per-validator checks it replaces: only names that pass
// validateName participate (an invalid name is reported as a name error, not a
// duplicate), and the second and later occurrences are flagged.
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
	errs = append(errs, validateClusterRootNameCollisions(state)...)
	errs = append(errs, validateReservedClusterRootNames(state)...)
	errs = append(errs, validateDisconnectedRequiresRegistry(state)...)
	errs = append(errs, validateArtifactServerRequirements(state)...)
	errs = append(errs, validateSharedMachineServices(state)...)
	errs = append(errs, validateMachineNodeBindings(state)...)
	errs = append(errs, validateKubeVirtHostClusterDependencies(state)...)
	errs = append(errs, validateMachineImageEntitlements(state)...)
	return errs
}

// validateClusterRootNameCollisions enforces one cluster-root name namespace
// across ContainerCluster and StorageCluster. Cluster selection resolves bare
// names against both kinds (`--clusters`, Environment.spec.containerClusters /
// spec.storageClusters), so a name declared by both would silently widen
// apply, state-check, and destroy scope to the second cluster. Same-kind
// duplicates stay with the per-kind `duplicate <Kind>` rules.
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

// reservedClusterRootName is the literal `destroy --stage infra --clusters`
// accepts to remove only the generated artifact publication service. A cluster
// root of this name would make that destructive selection ambiguous, so it is
// reserved out of the cluster-name namespace.
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
		if source.Type != v1alpha1.MachineImageInstallSourceTypeRHSM {
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
		if clusterInstallUsesHostedTree(state, ci) {
			if _, _, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerMachineBoot); !ok {
				errs = append(errs, fmt.Sprintf("%s installs a node from a hostedTree MachineImage, so the installer fetches packages from the artifact server; set spec.install.artifactAccess.machineBoot.endpointRef to a resolvable endpoint (serve it over http so the installer does not reject a self-signed certificate)",
					prefix))
			}
		}
	}
	// A StorageCluster cannot author spec.install.artifactAccess, so a bare-metal
	// node whose managed OS installs over the BMC draws its Redfish virtual-media
	// publication target from the Environment artifact-access defaults. Require
	// them to resolve here; otherwise the install ISO stage path renders empty and
	// the managed-OS role fails deep in apply with an opaque empty-path error.
	for _, cluster := range state.StorageClusters {
		ci, ok := stateview.StorageClusterArtifactInstall(state, cluster)
		if !ok {
			continue
		}
		prefix := fmt.Sprintf("StorageCluster/%s", cluster.Metadata.Name)
		if env == nil || len(env.Spec.InfraComponents.ArtifactServers) == 0 {
			errs = append(errs, fmt.Sprintf("%s installs a bare-metal node's OS over the BMC, which needs generated artifact publication; set Environment.spec.infraComponents.artifactServers and spec.defaults.artifactAccess.redfishVirtualMedia.endpointRef", prefix))
			continue
		}
		if ci.ArtifactAccess.ServerRef.Name == "" || ci.ArtifactAccess.RedfishVirtualMedia.EndpointRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s installs a bare-metal node's OS over the BMC; set Environment.spec.defaults.artifactAccess.serverRef and .redfishVirtualMedia.endpointRef so the install ISO can publish for Redfish virtual media", prefix))
			continue
		}
		if _, _, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerRedfishVirtualMedia); !ok {
			errs = append(errs, fmt.Sprintf("%s bare-metal managed-OS install requires Environment.spec.defaults.artifactAccess.redfishVirtualMedia.endpointRef to resolve on artifact server %q", prefix, ci.ArtifactAccess.ServerRef.Name))
		}
		if clusterInstallUsesHostedTree(state, ci) {
			if _, _, ok := artifacts.ResolveConsumerEndpoint(state, ci, v1alpha1.ArtifactConsumerMachineBoot); !ok {
				errs = append(errs, fmt.Sprintf("%s installs a node from a hostedTree MachineImage; set Environment.spec.defaults.artifactAccess.machineBoot.endpointRef so the installer can fetch packages from artifact server %q (serve it over http)", prefix, ci.ArtifactAccess.ServerRef.Name))
			}
		}
	}
	return errs
}

// clusterInstallUsesHostedTree reports whether any machine in ci installs from a
// MachineImage whose installSource.type is hostedTree, so validation can require
// the node-reachable machineBoot artifact endpoint the installer fetches the
// tree from.
func clusterInstallUsesHostedTree(state v1alpha1.State, ci v1alpha1.ClusterInstall) bool {
	for _, m := range ci.Machines {
		// The OS install profile is machine.spec.os.installProfileRef, not the
		// substrate machineProfile that m.Source.ProfileRef carries; resolve the
		// Machine and read the OS install ref the renderer uses
		// (vars_machine_os_install.go). Keying off the substrate profile always
		// missed (bare-metal machines author no substrate profile), so this guard
		// never fired.
		machine, ok := stateview.Machine(state, m.Source.MachineRef.Name)
		if !ok {
			continue
		}
		profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
		if !ok || profile.Spec.Installer.Anaconda == nil {
			continue
		}
		image, ok := stateview.MachineImage(state, profile.Spec.Installer.Anaconda.ImageRef.Name)
		if !ok {
			continue
		}
		if image.Spec.InstallSource.Type == v1alpha1.MachineImageInstallSourceTypeHostedTree {
			return true
		}
	}
	return false
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
	// requireNoted carries an optional note appended to the dangling-secret
	// diagnostic; normalize-injected refs use it to say the value was
	// defaulted and how to override, since it appears nowhere in the
	// author's files.
	requireNoted := func(owner string, ref v1alpha1.SecretRef, note string) {
		if ref.Name == "" {
			return
		}
		if !dnsLabel.MatchString(ref.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q is not a DNS label", owner, ref.Name))
			return
		}
		if !declared[ref.Name] {
			msg := fmt.Sprintf("%s %q is not declared in Environment/%s spec.secrets", owner, ref.Name, env.Metadata.Name)
			if note != "" {
				msg += " " + note
			}
			errs = append(errs, msg)
		}
	}
	require := func(owner string, ref v1alpha1.SecretRef) {
		requireNoted(owner, ref, "")
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
	requireSSHKeyNoted := func(owner string, ref v1alpha1.SecretRef, note string) {
		requireNoted(owner, ref, note)
		if ref.Name == "" || !declared[ref.Name] {
			return
		}
		spec := env.Spec.Secrets[ref.Name]
		if spec.Generated != nil && spec.Generated.SSHKeyPair == nil {
			errs = append(errs, fmt.Sprintf("%s %q uses generated material but Environment/%s spec.secrets[%s].generated is not sshKeyPair", owner, ref.Name, env.Metadata.Name, ref.Name))
		}
	}
	requireSSHKey := func(owner string, ref v1alpha1.SecretRef) {
		requireSSHKeyNoted(owner, ref, "")
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
	for _, p := range state.InfraProviders {
		if p.Spec.Libvirt != nil && p.Spec.Libvirt.BMCEmulationDefaults != nil && p.Spec.Libvirt.BMCEmulationDefaults.Auth != nil {
			require(fmt.Sprintf("InfraProvider/%s spec.libvirt.bmcEmulationDefaults.auth.credentialsRef",
				p.Metadata.Name), p.Spec.Libvirt.BMCEmulationDefaults.Auth.CredentialsRef)
		}
		// credentialsRef on the provider BMC default is optional (a cert-only default
		// is valid); validate it only when set. Per-machine credentialsRef is
		// independently required in validate_machine.go.
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
		// Normalize-injected install refs (Environment defaults or invented
		// convention names) appear nowhere in the cluster author's file;
		// when one dangles, say it was defaulted and how to override.
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
			for name, property := range acceptedInput.Schema.Properties {
				if !property.Secret {
					continue
				}
				if ref := addoninputs.SecretRefValue(input.Values, name); ref.Name != "" {
					require(fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s].values.%s", effective.Binding.Metadata.Name, effective.Addon.AddonRef.Name, input.Name, name), ref)
				}
			}
		}
	}
	for _, playbook := range state.ProvisioningPlaybooks {
		for i, ref := range playbook.Spec.SecretRefs {
			require(fmt.Sprintf("ProvisioningPlaybook/%s spec.secretRefs[%d]", playbook.Metadata.Name, i), ref)
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
