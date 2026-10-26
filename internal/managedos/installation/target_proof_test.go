package installation

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// installerNIC is one network interface the installer sees; a driver of ""
// is a virtual interface, backed by no device.
type installerNIC struct {
	name, address, driver, permanent string
}

type installerHost struct {
	nics []installerNIC
	root string
}

// runTargetProof runs the rendered target-proof %pre against a synthetic
// host: its network interfaces under a temporary sysfs, and readlink, the
// block-device test and lsblk answering for the one disk tree it holds, where
// /dev/sda is a whole disk and /dev/sda1 a partition of it.
func runTargetProof(t *testing.T, input Installation, host installerHost) (string, error) {
	t.Helper()
	rendered, err := RenderKickstart(input)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(rendered, "\n")
	start := lineIndex(t, lines, "%pre --erroronfail --interpreter=/bin/bash")
	end := start + lineIndex(t, lines[start:], "%end")
	for _, clause := range []string{"ignoredisk", "clearpart", "autopart"} {
		if at := strings.Index(rendered, clause); at >= 0 && at < strings.Index(rendered, "%pre --erroronfail") {
			t.Fatalf("%s is rendered before the target proof", clause)
		}
	}
	sysfs := filepath.Join(t.TempDir(), "net")
	for _, nic := range host.nics {
		directory := filepath.Join(sysfs, nic.name)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "address"), []byte(nic.address+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if nic.permanent != "" {
			if err := os.MkdirAll(filepath.Join(directory, "bonding_slave"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "bonding_slave", "perm_hwaddr"), []byte(nic.permanent+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if nic.driver == "" {
			continue
		}
		device, driver := filepath.Join(sysfs, "..", "devices", nic.name), filepath.Join(sysfs, "..", "drivers", nic.driver)
		for _, made := range []error{os.MkdirAll(device, 0o755), os.MkdirAll(driver, 0o755),
			os.Symlink(device, filepath.Join(directory, "device")), os.Symlink(driver, filepath.Join(device, "driver"))} {
			if made != nil && !os.IsExist(made) {
				t.Fatal(made)
			}
		}
	}
	script := strings.ReplaceAll(strings.Join(lines[start+1:end], "\n"), "/sys/class/net", sysfs)
	mocks := `
readlink() { case "${@: -1}" in /dev/*) ;; *) command readlink "$@"; return;; esac; if builtin [ -z "$resolved" ]; then return 1; fi; printf '%s\n' "$resolved"; }
[() { if builtin [ "$1 $2" = "! -b" ]; then case "$3" in /dev/sda|/dev/sda1) return 1;; *) return 0;; esac; fi; builtin [ "$@"; }
lsblk() { case "${@: -1}" in /dev/sda) echo disk;; /dev/sda1) echo part;; esac; }
`
	command := exec.Command("/bin/bash", "-c", mocks+script+"\necho 'reached storage'\n")
	command.Env = append(os.Environ(), "resolved="+host.root)
	output, err := command.CombinedOutput()
	if err == nil && !strings.Contains(string(output), "reached storage") {
		t.Fatalf("the target proof exited cleanly without reaching storage:\n%s", output)
	}
	if err != nil && strings.Contains(string(output), "reached storage") {
		t.Fatalf("the target proof failed after reaching storage:\n%s", output)
	}
	return string(output), err
}

// The %pre resolves the declared by-id path on the booted machine and lets
// the installer reach a storage directive only when it names a whole disk: a
// path that resolves to a partition, or to nothing, stops it first.
func TestThePhysicalTargetProofStopsBeforeStorageUnlessTheRootIsAWholeDisk(t *testing.T) {
	declared := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}}
	for name, want := range map[string]struct {
		root, refusal string
	}{
		"a whole disk":       {"/dev/sda", ""},
		"a partition":        {"/dev/sda1", "is not a whole disk"},
		"nothing":            {"", "is not a block device here"},
		"not a block device": {"/tmp/disk", "is not a block device here"},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := runTargetProof(t, physicalInstallation(), installerHost{nics: declared, root: want.root})
			if want.refusal == "" {
				if err != nil {
					t.Fatalf("a whole disk was refused: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, "the declared root device /dev/disk/by-id/wwn-0x5000c500a1b2c3d4 "+want.refusal+"; refusing before any disk is touched.") {
				t.Fatalf("%s was not refused before storage: %v\n%s", name, err, output)
			}
		})
	}
}

// The installer repeats the MAC-set predicate: the device-backed ports must
// report exactly the declared set, a management controller's USB host
// interface and a virtual interface excepted, so a missing declared address
// and an extra reported one both stop it before storage.
func TestThePhysicalTargetProofRequiresExactlyTheDeclaredMACSet(t *testing.T) {
	ports := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9A:1B:02", "e1000e", ""}}
	for name, want := range map[string]struct {
		nics    []installerNIC
		refusal string
	}{
		"exactly the declared set": {ports, ""},
		"bond ports reporting the bond's address": {[]installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", "52:54:00:9a:1b:01"},
			{"eno2", "52:54:00:9a:1b:01", "e1000e", "52:54:00:9a:1b:02"}}, ""},
		"a host interface beside it":    {append(slices.Clone(ports), installerNIC{"usb0", "a2:b3:c4:d5:e6:f7", "cdc_ether", ""}), ""},
		"a virtual interface beside it": {append(slices.Clone(ports), installerNIC{"lo", "00:00:00:00:00:00", "", ""}), ""},
		"an extra reported port": {append(slices.Clone(ports), installerNIC{"eno3", "52:54:00:9a:1b:09", "e1000e", ""}),
			"this machine reports 1 hardware address(es) its declaration does not name"},
		"a declared port missing": {ports[:1], "this machine does not report 52:54:00:9a:1b:02, which its declaration requires"},
		"a declared port only as a host interface": {[]installerNIC{ports[0], {"usb0", "52:54:00:9a:1b:02", "cdc_ether", ""}},
			"this machine does not report 52:54:00:9a:1b:02, which its declaration requires"},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := runTargetProof(t, physicalInstallation(), installerHost{nics: want.nics, root: "/dev/sda"})
			if want.refusal == "" {
				if err != nil {
					t.Fatalf("the declared machine was refused: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, "Bootwright: "+want.refusal+"; refusing before any disk is touched.") {
				t.Fatalf("%s was not refused before storage: %v\n%s", name, err, output)
			}
			if strings.Contains(output, "52:54:00:9a:1b:09") {
				t.Fatalf("the refusal printed an undeclared address:\n%s", output)
			}
		})
	}
}

// A physical installation that declares no hardware address proves nothing,
// so its installer stops before storage rather than compare with nothing.
func TestThePhysicalTargetProofRefusesADeclarationWithNoAddress(t *testing.T) {
	input := physicalInstallation()
	input.ExpectedMACs = nil
	output, err := runTargetProof(t, input, installerHost{nics: []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}}, root: "/dev/sda"})
	if err == nil || !strings.Contains(output, "declares no hardware address to prove it by") {
		t.Fatalf("a declaration with no address reached storage: %v\n%s", err, output)
	}
}
