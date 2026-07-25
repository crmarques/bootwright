package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInstallerMediaCheckFailsWhenSourceMissing(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}

	checks := installerMediaChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 1 {
		t.Fatalf("media checks = %d, want 1 deduped across nodes: %+v", len(checks), checks)
	}
	c := checks[0]
	if c.Group != checkGroupInstallerMedia || c.Status != StatusFail {
		t.Fatalf("check = %+v, want FAIL in %q", c, checkGroupInstallerMedia)
	}
	if c.Name != "local-media:rhel-9.7-x86_64-dvd.iso" {
		t.Fatalf("name = %q, want the operator-facing reference", c.Name)
	}
	if !strings.Contains(c.Remediation, "bootwright media add --name rhel-9.7-x86_64-dvd.iso") {
		t.Fatalf("remediation = %q, want a bootwright media add hint", c.Remediation)
	}
}

func TestInstallerMediaCheckPassesWhenSourcePresent(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	regular := filepath.Join(t.TempDir(), "rhel.iso")
	if err := os.WriteFile(regular, []byte("iso"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(regular)
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return info, nil }}

	checks := installerMediaChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 1 || checks[0].Status != StatusOK {
		t.Fatalf("media checks = %+v, want one OK", checks)
	}
}

func TestInstallerMediaCheckSkippedOutsideMachinesPhase(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}

	if checks := installerMediaChecks(state, []Phase{{Name: "add-ons"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("media checks = %+v, want none outside the machines phase", checks)
	}
}

func TestInstallerMediaCheckHonorsSecretScope(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}
	scope := &SecretScope{Machines: map[string]bool{"some-other-machine": true}}

	if checks := installerMediaChecks(state, []Phase{{Name: "machines"}}, deps, scope); len(checks) != 0 {
		t.Fatalf("media checks = %+v, want none for out-of-scope machines", checks)
	}
}

func TestInstallerMediaCheckReportsRemoteURL(t *testing.T) {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "node-0"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided:          v1alpha1.BoolPtr(false),
					InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel"},
				},
			},
		}},
		MachineInstallProfiles: []v1alpha1.MachineInstallProfile{{
			Metadata: v1alpha1.Metadata{Name: "rhel"},
			Spec: v1alpha1.MachineInstallProfileSpec{
				Installer: v1alpha1.MachineInstallProfileInstaller{
					Anaconda: &v1alpha1.MachineInstallAnaconda{
						ImageRef: v1alpha1.LocalObjectReference{Name: "rhel-img"},
					},
				},
			},
		}},
		MachineImages: []v1alpha1.MachineImage{{
			Metadata: v1alpha1.Metadata{Name: "rhel-img"},
			Spec:     v1alpha1.MachineImageSpec{BootMedia: "https://mirror.example.test/rhel-9.7.iso"},
		}},
	}
	deps := Deps{StatPath: func(string) (os.FileInfo, error) {
		t.Fatal("StatPath must not be called for a remote-URL ISO")
		return nil, nil
	}}

	checks := installerMediaChecks(state, []Phase{{Name: "machines"}}, deps, nil)
	if len(checks) != 1 {
		t.Fatalf("media checks = %d, want 1: %+v", len(checks), checks)
	}
	c := checks[0]
	if c.Status != StatusInfo || c.Group != checkGroupInstallerMedia {
		t.Fatalf("check = %+v, want INFO in %q", c, checkGroupInstallerMedia)
	}
	if c.Name != "https://mirror.example.test/rhel-9.7.iso" {
		t.Fatalf("name = %q, want the URL reference", c.Name)
	}
	if !strings.Contains(c.Evidence, "bootwright media add --from-url") {
		t.Fatalf("evidence = %q, want a media-store staging hint", c.Evidence)
	}
}

func TestCollectChecksIncludesInstallerMedia(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{
		LookPath:      func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath:      func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		CommandOutput: func(string, ...string) ([]byte, error) { return []byte("Python 3.12.4"), nil },
		UID:           func() int { return 0 },
	}
	checks := CollectChecks(state, []Phase{{Name: "machines"}}, true, "test", "/context/secrets", "/host-state", "/runs", deps, nil, nil)
	assertPreflightCheckStatus(t, checks, "local-media:rhel-9.7-x86_64-dvd.iso", "FAIL")
}
