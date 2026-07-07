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

// managedOSSSHLoginPasswordHash is a stable SHA-512 crypt hash of a discarded
// high-entropy random value. Anaconda must leave the account unlocked for
// OpenSSH/PAM to accept public-key auth, while sshd password auth remains
// controlled separately by spec.customizations.ssh.passwordAuthentication.
const managedOSSSHLoginPasswordHash = "$6$bwmossshlogin$Ol7r6C1RKzV8XE5IhNM4r2XrCBVflPt5NX.xLZ51oqBSBQMN/cmkAP0nHExtyEB7NiXgGuYuU9PQwUDnJIo.x."

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
	sourceURL, imageRepositories, rhsm := machineImageInstallSourceVars(image.Spec.PackageSource, env, paths.SecretsDir)
	// hostedTree overrides the package source: bootwright extracts fromMedia (a
	// DVD) into the cluster artifact server and the installing node fetches
	// GPG-signed packages from that tree over the machineBoot endpoint. The DVD
	// .treeinfo already advertises BaseOS + AppStream, so one tree URL replaces
	// any repositories. An unresolvable tree leaves sourceURL empty so the boot
	// ISO install fails loudly (cdrom on a package-less ISO) instead of
	// mis-installing; the cluster-install validator rejects that up front.
	var hostedTree map[string]any
	if image.Spec.PackageSource.GetHostedTree() != nil {
		treeURL, tree, _ := machineOSHostedTreeVars(state, ci, image)
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
	}
	if len(rhsm) > 0 {
		installer["rhsm"] = rhsm
	}
	if proxyVars := managedOSInstallProxyVars(state, env, paths.SecretsDir); len(proxyVars) > 0 {
		installer["proxy"] = proxyVars
	}
	sshUser := managedOSSSHUser(machine)
	imageVars := machineOSInstallImageVars(resolved, machineOSMediaType(image.Spec.PackageSource), image.Spec.Checksum, machineOSInstallImageSourceOnTarget(state, m), clusterName)
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
			"sshPasswordHash":        managedOSSSHLoginPasswordHash,
			"sshPublicKeyPath":       secret.ResolveSSHPublicKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir),
			"passwordAuthentication": profile.Spec.Customizations.SSH.PasswordAuthentication,
			"authorizeMachineSSHKey": profile.Spec.Customizations.SSH.AuthorizeMachineSSHKey,
			"localization":           machineInstallLocalizationVars(profile.Spec.Customizations.Localization),
			"packages":               machineInstallPackagesVars(profile.Spec.Customizations.Packages),
			"services":               machineInstallServicesVars(profile.Spec.Customizations.Services),
			"security":               machineInstallSecurityVars(profile.Spec.Customizations.Security),
			"storage":                machineInstallStorageVars(profile, state, m),
			"network":                machineInstallNetworkVars(state, ci, m, clusterName),
		},
	}
	if machine.Spec.Access.SSH != nil {
		ssh := map[string]any{
			"address":        v1alpha1.MachineSSHAddress(machine),
			"user":           sshUser,
			"privateKeyPath": secret.ResolveSSHPrivateKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir),
		}
		// The managed trust store has no portable form: in placeholder mode these
		// resolve empty, so omit the keys rather than emit blank values (matching
		// how machineInventoryEntry / storageClusterSSHVars already guard them). An
		// explicit known-hosts SecretRef still tokenizes via machineKnownHostsPath.
		if knownHosts := machineKnownHostsPath(machine, env, paths); knownHosts != "" {
			ssh["knownHostsPath"] = knownHosts
		}
		if trustDir := sshtrust.DirForSecrets(paths.trustSecretsDir()); trustDir != "" {
			ssh["trustDir"] = trustDir
		}
		out["ssh"] = ssh
	}
	desiredNetwork := machineInstallDesiredNetwork(state, ci, m, clusterName)
	// The post-install network task applies the desired state with nmstatectl, so
	// the tool must be on the installed system. Inject it into the install package
	// set before the marker is computed: adding a package is an install-content
	// change, so it belongs in the marker (enabling post-install networking on an
	// owned node re-triggers the install that lays nmstate down).
	if len(desiredNetwork) > 0 {
		ensureKickstartPackage(out, "nmstate")
	}
	out["marker"] = machineOSInstallMarkerVars(out, clusterName, machine.Metadata.Name, profile.Metadata.Name)
	// The desired network state itself is applied idempotently every run and is
	// deliberately NOT part of the install marker: a network change re-applies
	// nmstate as a day-2 operation and must never force a destructive OS reinstall.
	if len(desiredNetwork) > 0 {
		out["network"] = map[string]any{"desiredState": desiredNetwork}
	}
	return out
}

// ensureKickstartPackage adds pkg to the kickstart install package list when not
// already present, so a feature that needs a tool on the installed system can
// require it without every MachineInstallProfile having to list it.
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

// machineOSInstallImageVars renders the normalize-materialized mediaType
// as-is; the boot.iso filename derivation lives in the normalize phase. It also
// owns the shared-source dedup identity (sourceId) and the effective source ISO
// path: every machine in an OS group installs from one source ISO staged once
// per (cluster, image), so the role consumes these rather than re-deriving the
// identity and path policy itself.
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

// managedOSSourceID is the shared-source dedup identity: the sha256 checksum
// when known (already normalized, no sha256: prefix), else the validated media
// key, else a sanitized original reference (a URL or file path has characters a
// filename cannot carry), else "source".
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

// machineOSMediaType derives the render-only image.mediaType the Ansible role
// keys its mkksiso path on: a full DVD (no packageSource) carries its packages,
// anything with a packageSource is a small boot ISO.
func machineOSMediaType(source *v1alpha1.MachinePackageSource) string {
	if source == nil {
		return v1alpha1.MachineImageMediaTypeDVD
	}
	return v1alpha1.MachineImageMediaTypeBoot
}

// machineImageInstallSourceVars projects the packageSource arm into the
// kickstart install-source vars (sourceURL, repositories, rhsm): mirror →
// BaseURL + repo entries; redhatCDN → resolved rhsm; nil (a full DVD) and
// hostedTree both return empty (nil installs via cdrom, and the caller overlays
// the derived tree URL for hostedTree).
func machineImageInstallSourceVars(source *v1alpha1.MachinePackageSource, env *v1alpha1.Environment, secretsDir string) (string, []any, map[string]any) {
	rhsm := map[string]any{}
	if source == nil {
		return "", machineInstallRepositoryVars(nil), rhsm
	}
	if m := source.Mirror; m != nil {
		return m.BaseURL, machineInstallRepositoryVars(m.Repositories), rhsm
	}
	cdn := source.RedhatCDN
	if cdn == nil || cdn.EntitlementRef.Name == "" {
		return "", machineInstallRepositoryVars(nil), rhsm
	}
	resolved, ok := entitlements.Resolve(env, cdn.EntitlementRef.Name, "", secretsDir)
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
	// ks.cfg.j2 reads only storage.rootDisk (clearpart --all is unconditional, so
	// customizations.storage.wipe is inert, and rootDevice is unused). Emitting only
	// the consumed key keeps the vars contract - and the install marker hash derived
	// from it - free of dead, misleading fields.
	out := map[string]any{
		"rootDisk": strings.TrimPrefix(rootDevice, "/dev/"),
	}
	return out
}
