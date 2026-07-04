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
	findings := validateFindings(state)
	if len(findings) == 0 {
		return nil
	}
	return newValidationError(findings)
}

// validateFindings runs every per-kind validator and returns the raw findings.
// Validate wraps them in a ValidationError; ValidateScoped diffs two findings
// sets. Keep the validator call order here as the single source of truth.
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
	errs = append(errs, notes(validateStorage(state))...)
	errs = append(errs, notes(validateCrossLayer(state))...)
	errs = append(errs, notes(validateSecretReferences(state))...)
	errs = append(errs, duplicateNameFindings(state)...)
	return errs
}

// ValidateScoped validates a scoped subset of the effective desired state while
// suppressing findings that are artifacts of scoping rather than genuine
// desired-state errors. `--scoped-validation` narrows validation to the objects
// a --clusters/--stage run acts on so an error in an out-of-scope object does
// not block the run; but the scoped state is deliberately incomplete, so
// dropping out-of-scope objects can orphan a reference an in-scope or
// render-reference object still carries:
//
//   - an InfraComponent (artifact server, proxy, ...) survives a --clusters
//     scope in full, but its host Machine is pulled in only when an in-scope
//     cluster consumes the service; scoping a storage cluster that does not
//     consume it leaves the component referencing a Machine no longer present.
//   - a storage-cluster apply keeps the data-foundation ClusterAddonBindings as
//     render references (they drive the per-consumer Ceph client auth) but drops
//     the consuming ContainerClusters from the work set, so each binding's
//     clusterRef names a ContainerCluster no longer present.
//
// Such a finding appears when validating `scoped` but not when validating the
// self-consistent `full` state, so it is a false positive for the scoped run and
// is dropped. A genuine error in an in-scope object appears in both and is
// reported. An error in an object scoping removed entirely never appears in
// `scoped`, so the existing out-of-scope-error tolerance is preserved. The
// reported set is always a subset of the scoped findings: ValidateScoped only
// ever suppresses, never adds.
//
// full must be the complete effective desired state `scoped` was derived from.
func ValidateScoped(scoped, full v1alpha1.State) error {
	scopedFindings := validateFindings(scoped)
	if len(scopedFindings) == 0 {
		return nil
	}
	genuine := make(map[string]bool)
	for _, finding := range validateFindings(full) {
		genuine[finding.Message] = true
	}
	var kept []Finding
	for _, finding := range scopedFindings {
		if genuine[finding.Message] {
			kept = append(kept, finding)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return newValidationError(kept)
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
