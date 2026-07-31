package inventory

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func managedOSStateWithDiskEncryption(t *testing.T, tpm2 *v1alpha1.DiskEncryptionTPM2, fips bool) v1alpha1.State {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Secrets = append(state.Secrets, v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: "luks-recovery"},
		Spec:     v1alpha1.SecretSpec{Type: v1alpha1.SecretTypeToken},
	})
	for i := range state.MachineInstallProfiles {
		security := &state.MachineInstallProfiles[i].Spec.Customizations.Security
		security.FIPS.Enabled = fips
		security.DiskEncryption = &v1alpha1.MachineInstallDiskEncryption{
			Unlock:                v1alpha1.DiskEncryptionUnlock{TPM2: tpm2},
			RecoveryPassphraseRef: v1alpha1.SecretRef{Name: "luks-recovery"},
		}
	}
	return state
}

func firstManagedOSKickstart(t *testing.T, state v1alpha1.State, secretsDir string) map[string]any {
	t.Helper()
	vars := VarsWithSecretsDir(state, secretsDir)
	groups := vars["bootwright_managed_os_install_groups"].([]any)
	components := groups[0].(map[string]any)["components"].([]any)
	osInstall := components[0].(map[string]any)["osInstall"].(map[string]any)
	return osInstall["kickstart"].(map[string]any)
}

func TestManagedOSKickstartCarriesDiskEncryption(t *testing.T) {
	state := managedOSStateWithDiskEncryption(t, &v1alpha1.DiskEncryptionTPM2{}, false)
	kickstart := firstManagedOSKickstart(t, state, "/context/secrets")
	security := kickstart["security"].(map[string]any)
	encryption, ok := security["diskEncryption"].(map[string]any)
	if !ok {
		t.Fatalf("kickstart security = %v, want a diskEncryption block", security)
	}
	if got := encryption["luksVersion"]; got != v1alpha1.DiskEncryptionLUKSVersion {
		t.Fatalf("luksVersion = %v, want %s", got, v1alpha1.DiskEncryptionLUKSVersion)
	}
	if got := encryption["passphrasePath"]; got != "/context/secrets/luks-recovery" {
		t.Fatalf("passphrasePath = %v", got)
	}
	if _, ok := encryption["pbkdf"]; ok {
		t.Fatalf("pbkdf = %v, want it unset outside FIPS", encryption["pbkdf"])
	}
	clevis := encryption["clevis"].(map[string]any)
	if clevis["pin"] != v1alpha1.DiskEncryptionClevisPinTPM2 {
		t.Fatalf("clevis pin = %v", clevis["pin"])
	}
	if clevis["config"] != `{"hash":"sha256","key":"ecc"}` {
		t.Fatalf("clevis config = %v", clevis["config"])
	}
	if encryption["measuresBoot"] != false {
		t.Fatalf("measuresBoot = %v, want false without pcrIds", encryption["measuresBoot"])
	}
	packages := kickstart["packages"].(map[string]any)["install"].([]string)
	for _, want := range []string{"clevis", "clevis-dracut", "clevis-luks", "clevis-systemd", "tpm2-tools", v1alpha1.TPM2StackPackage} {
		if !containsString(packages, want) {
			t.Fatalf("kickstart packages %v are missing %q; the installed node cannot bind or unlock the LUKS volume without it", packages, want)
		}
	}
}

func TestManagedOSKickstartPinsPBKDF2UnderFIPS(t *testing.T) {
	state := managedOSStateWithDiskEncryption(t, &v1alpha1.DiskEncryptionTPM2{}, true)
	kickstart := firstManagedOSKickstart(t, state, "/context/secrets")
	encryption := kickstart["security"].(map[string]any)["diskEncryption"].(map[string]any)
	if got := encryption["pbkdf"]; got != v1alpha1.DiskEncryptionFIPSPBKDF {
		t.Fatalf("pbkdf = %v, want %s: Argon2 is not FIPS-approved and cryptsetup refuses it in FIPS mode", got, v1alpha1.DiskEncryptionFIPSPBKDF)
	}
}

func TestManagedOSKickstartWithoutDiskEncryptionStaysBare(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "..", "test", "e2e", "006-ceph-3nodes-libvirt-managed-os")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	kickstart := firstManagedOSKickstart(t, state, "/context/secrets")
	if _, ok := kickstart["security"].(map[string]any)["diskEncryption"]; ok {
		t.Fatal("kickstart carries diskEncryption without customizations.security.diskEncryption")
	}
	packages := kickstart["packages"].(map[string]any)["install"].([]string)
	if containsString(packages, "clevis") {
		t.Fatalf("kickstart packages %v pull clevis in without disk encryption", packages)
	}
}

func TestManagedOSMarkerHashIgnoresThePassphraseSecretDir(t *testing.T) {
	hash := func(secretsDir string) string {
		state := managedOSStateWithDiskEncryption(t, &v1alpha1.DiskEncryptionTPM2{}, false)
		vars := VarsWithSecretsDir(state, secretsDir)
		groups := vars["bootwright_managed_os_install_groups"].([]any)
		components := groups[0].(map[string]any)["components"].([]any)
		osInstall := components[0].(map[string]any)["osInstall"].(map[string]any)
		return osInstall["marker"].(map[string]any)["desiredHash"].(string)
	}
	if a, b := hash("/runs/run-a/secrets"), hash("/runs/run-b/secrets"); a != b {
		t.Fatalf("marker hash changed across per-run secret dirs: %s vs %s", a, b)
	}
}

func TestManagedOSMarkerHashTracksThePCRPolicy(t *testing.T) {
	hash := func(tpm2 *v1alpha1.DiskEncryptionTPM2) string {
		state := managedOSStateWithDiskEncryption(t, tpm2, false)
		vars := VarsWithSecretsDir(state, "/context/secrets")
		groups := vars["bootwright_managed_os_install_groups"].([]any)
		components := groups[0].(map[string]any)["components"].([]any)
		osInstall := components[0].(map[string]any)["osInstall"].(map[string]any)
		return osInstall["marker"].(map[string]any)["desiredHash"].(string)
	}
	if a, b := hash(&v1alpha1.DiskEncryptionTPM2{}), hash(&v1alpha1.DiskEncryptionTPM2{PCRIDs: []int{7}}); a == b {
		t.Fatal("marker hash did not move when the TPM policy changed; the node would keep an unlock policy the desired state no longer asks for")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
