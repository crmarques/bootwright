package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 006 fixture's three ceph nodes all install a managed OS from the same
// local-media image, so the check must fail once (deduped) with an actionable
// "bootwright media add" remediation when the source ISO is absent.
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

// The source ISO is only needed once the machines phase provisions the OS, so a
// phase selection that excludes it (e.g. an addons-only apply) skips the check.
func TestInstallerMediaCheckSkippedOutsideMachinesPhase(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}

	if checks := installerMediaChecks(state, []Phase{{Name: "add-ons"}}, deps, nil); len(checks) != 0 {
		t.Fatalf("media checks = %+v, want none outside the machines phase", checks)
	}
}

// A scoped run that does not act on the managed-OS machine must not require its
// install media.
func TestInstallerMediaCheckHonorsSecretScope(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{StatPath: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }}
	scope := &SecretScope{Machines: map[string]bool{"some-other-machine": true}}

	if checks := installerMediaChecks(state, []Phase{{Name: "machines"}}, deps, scope); len(checks) != 0 {
		t.Fatalf("media checks = %+v, want none for out-of-scope machines", checks)
	}
}

// CollectChecks must wire the media check into the apply/preflight host check.
func TestCollectChecksIncludesInstallerMedia(t *testing.T) {
	state := loadFixtureState(t, "006-ceph-3nodes-libvirt-managed-os")
	deps := Deps{
		LookPath:      func(name string, _ []string) (string, error) { return "/bin/" + name, nil },
		StatPath:      func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		CommandOutput: func(string, ...string) ([]byte, error) { return []byte("Python 3.12.4"), nil },
		UID:           func() int { return 0 },
	}
	checks := CollectChecks(state, []Phase{{Name: "machines"}}, true, "test", "/context/secrets", "/host-state", deps, nil, nil)
	assertPreflightCheckStatus(t, checks, "local-media:rhel-9.7-x86_64-dvd.iso", "FAIL")
}
