package inventory

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestManagedOSInstallVarsFromCephLibvirtFixture(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("managed OS groups = %v", groups)
	}
	group := groups[0].(map[string]any)
	if got := group["name"]; got != "ceph-libvirt" {
		t.Fatalf("group name = %v", got)
	}
	components := group["components"].([]any)
	if len(components) != 3 {
		t.Fatalf("components = %v", components)
	}
	first := components[0].(map[string]any)
	if got := first["substratePrepareRole"]; got != "bootwright.core.machine_substrate_libvirt" {
		t.Fatalf("substratePrepareRole = %v", got)
	}
	if got := first["substratePrepareFrom"]; got != "network" {
		t.Fatalf("substratePrepareFrom = %v", got)
	}
	if got := first["substrateApplyFrom"]; got != "machine" {
		t.Fatalf("substrateApplyFrom = %v", got)
	}
	profile := first["profile"].(map[string]any)
	dataDisks := profile["dataDisks"].([]any)
	if len(dataDisks) != 2 {
		t.Fatalf("profile dataDisks = %v", dataDisks)
	}
	osInstall := first["osInstall"].(map[string]any)
	marker := osInstall["marker"].(map[string]any)
	if marker["owner"] != "bootwright" || marker["path"] != "/etc/bootwright/install-marker.json" {
		t.Fatalf("marker vars = %v", marker)
	}
	if hash, ok := marker["desiredHash"].(string); !ok || !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("marker desiredHash = %v, want sha256", marker["desiredHash"])
	}
	image := osInstall["image"].(map[string]any)
	if image["kind"] != "media" || image["key"] != "rhel-9.7-x86_64-dvd.iso" {
		t.Fatalf("image vars = %v", image)
	}
	if image["mediaType"] != "dvd" {
		t.Fatalf("image mediaType = %v", image["mediaType"])
	}
	if !strings.HasSuffix(image["path"].(string), "/media/rhel-9.7-x86_64-dvd.iso") {
		t.Fatalf("image path = %v", image["path"])
	}
	if image["sourceOnTarget"] != true {
		t.Fatalf("sourceOnTarget = %v, want true for controller-local provider host", image["sourceOnTarget"])
	}
	if image["sourceId"] != "rhel-9.7-x86_64-dvd.iso" {
		t.Fatalf("image sourceId = %v, want the media key", image["sourceId"])
	}
	if image["effectiveSourcePath"] != image["path"] {
		t.Fatalf("image effectiveSourcePath = %v, want image.path %v for sourceOnTarget media", image["effectiveSourcePath"], image["path"])
	}
	installer := osInstall["installer"].(map[string]any)
	if _, ok := installer["sourceURL"]; ok {
		t.Fatalf("installer.sourceURL = %v, want omitted for DVD media", installer["sourceURL"])
	}
	repositories := installer["repositories"].([]any)
	if len(repositories) != 0 {
		t.Fatalf("installer.repositories = %v", repositories)
	}
	if _, ok := installer["kernelArgs"]; ok {
		t.Fatalf("installer.kernelArgs = %v, want omitted when FIPS is disabled", installer["kernelArgs"])
	}
	ks := osInstall["kickstart"].(map[string]any)
	if ks["hostname"] != "ceph-0" {
		t.Fatalf("kickstart hostname = %v", ks["hostname"])
	}
	if got, _ := ks["sshPasswordHash"].(string); !strings.HasPrefix(got, "$6$") {
		t.Fatalf("kickstart sshPasswordHash = %v, want a SHA-512 crypt hash", ks["sshPasswordHash"])
	}
	packages := ks["packages"].(map[string]any)
	if packages["environment"] != "minimal" || packages["installWeakDeps"] != false || packages["excludeDocs"] != true {
		t.Fatalf("kickstart package options = %v", packages)
	}
	if got := packages["install"].([]string); !reflect.DeepEqual(got, []string{"podman", "lvm2", "chrony", "firewalld", "nmstate"}) {
		t.Fatalf("kickstart packages.install = %v", got)
	}
	if _, ok := packages["languages"]; ok {
		t.Fatalf("kickstart packages.languages = %v, want removed (folded into localization)", packages["languages"])
	}
	localization := ks["localization"].(map[string]any)
	if localization["language"] != "en_US.UTF-8" || localization["keyboard"] != "br-abnt2" || localization["timezone"] != "America/Sao_Paulo" {
		t.Fatalf("kickstart localization = %v", localization)
	}
	if localization["formats"] != "pt_BR.UTF-8" {
		t.Fatalf("kickstart localization.formats = %v, want pt_BR.UTF-8", localization["formats"])
	}
	if got := localization["instLangs"].([]string); !reflect.DeepEqual(got, []string{"en_US.UTF-8", "pt_BR.UTF-8"}) {
		t.Fatalf("kickstart localization.instLangs = %v, want [en_US.UTF-8 pt_BR.UTF-8]", got)
	}
	if got := localization["localePackages"].([]string); !reflect.DeepEqual(got, []string{"glibc-langpack-pt"}) {
		t.Fatalf("kickstart localization.localePackages = %v, want [glibc-langpack-pt]", got)
	}
	services := ks["services"].(map[string]any)
	if got := services["enabled"].([]string); !reflect.DeepEqual(got, []string{"sshd", "chronyd", "firewalld"}) {
		t.Fatalf("kickstart services.enabled = %v", got)
	}
	if got := services["disabled"].([]string); !reflect.DeepEqual(got, []string{"avahi-daemon", "cockpit.socket", "cups", "kdump", "postfix"}) {
		t.Fatalf("kickstart services.disabled = %v", got)
	}
	security := ks["security"].(map[string]any)
	selinux := security["selinux"].(map[string]any)
	if selinux["mode"] != "enforcing" {
		t.Fatalf("kickstart selinux = %v", selinux)
	}
	firewall := security["firewall"].(map[string]any)
	if firewall["enabled"] != true {
		t.Fatalf("kickstart firewall = %v", firewall)
	}
	if _, ok := security["fips"]; ok {
		t.Fatalf("kickstart fips = %v, want omitted when disabled", security["fips"])
	}
	network := ks["network"].(map[string]any)
	if network["ip"] != "192.168.134.20" || network["netmask"] != "255.255.255.0" {
		t.Fatalf("kickstart network = %v", network)
	}
	if got := network["device"]; got != "52:54:00:0f:07:d2" {
		t.Fatalf("kickstart network device = %v, want deterministic libvirt MAC", got)
	}
	ifaces := network["interfaces"].([]map[string]any)
	if len(ifaces) != 1 {
		t.Fatalf("kickstart network interfaces = %v, want one static ethernet stanza", ifaces)
	}
	if ifaces[0]["bootproto"] != "static" || ifaces[0]["device"] != "52:54:00:0f:07:d2" || ifaces[0]["hostname"] != true {
		t.Fatalf("kickstart network ethernet stanza = %v", ifaces[0])
	}
	storage := ks["storage"].(map[string]any)
	if storage["rootDisk"] != "vda" {
		t.Fatalf("kickstart storage = %v", storage)
	}
	if _, ok := storage["wipe"]; ok {
		t.Fatalf("kickstart storage must omit dead wipe var, got %v", storage)
	}
	if _, ok := storage["rootDevice"]; ok {
		t.Fatalf("kickstart storage must omit dead rootDevice var, got %v", storage)
	}
	boot := first["boot"].(map[string]any)
	iso := boot["agentIso"].(map[string]any)
	if !strings.Contains(iso["stagePath"].(string), "os-ceph-libvirt-ceph-0.iso") {
		t.Fatalf("managed OS stagePath = %v", iso["stagePath"])
	}
	readiness := boot["readiness"].(map[string]any)
	if readiness["type"] != "none" {
		t.Fatalf("managed OS boot readiness = %v, want none", readiness)
	}
	redfish := boot["redfish"].(map[string]any)
	if redfish["setBootSource"] != false {
		t.Fatalf("managed OS libvirt Redfish setBootSource = %v, want false", redfish["setBootSource"])
	}
}

func TestManagedOSInstallDefaultsOmittedSSHUserToRoot(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Machines[0].Spec.Access.SSH.User = ""

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	osInstall := first["osInstall"].(map[string]any)
	if got := osInstall["ssh"].(map[string]any)["user"]; got != "root" {
		t.Fatalf("managed OS ssh.user = %v, want root for omitted Machine.spec.access.ssh.user", got)
	}
	if got := osInstall["kickstart"].(map[string]any)["sshUser"]; got != "root" {
		t.Fatalf("managed OS kickstart.sshUser = %v, want root for omitted Machine.spec.access.ssh.user", got)
	}
	if got := osInstall["kickstart"].(map[string]any)["sshPasswordHash"]; got != managedOSSSHLoginPasswordHash {
		t.Fatalf("managed OS kickstart.sshPasswordHash = %v, want stable login hash", got)
	}
}

func TestManagedOSInstallVarsFromBootISOFixture(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "010-ceph-3nodes-libvirt-boot-iso")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("managed OS groups = %v", groups)
	}
	group := groups[0].(map[string]any)
	if got := group["name"]; got != "ceph-libvirt" {
		t.Fatalf("group name = %v", got)
	}
	components := group["components"].([]any)
	if len(components) != 3 {
		t.Fatalf("components = %v", components)
	}
	osInstall := components[0].(map[string]any)["osInstall"].(map[string]any)
	image := osInstall["image"].(map[string]any)
	if image["kind"] != "media" || image["key"] != "rhel-9.7-x86_64-boot.iso" {
		t.Fatalf("image vars = %v", image)
	}
	if image["mediaType"] != "boot" {
		t.Fatalf("image mediaType = %v, want boot", image["mediaType"])
	}
	installer := osInstall["installer"].(map[string]any)
	if got := installer["sourceURL"]; got != "https://mirror.example.test/rhel/9/BaseOS/x86_64/os/" {
		t.Fatalf("installer.sourceURL = %v, want the BaseOS install tree", got)
	}
	repositories := installer["repositories"].([]any)
	if len(repositories) != 1 {
		t.Fatalf("installer.repositories = %v, want one AppStream entry", repositories)
	}
	repo := repositories[0].(map[string]any)
	if repo["id"] != "appstream" || repo["baseURL"] != "https://mirror.example.test/rhel/9/AppStream/x86_64/os/" {
		t.Fatalf("installer.repositories[0] = %v", repo)
	}
}

func TestManagedOSInstallImageSourceIdentityAndPath(t *testing.T) {
	const sha = "abc123abc123abc123abc123abc123abc123abc123abc123abc123abc123abcd"

	cases := []struct {
		name           string
		resolved       media.Resolved
		checksum       string
		sourceOnTarget bool
		wantSourceID   string
		wantEffective  string
	}{
		{
			name:           "media in place when sourceOnTarget",
			resolved:       media.Resolved{Kind: "media", Key: "rhel-9.7-x86_64-dvd.iso", Path: "/var/lib/bootwright/media/rhel-9.7-x86_64-dvd.iso", Original: "local-media:rhel-9.7-x86_64-dvd.iso"},
			sourceOnTarget: true,
			wantSourceID:   "rhel-9.7-x86_64-dvd.iso",
			wantEffective:  "/var/lib/bootwright/media/rhel-9.7-x86_64-dvd.iso",
		},
		{
			name:           "file in place when sourceOnTarget",
			resolved:       media.Resolved{Kind: "file", Path: "/srv/isos/rhel.iso", Original: "file:///srv/isos/rhel.iso"},
			sourceOnTarget: true,
			wantSourceID:   "file____srv_isos_rhel.iso",
			wantEffective:  "/srv/isos/rhel.iso",
		},
		{
			name:          "shared staged source when not sourceOnTarget",
			resolved:      media.Resolved{Kind: "media", Key: "rhel-9.7-x86_64-dvd.iso", Path: "/var/lib/bootwright/media/rhel-9.7-x86_64-dvd.iso", Original: "local-media:rhel-9.7-x86_64-dvd.iso"},
			wantSourceID:  "rhel-9.7-x86_64-dvd.iso",
			wantEffective: "{{ bootwright_provider_state_dir }}/os-install/ceph-libvirt/_source/rhel-9.7-x86_64-dvd.iso.iso",
		},
		{
			name:          "checksum keys the shared source over key",
			resolved:      media.Resolved{Kind: "media", Key: "rhel-9.7-x86_64-dvd.iso", Path: "/var/lib/bootwright/media/rhel-9.7-x86_64-dvd.iso"},
			checksum:      "sha256:" + sha,
			wantSourceID:  sha,
			wantEffective: "{{ bootwright_provider_state_dir }}/os-install/ceph-libvirt/_source/" + sha + ".iso",
		},
		{
			name:          "url source stages under a sanitized original",
			resolved:      media.Resolved{Kind: "url", URL: "https://mirror.example.test/rhel.iso", Original: "https://mirror.example.test/rhel.iso"},
			wantSourceID:  "https___mirror.example.test_rhel.iso",
			wantEffective: "{{ bootwright_provider_state_dir }}/os-install/ceph-libvirt/_source/https___mirror.example.test_rhel.iso.iso",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			image := machineOSInstallImageVars(tc.resolved, "dvd", tc.checksum, tc.sourceOnTarget, "ceph-libvirt")
			if image["sourceId"] != tc.wantSourceID {
				t.Fatalf("sourceId = %v, want %v", image["sourceId"], tc.wantSourceID)
			}
			if image["effectiveSourcePath"] != tc.wantEffective {
				t.Fatalf("effectiveSourcePath = %v, want %v", image["effectiveSourcePath"], tc.wantEffective)
			}
		})
	}
}

func TestManagedOSInstallRendersFIPSKernelArgs(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.MachineInstallProfiles[0].Spec.Customizations.Security.FIPS.Enabled = true

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	osInstall := first["osInstall"].(map[string]any)
	installer := osInstall["installer"].(map[string]any)
	if got := installer["kernelArgs"].([]string); !reflect.DeepEqual(got, []string{"fips=1"}) {
		t.Fatalf("installer.kernelArgs = %v", got)
	}
	security := osInstall["kickstart"].(map[string]any)["security"].(map[string]any)
	if _, ok := security["fips"]; ok {
		t.Fatalf("kickstart security must omit fips (delivered via kernelArgs), got %v", security["fips"])
	}
}

func TestManagedOSInstallUsesImageSourceURL(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
		Mirror: &v1alpha1.MachineInstallPackageMirror{
			BaseURL: "https://repos.example.test/rhel/9/BaseOS/x86_64/os/",
			Repositories: []v1alpha1.MachineInstallRepository{
				{ID: "appstream", BaseURL: "https://repos.example.test/rhel/9/AppStream/x86_64/os/"},
			},
		},
	}

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	if got := installer["sourceURL"]; got != "https://repos.example.test/rhel/9/BaseOS/x86_64/os/" {
		t.Fatalf("installer.sourceURL = %v", got)
	}
	repositories := installer["repositories"].([]any)
	if len(repositories) != 1 {
		t.Fatalf("repositories = %v", repositories)
	}
}

func TestManagedOSInstallHostedTreeFailsClosedWithoutEndpoint(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
		HostedTree: &v1alpha1.MachineInstallPackageHostedTree{
			FromMedia: "local-media:rhel-9.7-x86_64-dvd.iso",
		},
	}

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	osInstall := first["osInstall"].(map[string]any)
	installer := osInstall["installer"].(map[string]any)
	if got, ok := installer["sourceURL"]; ok {
		t.Fatalf("installer.sourceURL = %v, want unset when the hosted-tree endpoint does not resolve", got)
	}
	if _, ok := osInstall["image"].(map[string]any)["installTree"]; ok {
		t.Fatalf("image.installTree must be absent when the hosted tree cannot resolve")
	}
}

func TestManagedOSInstallUsesRHSMInstallSource(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	upsertSecret(&state, "redhat-org", v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}, "")
	upsertSecret(&state, "redhat-activation-key", v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}, "")
	state.Entitlements = append(state.Entitlements, v1alpha1.Entitlement{
		Metadata: v1alpha1.Metadata{Name: "rhel"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatRHEL,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:   v1alpha1.SecretRef{Name: "redhat-org"},
				ActivationKeyRef:  v1alpha1.SecretRef{Name: "redhat-activation-key"},
				ConnectToInsights: true,
			},
		},
	})
	state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
		RedhatCDN: &v1alpha1.MachineInstallPackageRedhatCDN{
			EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
		},
	}

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	rhsm := installer["rhsm"].(map[string]any)
	if rhsm["organizationPath"] != "/context/secrets/redhat-org" {
		t.Fatalf("rhsm.organizationPath = %v", rhsm["organizationPath"])
	}
	if rhsm["activationKeyPath"] != "/context/secrets/redhat-activation-key" {
		t.Fatalf("rhsm.activationKeyPath = %v", rhsm["activationKeyPath"])
	}
	if rhsm["connectToInsights"] != true {
		t.Fatalf("rhsm.connectToInsights = %v", rhsm["connectToInsights"])
	}
	if _, ok := rhsm["satellite"]; ok {
		t.Fatalf("public-CDN rhsm must carry no satellite key, got %v", rhsm["satellite"])
	}
}

func TestManagedOSInstallRedirectsRHSMToSatellite(t *testing.T) {
	loadState := func() v1alpha1.State {
		state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
		if err != nil {
			t.Fatalf("LoadNormalizeValidate: %v", err)
		}
		upsertSecret(&state, "redhat-org", v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}, "")
		upsertSecret(&state, "redhat-activation-key", v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeOpaque}, "")
		upsertSecret(&state, "corp-satellite-ca", v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeCABundle}, "")
		state.Entitlements = append(state.Entitlements, v1alpha1.Entitlement{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.EntitlementSpec{
				Type: v1alpha1.EntitlementTypeRedHatRHEL,
				RHSM: &v1alpha1.EntitlementRHSM{
					OrganizationRef:   v1alpha1.SecretRef{Name: "redhat-org"},
					ActivationKeyRef:  v1alpha1.SecretRef{Name: "redhat-activation-key"},
					ConnectToInsights: true,
					Satellite: &v1alpha1.EntitlementRHSMSatellite{
						Hostname:       "satellite.corp.example.com",
						ContentBaseURL: "https://satellite.corp.example.com/pulp/content",
						TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
					},
				},
			},
		})
		state.MachineInstallProfiles[0].Spec.Installer.Anaconda.PackageSource = &v1alpha1.MachineInstallPackageSource{
			RedhatCDN: &v1alpha1.MachineInstallPackageRedhatCDN{
				EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
			},
		}
		return state
	}

	render := func(secretsDir string) map[string]any {
		vars := VarsWithSecretsDir(loadState(), secretsDir)
		groups := vars["bootwright_managed_os_install_groups"].([]any)
		first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
		return first["osInstall"].(map[string]any)
	}

	osInstall := render("/context/secrets")
	rhsm := osInstall["installer"].(map[string]any)["rhsm"].(map[string]any)
	satellite, ok := rhsm["satellite"].(map[string]any)
	if !ok {
		t.Fatalf("installer.rhsm.satellite missing, rhsm=%v", rhsm)
	}
	if satellite["hostname"] != "satellite.corp.example.com" {
		t.Fatalf("satellite.hostname = %v", satellite["hostname"])
	}
	if satellite["contentBaseURL"] != "https://satellite.corp.example.com/pulp/content" {
		t.Fatalf("satellite.contentBaseURL = %v", satellite["contentBaseURL"])
	}
	if satellite["caPath"] != "/context/secrets/corp-satellite-ca" {
		t.Fatalf("satellite.caPath = %v", satellite["caPath"])
	}

	hashA := render("/context/secrets/run-a")["marker"].(map[string]any)["desiredHash"]
	hashB := render("/context/secrets/run-b")["marker"].(map[string]any)["desiredHash"]
	if hashA == "" || hashA != hashB {
		t.Fatalf("install marker not stable across secrets dirs: %v vs %v", hashA, hashB)
	}
}

func TestManagedOSInstallRoutesThroughMachineOSInstallProxy(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	upsertSecret(&state, "proxy-credentials", v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeUsernamePassword}, "")
	state.Environments[0].Spec.InfraComponents.Proxies = append(state.Environments[0].Spec.InfraComponents.Proxies, v1alpha1.EnvironmentProxyComponent{
		Name:       "corp",
		Management: v1alpha1.EnvironmentComponentExternal,
		Connection: &v1alpha1.EnvironmentProxyConnection{
			HTTPProxy:  "http://proxy.example.test:8080",
			HTTPSProxy: "http://proxy.example.test:8080",
			Auth:       &v1alpha1.EnvironmentProxyAuthSpec{ProxyAuthRef: v1alpha1.SecretRef{Name: "proxy-credentials"}},
		},
	})
	state.Environments[0].Spec.ProxyFor.MachineOSInstall = "corp"

	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	proxyVars, ok := installer["proxy"].(map[string]any)
	if !ok {
		t.Fatalf("installer.proxy = %v, want a map", installer["proxy"])
	}
	if got := proxyVars["url"]; got != "http://proxy.example.test:8080" {
		t.Fatalf("installer.proxy.url = %v", got)
	}
	if got := proxyVars["credentialsPath"]; got != "/context/secrets/proxy-credentials" {
		t.Fatalf("installer.proxy.credentialsPath = %v", got)
	}
}

func TestManagedOSInstallOmitsProxyWhenSlotUnset(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	vars := VarsWithSecretsDir(state, "/context/secrets")
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	first := groups[0].(map[string]any)["components"].([]any)[0].(map[string]any)
	installer := first["osInstall"].(map[string]any)["installer"].(map[string]any)
	if _, ok := installer["proxy"]; ok {
		t.Fatalf("installer.proxy = %v, want omitted with no machineOSInstall proxy", installer["proxy"])
	}
}

func TestManagedStorageOSMachinesEnterInfraInventory(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	members := HostGroupMembers(state)
	if got := strings.Join(members[GroupInfraHosts], ","); got != "bastion" {
		t.Fatalf("infra hosts = %v", members[GroupInfraHosts])
	}
	if got := strings.Join(members[GroupProviderHosts], ","); got != "bastion" {
		t.Fatalf("provider hosts = %v", members[GroupProviderHosts])
	}
	wantMachineTaskHosts := strings.Join([]string{
		ManagedOSHostName("ceph-libvirt", "ceph-0"),
		ManagedOSHostName("ceph-libvirt", "ceph-1"),
		ManagedOSHostName("ceph-libvirt", "ceph-2"),
	}, ",")
	if got := strings.Join(members[GroupMachineTaskHosts], ","); got != wantMachineTaskHosts {
		t.Fatalf("machine task hosts = %v, want %s", members[GroupMachineTaskHosts], wantMachineTaskHosts)
	}
	if got := strings.Join(members[ManagedOSGroupName("ceph-libvirt")], ","); got != wantMachineTaskHosts {
		t.Fatalf("managed OS group hosts = %v, want %s", members[ManagedOSGroupName("ceph-libvirt")], wantMachineTaskHosts)
	}

	inv := Inventory(state, "/context/secrets")
	all := inv["all"].(map[string]any)
	hosts := all["hosts"].(map[string]any)
	pseudoHost := hosts[ManagedOSHostName("ceph-libvirt", "ceph-0")].(map[string]any)
	if got := pseudoHost["bootwright_machine_task_cluster_name"]; got != "ceph-libvirt" {
		t.Fatalf("machine task cluster = %v", got)
	}
	if got := pseudoHost["bootwright_machine_task_machine_name"]; got != "ceph-0" {
		t.Fatalf("machine task machine = %v", got)
	}
	if got := pseudoHost["bootwright_machine_task_provider_host_name"]; got != "bastion" {
		t.Fatalf("machine task provider host = %v", got)
	}
}

func TestManagedOSInstallMarkerHashStableAcrossSecretsDir(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	hashA := firstManagedOSMarkerHash(t, VarsWithSecretsDir(state, "/var/lib/bootwright/contexts/c/runs/history/apply-A/tasks/t/artifacts/runtime/secrets"))
	hashB := firstManagedOSMarkerHash(t, VarsWithSecretsDir(state, "/var/lib/bootwright/contexts/c/runs/history/apply-B/tasks/t/artifacts/runtime/secrets"))
	if hashA != hashB {
		t.Fatalf("marker desiredHash must be stable across per-run secrets dirs:\n  A=%s\n  B=%s", hashA, hashB)
	}
}

func firstManagedOSMarkerHash(t *testing.T, vars map[string]any) string {
	t.Helper()
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	group := groups[0].(map[string]any)
	components := group["components"].([]any)
	first := components[0].(map[string]any)
	osInstall := first["osInstall"].(map[string]any)
	marker := osInstall["marker"].(map[string]any)
	return marker["desiredHash"].(string)
}

func TestMachineInstallLocalizationVarsDefaultsAndFormatSplit(t *testing.T) {
	base := machineInstallLocalizationVars(v1alpha1.MachineInstallLocalization{})
	if base["language"] != "en_US.UTF-8" || base["keyboard"] != "us" || base["timezone"] != "UTC" {
		t.Fatalf("default localization = %v", base)
	}
	if _, ok := base["formats"]; ok {
		t.Fatalf("default localization must omit formats, got %v", base["formats"])
	}
	if _, ok := base["localePackages"]; ok {
		t.Fatalf("default localization must omit localePackages, got %v", base["localePackages"])
	}
	if got := base["instLangs"].([]string); !reflect.DeepEqual(got, []string{"en_US.UTF-8"}) {
		t.Fatalf("default localization instLangs = %v, want [en_US.UTF-8]", got)
	}
	split := machineInstallLocalizationVars(v1alpha1.MachineInstallLocalization{
		Language:          "en_US.UTF-8",
		Formats:           "pt_BR.UTF-8",
		Keyboard:          "br-abnt2",
		Timezone:          "America/Sao_Paulo",
		AdditionalLocales: []string{"es_ES.UTF-8", "pt_BR.UTF-8"},
	})
	if split["language"] != "en_US.UTF-8" || split["formats"] != "pt_BR.UTF-8" {
		t.Fatalf("split localization language/formats = %v", split)
	}
	if split["keyboard"] != "br-abnt2" || split["timezone"] != "America/Sao_Paulo" {
		t.Fatalf("split localization keyboard/timezone = %v", split)
	}
	if got := split["instLangs"].([]string); !reflect.DeepEqual(got, []string{"en_US.UTF-8", "pt_BR.UTF-8", "es_ES.UTF-8"}) {
		t.Fatalf("split localization instLangs = %v, want [en_US.UTF-8 pt_BR.UTF-8 es_ES.UTF-8] (deduped, language first)", got)
	}
	if got := split["localePackages"].([]string); !reflect.DeepEqual(got, []string{"glibc-langpack-pt", "glibc-langpack-es"}) {
		t.Fatalf("split localization localePackages = %v, want [glibc-langpack-pt glibc-langpack-es] (regional only, system language excluded)", got)
	}
}
