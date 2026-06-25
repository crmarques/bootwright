package inventory

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/nmstate"
	"github.com/crmarques/bootwright/internal/render/installer"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func managedMachineOSInstallGroupsVars(state v1alpha1.State, paths PathOptions) []any {
	var out []any
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		var components []any
		for _, m := range ci.Machines {
			machine, ok := stateview.Machine(state, m.Name)
			if !ok || !v1alpha1.MachineInstallsOS(machine) {
				continue
			}
			component := machineComponentVars(state, ci, m, cluster.Metadata.Name, paths.SecretsDir)
			// The managed-OS play selects this component by matching its
			// machineRef against the inventory host's provider_host_name. Pin
			// both to the same driver host so bare-metal nodes (which carry no
			// substrate provider host) resolve to the controller (localhost)
			// instead of leaving machineRef undefined and failing the match.
			component["machineRef"] = managedOSTaskHost(state, m)
			if osInstall := machineOSInstallVars(state, ci, m, machine, cluster.Metadata.Name, paths); len(osInstall) > 0 {
				component["osInstall"] = osInstall
				boot := machineBootVarsWithISO(state, ci, m, cluster.Metadata.Name, fmt.Sprintf("os-%s-%s.iso", cluster.Metadata.Name, m.Name))
				if boot != nil {
					boot["readiness"] = map[string]any{
						"type": "none",
					}
					component["boot"] = boot
				}
				components = append(components, component)
			}
		}
		if len(components) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"name":               cluster.Metadata.Name,
			"storageClusterName": cluster.Metadata.Name,
			"networks":           clusterNetworksVars(state, ci),
			"components":         components,
		})
	}
	return out
}

func storageClusterInstall(state v1alpha1.State, cluster v1alpha1.StorageCluster) (v1alpha1.ClusterInstall, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.ClusterInstall{}, false
	}
	seen := map[string]bool{}
	var machines []v1alpha1.InstallMachine
	var bindings []v1alpha1.MachineNetworkBinding
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
			continue
		}
		machine, ok := stateview.Machine(state, node.MachineRef.Name)
		if !ok {
			continue
		}
		seen[node.MachineRef.Name] = true
		machines = append(machines, stateview.InstallMachineFromMachine(machine))
		network := machine.Spec.Network.Config
		if network.NetworkConfigRef.Name != "" && network.AttachmentRef.Name != "" && machine.Spec.Substrate.ProviderRef.Name != "" {
			bindings = append(bindings, v1alpha1.MachineNetworkBinding{
				NetworkConfigRef: network.NetworkConfigRef,
				ProviderRef:      machine.Spec.Substrate.ProviderRef,
				AttachmentRef:    network.AttachmentRef,
			})
		}
	}
	sort.SliceStable(machines, func(i, j int) bool { return machines[i].Name < machines[j].Name })
	sort.SliceStable(bindings, func(i, j int) bool {
		if bindings[i].ProviderRef.Name != bindings[j].ProviderRef.Name {
			return bindings[i].ProviderRef.Name < bindings[j].ProviderRef.Name
		}
		return bindings[i].NetworkConfigRef.Name < bindings[j].NetworkConfigRef.Name
	})
	return v1alpha1.ClusterInstall{
		Metadata:        v1alpha1.Metadata{Name: cluster.Metadata.Name},
		NetworkBindings: bindings,
		Machines:        machines,
	}, len(machines) > 0
}

func machineOSInstallVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, machine v1alpha1.Machine, clusterName string, paths PathOptions) map[string]any {
	if machine.Spec.Access.SSH == nil {
		return nil
	}
	profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
	if !ok || profile.Spec.Installer.Anaconda == nil {
		return nil
	}
	image, ok := stateview.MachineImage(state, profile.Spec.Installer.Anaconda.ImageRef.Name)
	if !ok {
		return nil
	}
	resolved, err := media.Resolve(image.Spec.URL)
	if err != nil {
		return nil
	}
	env := stateview.Environment(state)
	sourceURL, imageRepositories, rhsm := machineImageInstallSourceVars(image.Spec.InstallSource, env, paths.SecretsDir)
	profileRepositories := machineInstallRepositoryVars(profile.Spec.Installer.Anaconda.Repositories)
	installer := map[string]any{
		"type":         profile.Spec.Installer.Type,
		"repositories": append(imageRepositories, profileRepositories...),
	}
	if profile.Spec.Customizations.Security.FIPS.Enabled {
		installer["kernelArgs"] = []string{"fips=1"}
	}
	if sourceURL != "" {
		installer["sourceURL"] = sourceURL
	}
	if len(rhsm) > 0 {
		installer["rhsm"] = rhsm
	}
	out := map[string]any{
		"profileName": profile.Metadata.Name,
		"os": map[string]any{
			"family":       profile.Spec.OS.Family,
			"version":      profile.Spec.OS.Version,
			"architecture": profile.Spec.OS.Architecture,
		},
		"installer": installer,
		"image":     machineOSInstallImageVars(resolved, image.Spec.MediaType, image.Spec.Checksum, machineOSInstallImageSourceOnTarget(state, m)),
		"kickstart": map[string]any{
			"hostname":               machineInstallHostname(state, machine),
			"sshUser":                machine.Spec.Access.SSH.User,
			"sshPublicKeyPath":       secret.ResolveSSHPublicKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir),
			"passwordAuthentication": profile.Spec.Customizations.SSH.PasswordAuthentication,
			"authorizeMachineSSHKey": profile.Spec.Customizations.SSH.AuthorizeMachineSSHKey,
			"packages":               machineInstallPackagesVars(profile.Spec.Customizations.Packages),
			"services":               machineInstallServicesVars(profile.Spec.Customizations.Services),
			"security":               machineInstallSecurityVars(profile.Spec.Customizations.Security),
			"storage":                machineInstallStorageVars(profile, state, m),
			"network":                machineInstallNetworkVars(state, ci, m, clusterName),
		},
	}
	if machine.Spec.Access.SSH != nil {
		out["ssh"] = map[string]any{
			"address":        v1alpha1.MachineSSHAddress(machine),
			"user":           machine.Spec.Access.SSH.User,
			"privateKeyPath": secret.ResolveSSHPrivateKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir),
			"knownHostsPath": machineKnownHostsPath(machine, env, paths),
			"trustDir":       sshtrust.DirForSecrets(paths.trustSecretsDir()),
		}
	}
	out["marker"] = machineOSInstallMarkerVars(out, clusterName, machine.Metadata.Name, profile.Metadata.Name)
	return out
}

// machineOSInstallImageVars renders the normalize-materialized mediaType
// as-is; the boot.iso filename derivation lives in the normalize phase.
func machineOSInstallImageVars(resolved media.Resolved, mediaType, checksum string, sourceOnTarget bool) map[string]any {
	normalizedChecksum, _ := media.NormalizeSHA256(checksum)
	out := map[string]any{
		"kind":      resolved.Kind,
		"mediaType": mediaType,
		"original":  resolved.Original,
		"checksum":  normalizedChecksum,
	}
	if sourceOnTarget && (resolved.Key != "" || resolved.Kind == "file") {
		out["sourceOnTarget"] = true
	}
	if resolved.Key != "" {
		out["key"] = resolved.Key
	}
	if resolved.Path != "" {
		out["path"] = resolved.Path
	}
	if resolved.URL != "" {
		out["url"] = resolved.URL
	}
	return out
}

func machineOSInstallImageSourceOnTarget(state v1alpha1.State, m v1alpha1.InstallMachine) bool {
	machineRef := machineHostRef(state, m)
	if machineRef == "" {
		return false
	}
	machine, ok := stateview.Machine(state, machineRef)
	if !ok {
		// API-native substrates run machine tasks on the controller, so a
		// file-sourced install image is already on the task host.
		return machineRef == "localhost"
	}
	return locality.IsControllerLocalMachine(machine, locality.DefaultPolicy)
}

// machineImageInstallSourceVars renders the install source exactly as
// normalize materialized it: the primary install tree is source.url
// (normalize already promoted repositories[0].baseURL into it when the url
// was omitted) and every remaining repository is an additional repo entry.
func machineImageInstallSourceVars(source v1alpha1.MachineImageInstallSource, env *v1alpha1.Environment, secretsDir string) (string, []any, map[string]any) {
	rhsm := map[string]any{}
	if source.EntitlementRef.Name != "" {
		resolved, ok := entitlements.Resolve(env, source.EntitlementRef.Name, "", secretsDir)
		if !ok {
			return source.URL, machineInstallRepositoryVars(source.Repositories), rhsm
		}
		rhsm["enabled"] = true
		rhsm["organizationPath"] = resolved.RHSM.OrganizationPath
		rhsm["activationKeyPath"] = resolved.RHSM.ActivationKeyPath
		rhsm["connectToInsights"] = resolved.RHSM.ConnectToInsights
	}
	return source.URL, machineInstallRepositoryVars(source.Repositories), rhsm
}

func machineInstallRepositoryVars(repos []v1alpha1.MachineInstallRepository) []any {
	out := make([]any, 0, len(repos))
	for _, repo := range repos {
		out = append(out, map[string]any{
			"id":      repo.ID,
			"baseURL": repo.BaseURL,
		})
	}
	return out
}

// machineInstallHostname is the OS hostname written at install. For a cluster
// node it mirrors the hostname its cluster topology registers (normalize
// already resolved that to the FQDN, an explicit pin, or the bare name), so the
// installed hostname can never drift from the name cephadm/the installer
// expects. A machine no cluster node-binds keeps its bare name.
func machineInstallHostname(state v1alpha1.State, machine v1alpha1.Machine) string {
	if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok {
		return hostname
	}
	return machine.Metadata.Name
}

func machineInstallStorageVars(profile v1alpha1.MachineInstallProfile, state v1alpha1.State, m v1alpha1.InstallMachine) map[string]any {
	storage := profile.Spec.Customizations.Storage
	rootDevice := ""
	if storage.RootDevice.Source == v1alpha1.MachineInstallRootDeviceMachine || storage.RootDevice.Source == "" {
		if hints := installer.MachineRootDeviceHints(state, m); hints != nil {
			rootDevice = hints.DeviceName
		}
	}
	out := map[string]any{
		"wipe":       storage.Wipe,
		"rootDevice": rootDevice,
		"rootDisk":   strings.TrimPrefix(rootDevice, "/dev/"),
	}
	return out
}

func machineInstallNetworkVars(state v1alpha1.State, ci v1alpha1.ClusterInstall, m v1alpha1.InstallMachine, clusterName string) map[string]any {
	config := installer.AgentNetworkConfig(state, ci, m, clusterName)
	network := map[string]any{
		"bootproto": "dhcp",
		"device":    "link",
	}
	if len(config) == 0 {
		return network
	}
	if iface := kickstartPrimaryInterface(config); len(iface) > 0 {
		for k, v := range iface {
			network[k] = v
		}
	}
	if gateway := networkConfigGatewayFromMap(config); gateway != "" {
		network["gateway"] = gateway
	}
	if dns := nmstate.NetworkConfigDNSServers(config); len(dns) > 0 {
		network["dnsServers"] = dns
	}
	return network
}

func kickstartPrimaryInterface(config map[string]any) map[string]any {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		mac, _ := entry["mac-address"].(string)
		ip, prefix := networkConfigFamilyIPPrefix(entry, "ipv4")
		if ip == "" {
			continue
		}
		out := map[string]any{
			"bootproto": "static",
			"ip":        ip,
			"prefix":    prefix,
			"netmask":   prefixNetmask(prefix),
		}
		if mac != "" {
			out["device"] = mac
		} else if name != "" {
			out["device"] = name
		}
		return out
	}
	return nil
}

func networkConfigFamilyIPPrefix(entry map[string]any, family string) (string, int) {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return "", 0
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return "", 0
	}
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := address["ip"].(string)
		if ip == "" {
			continue
		}
		return ip, intFromYAML(address["prefix-length"])
	}
	return "", 0
}

func networkConfigGatewayFromMap(config map[string]any) string {
	routes, ok := config["routes"].(map[string]any)
	if !ok {
		return ""
	}
	rawConfig, ok := routes["config"].([]any)
	if !ok {
		return ""
	}
	for _, item := range rawConfig {
		route, ok := item.(map[string]any)
		if !ok {
			continue
		}
		destination, _ := route["destination"].(string)
		if destination != "0.0.0.0/0" && destination != "::/0" {
			continue
		}
		nextHop, _ := route["next-hop-address"].(string)
		if nextHop != "" {
			return nextHop
		}
	}
	return ""
}

func intFromYAML(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func prefixNetmask(prefix int) string {
	if prefix <= 0 || prefix > 32 {
		return ""
	}
	return net.IP(net.CIDRMask(prefix, 32)).String()
}
