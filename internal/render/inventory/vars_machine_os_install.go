package inventory

import (
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
	if proxyVars := managedOSInstallProxyVars(state, env, paths.SecretsDir); len(proxyVars) > 0 {
		installer["proxy"] = proxyVars
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
	// Resolve the host that actually drives the managed-OS install, mirroring
	// managedOSTaskHost: libvirt machines install on their provider host, while
	// API-native substrates (KubeVirt, vSphere) and bare-metal over the BMC all
	// run from the controller (localhost). When that host is the controller, a
	// file/media-library source already lives on it, so the copy-to-provider
	// step is pure waste — skip it and point mkksiso at the source in place.
	host := managedOSTaskHost(state, m)
	machine, ok := stateview.Machine(state, host)
	if !ok {
		return host == "localhost"
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
		if satellite := rhsmSatelliteVars(resolved.RHSM.Satellite); satellite != nil {
			rhsm["satellite"] = satellite
		}
	}
	return source.URL, machineInstallRepositoryVars(source.Repositories), rhsm
}

// managedOSInstallProxyVars resolves the environment proxy named by
// spec.proxyFor.machineOSInstall into the kickstart proxy inputs. A boot ISO
// fetches packages (or registers against the Red Hat CDN) over the network
// during install, so on a proxied estate that traffic must traverse this
// proxy. Only an external proxy applies — the node installs before any managed
// proxy could exist — so a managed or unset selection emits nothing. The
// credentialed proxy URL is assembled in the kickstart from this
// (unauthenticated) url plus the proxy credentials file, keeping the proxy
// password out of vars.yaml the same way rhsm secrets stay out of it.
func managedOSInstallProxyVars(state v1alpha1.State, env *v1alpha1.Environment, secretsDir string) map[string]any {
	if env == nil {
		return nil
	}
	eff := proxy.ResolveFor(state, env, env.Spec.ProxyFor.MachineOSInstall)
	if eff == nil {
		return nil
	}
	url := eff.HTTPS
	if url == "" {
		url = eff.HTTP
	}
	if url == "" {
		return nil
	}
	out := map[string]any{"url": url}
	if eff.Auth.Name != "" {
		if path := secret.ResolveMaterialPath(eff.Auth.Name, env, secretsDir, secret.MaterialPrimary); path != "" {
			out["credentialsPath"] = path
		}
	}
	return out
}

// rhsmSatelliteVars projects a resolved corporate Satellite redirect into the
// nested install/day-2 vars map (hostname, contentBaseURL, caPath). It returns
// nil when registration targets the public Red Hat CDN, so callers omit the
// satellite key entirely and existing CDN renders stay byte-identical.
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
