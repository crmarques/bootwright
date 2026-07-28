package inventory

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	"github.com/crmarques/bootwright/internal/render/installer"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

var managedOSSourceIDUnsafeRE = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func machineInstallInitialPasswordPath(profile v1alpha1.MachineInstallProfile, paths PathOptions) string {
	initial := profile.Spec.Customizations.SSH.InitialPassword
	if initial == nil || initial.SecretRef.Name == "" {
		return ""
	}
	return secret.ResolvePath(initial.SecretRef.Name, paths.SecretIndex, paths.SecretsDir)
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
	resolved, err := media.Resolve(image.Spec.BootMedia)
	if err != nil {
		return nil
	}
	env := stateview.Environment(state)
	idx := secret.NewIndex(state)
	var eff *proxy.Effective
	if env != nil {
		eff = proxy.ResolveFor(state, env, env.Spec.ProxyNameFor(v1alpha1.ProxyConsumerMachineOSInstall))
	}
	packageSource := profile.Spec.Installer.Anaconda.PackageSource
	sourceURL, imageRepositories, rhsm := machineInstallPackageSourceVars(packageSource, state.Entitlements, idx, paths.SecretsDir, eff)
	var hostedTree map[string]any
	if packageSource.GetHostedTree() != nil {
		treeURL, tree, _ := machineOSHostedTreeVars(state, packageSource)
		sourceURL, imageRepositories, hostedTree = treeURL, nil, tree
	}
	installer := map[string]any{
		"repositories": imageRepositories,
	}
	if profile.Spec.Customizations.Security.FIPS.Enabled {
		installer["kernelArgs"] = []string{"fips=1"}
	}
	if sourceURL != "" {
		installer["sourceURL"] = sourceURL
		if installTargetProxied(eff, sourceURL) {
			installer["sourceProxied"] = true
		}
	}
	if len(rhsm) > 0 {
		if installTargetProxied(eff, rhsmRegistrationHost(rhsm)) {
			rhsm["proxied"] = true
		}
		installer["rhsm"] = rhsm
	}
	if proxyVars := managedOSInstallProxyVars(eff, state, paths.SecretsDir); len(proxyVars) > 0 {
		installer["proxy"] = proxyVars
	}
	sshUser := managedOSSSHUser(machine)
	imageVars := machineOSInstallImageVars(resolved, machineOSMediaType(packageSource), image.Spec.Checksum, machineOSInstallImageSourceOnTarget(state, m), clusterName)
	if hostedTree != nil {
		imageVars["installTree"] = hostedTree
	}
	out := map[string]any{
		"profileName": profile.Metadata.Name,
		"os": map[string]any{
			"family":       profile.Spec.OS.Family,
			"version":      profile.Spec.OS.Version,
			"architecture": profile.Spec.OS.Architecture,
		},
		"installer": installer,
		"image":     imageVars,
		"kickstart": map[string]any{
			"hostname":               machineInstallHostname(state, machine),
			"sshUser":                sshUser,
			"sshPublicKeyPath":       secret.ResolveSSHPublicKeyPath(v1alpha1.MachineSSHKeyRef(machine).Name, paths.SecretIndex, paths.SecretsDir),
			"passwordAuthentication": profile.Spec.Customizations.SSH.PasswordAuthentication,
			"sudo":                   v1alpha1.MachineInstallSudoPolicy(profile),
			"localization":           machineInstallLocalizationVars(profile.Spec.Customizations.Localization),
			"packages":               machineInstallPackagesVars(profile.Spec.Customizations.Packages),
			"services":               machineInstallServicesVars(profile.Spec.Customizations.Services),
			"security":               machineInstallSecurityVars(profile.Spec.Customizations.Security),
			"storage":                machineInstallStorageVars(profile, state, m),
			"network":                machineInstallNetworkVars(state, ci, m, clusterName),
		},
	}
	if path := machineInstallInitialPasswordPath(profile, paths); path != "" {
		out["kickstart"].(map[string]any)["initialPasswordPath"] = path
	}
	if machine.Spec.Access.SSH != nil {
		ssh := map[string]any{
			"address":           v1alpha1.MachineSSHAddress(machine),
			"connectionAddress": stateview.MachineConnectionAddress(state, machine),
			"user":              sshUser,
			"privateKeyPath":    secret.ResolveSSHPrivateKeyPath(v1alpha1.MachineSSHKeyRef(machine).Name, paths.SecretIndex, paths.SecretsDir),
		}
		if knownHosts := machineKnownHostsPath(machine, paths); knownHosts != "" {
			ssh["knownHostsPath"] = knownHosts
		}
		if trustDir := sshtrust.DirForSecrets(paths.trustSecretsDir()); trustDir != "" {
			ssh["trustDir"] = trustDir
		}
		if fallback := managedOSFallbackSSHUser(state, machine, clusterName); fallback != "" && fallback != sshUser {
			ssh["fallbackUser"] = fallback
		}
		out["ssh"] = ssh
	}
	desiredNetwork := machineInstallDesiredNetwork(state, ci, m, clusterName)
	if len(desiredNetwork) > 0 {
		ensureKickstartPackage(out, "nmstate")
	}
	out["marker"] = machineOSInstallMarkerVars(out, clusterName, machine.Metadata.Name, profile.Metadata.Name)
	if len(desiredNetwork) > 0 {
		out["network"] = map[string]any{"desiredState": desiredNetwork}
	}
	return out
}

func ensureKickstartPackage(osInstall map[string]any, pkg string) {
	kickstart, ok := osInstall["kickstart"].(map[string]any)
	if !ok {
		return
	}
	packages, ok := kickstart["packages"].(map[string]any)
	if !ok {
		return
	}
	install, _ := packages["install"].([]string)
	for _, existing := range install {
		if existing == pkg {
			return
		}
	}
	packages["install"] = append(install, pkg)
}

func managedOSSSHUser(machine v1alpha1.Machine) string {
	if machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User
	}
	return "root"
}

func managedOSFallbackSSHUser(state v1alpha1.State, machine v1alpha1.Machine, clusterName string) string {
	if !v1alpha1.MachineRevokesRootLogin(machine) {
		return ""
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name != clusterName || !v1alpha1.StorageClusterManaged(cluster) {
			continue
		}
		if user := v1alpha1.StorageClusterCephadmSSHUser(cluster); user != v1alpha1.RootSSHUser {
			return user
		}
	}
	return ""
}

func machineOSInstallImageVars(resolved media.Resolved, mediaType, checksum string, sourceOnTarget bool, clusterName string) map[string]any {
	normalizedChecksum, _ := media.NormalizeSHA256(checksum)
	useSourceOnTarget := sourceOnTarget && (resolved.Key != "" || resolved.Kind == "file")
	sourceID := managedOSSourceID(normalizedChecksum, resolved.Key, resolved.Original)
	out := map[string]any{
		"kind":      resolved.Kind,
		"mediaType": mediaType,
		"original":  resolved.Original,
		"checksum":  normalizedChecksum,
		"sourceId":  sourceID,
	}
	if useSourceOnTarget {
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
	if useSourceOnTarget && (resolved.Kind == "media" || resolved.Kind == "file") {
		out["effectiveSourcePath"] = resolved.Path
	} else {
		out["effectiveSourcePath"] = fmt.Sprintf(
			"{{ bootwright_provider_state_dir }}/os-install/%s/_source/%s.iso", clusterName, sourceID)
	}
	return out
}

func managedOSSourceID(checksum, key, original string) string {
	if checksum != "" {
		return checksum
	}
	if key != "" {
		return key
	}
	if original != "" {
		return managedOSSourceIDUnsafeRE.ReplaceAllString(original, "_")
	}
	return "source"
}

func machineOSInstallImageSourceOnTarget(state v1alpha1.State, m v1alpha1.InstallMachine) bool {
	host := managedOSTaskHost(state, m)
	machine, ok := stateview.Machine(state, host)
	if !ok {
		return host == "localhost"
	}
	return locality.IsControllerLocalMachine(machine, locality.DefaultPolicy)
}

func machineOSMediaType(source *v1alpha1.MachineInstallPackageSource) string {
	if source == nil {
		return v1alpha1.MachineImageMediaTypeDVD
	}
	return v1alpha1.MachineImageMediaTypeBoot
}

func machineInstallPackageSourceVars(source *v1alpha1.MachineInstallPackageSource, ents []v1alpha1.Entitlement, idx secret.Index, secretsDir string, eff *proxy.Effective) (string, []any, map[string]any) {
	rhsm := map[string]any{}
	if source == nil {
		return "", machineInstallRepositoryVars(nil, eff), rhsm
	}
	if m := source.Mirror; m != nil {
		return m.BaseURL, machineInstallRepositoryVars(m.Repositories, eff), rhsm
	}
	cdn := source.FromSubscription
	if cdn == nil || cdn.EntitlementRef.Name == "" {
		return "", machineInstallRepositoryVars(nil, eff), rhsm
	}
	resolved, ok := entitlements.Resolve(ents, idx, cdn.EntitlementRef.Name, "", secretsDir)
	if !ok {
		return "", nil, rhsm
	}
	rhsm["enabled"] = true
	rhsm["organizationPath"] = resolved.RHSM.OrganizationPath
	rhsm["activationKeyPath"] = resolved.RHSM.ActivationKeyPath
	rhsm["connectToInsights"] = resolved.RHSM.ConnectToInsights
	if satellite := rhsmSatelliteVars(resolved.RHSM.Satellite); satellite != nil {
		rhsm["satellite"] = satellite
	}
	return "", nil, rhsm
}

func rhsmSatelliteVars(satellite entitlements.RHSMSatellite) map[string]any {
	if satellite.Hostname == "" {
		return nil
	}
	out := map[string]any{"hostname": satellite.Hostname}
	if satellite.ContentBaseURL != "" {
		out["contentBaseURL"] = satellite.ContentBaseURL
	}
	if satellite.TrustBundlePath != "" {
		out["caPath"] = satellite.TrustBundlePath
	}
	return out
}

func machineInstallRepositoryVars(repos []v1alpha1.MachineInstallRepository, eff *proxy.Effective) []any {
	out := make([]any, 0, len(repos))
	for _, repo := range repos {
		entry := map[string]any{
			"id":      repo.ID,
			"baseURL": repo.BaseURL,
		}
		if installTargetProxied(eff, repo.BaseURL) {
			entry["proxied"] = true
		}
		out = append(out, entry)
	}
	return out
}

func machineInstallHostname(state v1alpha1.State, machine v1alpha1.Machine) string {
	if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok {
		return hostname
	}
	if fqdn := v1alpha1.MachineFQDNAddress(machine); fqdn != "" {
		return fqdn
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
		"rootDisk": strings.TrimPrefix(rootDevice, "/dev/"),
	}
	return out
}
