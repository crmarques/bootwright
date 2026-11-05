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
	// disk is the sysfs node of the resolved root disk, by path beneath it.
	disk map[string][]byte
	// prepare populates the block and proc trees beside the root disk, and
	// mocks are shell functions defined after the defaults, which they replace.
	prepare func(t *testing.T, blocks, proc string)
	mocks   string
	env     []string
	// include receives the storage include the %pre wrote, empty when none.
	include *string
}

// runTargetProof runs the rendered target-proof %pre against a synthetic
// host: its network interfaces and its root disk's identity attributes under a
// temporary sysfs, and readlink, the block-device test and lsblk answering for
// the one disk tree it holds, where /dev/sda is a whole disk and /dev/sda1 a
// partition of it.
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
	blocks := filepath.Join(sysfs, "..", "block")
	for name, content := range host.disk {
		attribute := filepath.Join(blocks, filepath.Base(host.root), name)
		if err := os.MkdirAll(filepath.Dir(attribute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(attribute, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	proc := filepath.Join(sysfs, "..", "proc")
	if host.prepare != nil {
		host.prepare(t, blocks, proc)
	}
	include := filepath.Join(sysfs, "..", "bootwright-disk.ks")
	script := strings.NewReplacer("/sys/class/net", sysfs, "/sys/class/block", blocks, "/sys/fs/btrfs", filepath.Join(sysfs, "..", "btrfs"), "/proc/", proc+"/",
		diskInclude, include).Replace(strings.Join(lines[start+1:end], "\n"))
	mocks := `
readlink() { case "${@: -1}" in /dev/*) ;; *) command readlink "$@"; return;; esac; if builtin [ -z "$resolved" ]; then return 1; fi; printf '%s\n' "$resolved"; }
[() { if builtin [ "$1 $2" = "! -b" ]; then case "$3" in /dev/sda|/dev/sda1) return 1;; *) return 0;; esac; fi; builtin [ "$@"; }
lsblk() { case "${@: -1}" in /dev/sda) echo disk;; /dev/sda1) echo part;; esac; }
`
	command := exec.Command("/bin/bash", "-c", mocks+host.mocks+script+"\necho 'reached storage'\n")
	command.Env = append(append(os.Environ(), "resolved="+host.root, "blocks="+blocks), host.env...)
	output, err := command.CombinedOutput()
	if err == nil && !strings.Contains(string(output), "reached storage") {
		t.Fatalf("the target proof exited cleanly without reaching storage:\n%s", output)
	}
	if err != nil && strings.Contains(string(output), "reached storage") {
		t.Fatalf("the target proof failed after reaching storage:\n%s", output)
	}
	written, readErr := os.ReadFile(include)
	if err != nil && readErr == nil {
		t.Fatalf("a target proof that stopped left a storage include:\n%s", written)
	}
	if host.include != nil {
		*host.include = string(written)
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

// vpdSerial is a SCSI VPD page 0x80 carrying serial, its length field set to
// the payload's length.
func vpdSerial(serial string) []byte {
	return append([]byte{0, 0x80, byte(len(serial) >> 8), byte(len(serial))}, serial...)
}

// The %pre compares the disk the declared path resolved to with the frozen
// WWN and serial number, read from sysfs as the physical erase worker reads
// them, and stops before storage on a mismatch and on any value it cannot
// read: a missing, empty, overlong or non-printable attribute, or a malformed
// VPD page.
func TestThePhysicalTargetProofComparesTheRootDiskWWNAndSerial(t *testing.T) {
	declared := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}}
	scsi := func(changes map[string][]byte) map[string][]byte {
		disk := map[string][]byte{"device/wwid": []byte("naa.5000c500a1b2c3d4\n"), "device/vpd_pg80": vpdSerial("  ZC1A2B3C")}
		for name, content := range changes {
			if content == nil {
				delete(disk, name)
				continue
			}
			disk[name] = content
		}
		return disk
	}
	const (
		wwnMismatch    = "reports a WWN other than 0x5000c500a1b2c3d4"
		wwnUnread      = "reports no readable WWN"
		serialMismatch = "reports a serial number other than ZC1A2B3C"
		serialUnread   = "reports no readable serial number"
	)
	for name, test := range map[string]struct {
		disk    map[string][]byte
		refusal string
	}{
		"a SCSI disk reporting both":         {scsi(nil), ""},
		"a disk with native attributes":      {map[string][]byte{"wwid": []byte("naa.5000c500a1b2c3d4\n"), "device/serial": []byte("ZC1A2B3C  \n")}, ""},
		"another WWN":                        {scsi(map[string][]byte{"device/wwid": []byte("naa.5000c500a1b2c3d5\n")}), wwnMismatch},
		"the WWN in another form":            {scsi(map[string][]byte{"device/wwid": []byte("0x5000c500a1b2c3d4\n")}), wwnMismatch},
		"the WWN with a longer extension":    {scsi(map[string][]byte{"device/wwid": []byte("naa.5000c500a1b2c3d40000000000000000\n")}), wwnMismatch},
		"no WWN attribute":                   {scsi(map[string][]byte{"device/wwid": nil}), wwnUnread},
		"an empty WWN":                       {scsi(map[string][]byte{"device/wwid": []byte(" \n")}), wwnUnread},
		"a WWN with a NUL byte":              {scsi(map[string][]byte{"device/wwid": []byte("naa.5000c500\x00a1b2c3d4\n")}), wwnUnread},
		"a WWN beyond its bound":             {scsi(map[string][]byte{"device/wwid": []byte("naa." + strings.Repeat("a", 260) + "\n")}), wwnUnread},
		"another serial number":              {scsi(map[string][]byte{"device/vpd_pg80": vpdSerial("ZC1A2B3D")}), serialMismatch},
		"another native serial number":       {scsi(map[string][]byte{"device/serial": []byte("ZC1A2B3\n")}), serialMismatch},
		"no serial attribute":                {scsi(map[string][]byte{"device/vpd_pg80": nil}), serialUnread},
		"a VPD page of another code":         {scsi(map[string][]byte{"device/vpd_pg80": append([]byte{0, 0x83, 0, 8}, "ZC1A2B3C"...)}), serialUnread},
		"a VPD page of another length":       {scsi(map[string][]byte{"device/vpd_pg80": append([]byte{0, 0x80, 0, 9}, "ZC1A2B3C"...)}), serialUnread},
		"a truncated VPD page":               {scsi(map[string][]byte{"device/vpd_pg80": {0, 0x80, 0}}), serialUnread},
		"a blank VPD serial":                 {scsi(map[string][]byte{"device/vpd_pg80": vpdSerial("    ")}), serialUnread},
		"a VPD serial with a control byte":   {scsi(map[string][]byte{"device/vpd_pg80": vpdSerial("ZC1A2B3C\x01")}), serialUnread},
		"a non-ASCII native serial number":   {scsi(map[string][]byte{"device/serial": []byte("ZC1A2B3C\xc3\xa9\n")}), serialUnread},
		"a native serial beyond its bound":   {scsi(map[string][]byte{"device/serial": []byte(strings.Repeat("Z", 257) + "\n")}), serialUnread},
		"a mismatched WWN before its serial": {scsi(map[string][]byte{"device/wwid": []byte("naa.5000c500a1b2c3d5\n"), "device/vpd_pg80": nil}), wwnMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			input := physicalInstallation()
			input.RootWWN, input.RootSerial = "0x5000c500a1b2c3d4", "ZC1A2B3C"
			output, err := runTargetProof(t, input, installerHost{nics: declared, root: "/dev/sda", disk: test.disk})
			if test.refusal == "" {
				if err != nil {
					t.Fatalf("the declared disk was refused: %v\n%s", err, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, "Bootwright: the declared root device /dev/disk/by-id/wwn-0x5000c500a1b2c3d4 "+test.refusal+"; refusing before any disk is touched.") {
				t.Fatalf("%s was not refused before storage: %v\n%s", name, err, output)
			}
		})
	}
}

// Each identity is compared only when it is frozen, so a Machine declaring
// one reads only that attribute, and one declaring neither renders no read.
func TestThePhysicalTargetProofReadsOnlyTheFrozenIdentities(t *testing.T) {
	declared := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}}
	wwnOnly, serialOnly := physicalInstallation(), physicalInstallation()
	wwnOnly.RootWWN, serialOnly.RootSerial = "0x5000c500a1b2c3d4", "ZC1A2B3C"
	if output, err := runTargetProof(t, wwnOnly, installerHost{nics: declared, root: "/dev/sda",
		disk: map[string][]byte{"device/wwid": []byte("naa.5000c500a1b2c3d4\n")}}); err != nil {
		t.Fatalf("a WWN alone was refused without a serial attribute: %v\n%s", err, output)
	}
	if output, err := runTargetProof(t, serialOnly, installerHost{nics: declared, root: "/dev/sda",
		disk: map[string][]byte{"device/serial": []byte("ZC1A2B3C\n")}}); err != nil {
		t.Fatalf("a serial number alone was refused without a WWN attribute: %v\n%s", err, output)
	}
	rendered, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "bootwright_identity") || strings.Contains(rendered, "/sys/class/block") {
		t.Fatalf("a Machine declaring neither identity reads one:\n%s", rendered)
	}
}

// The renderer refuses a frozen identity its %pre could never match, or carry,
// rather than render an installation that always stops.
func TestTheKickstartRefusesARootIdentityItCannotCompare(t *testing.T) {
	for name, change := range map[string]func(*Installation){
		"a WWN naming another disk": func(i *Installation) { i.RootWWN = "0x5000c500a1b2c3d5" },
		"a WWN without its 0x":      func(i *Installation) { i.RootWWN = "5000c500a1b2c3d4" },
		"a serial with a quote":     func(i *Installation) { i.RootSerial = "ZC1A'2B3C" },
		"a serial with a space":     func(i *Installation) { i.RootSerial = "ZC1A 2B3C" },
		"an identity on a guest disk": func(i *Installation) {
			i.Physical, i.RootDevice, i.ExpectedMACs, i.RootSerial = false, "/dev/vda", nil, "ZC1A2B3C"
		},
		"a size on a guest disk": func(i *Installation) {
			i.Physical, i.RootDevice, i.ExpectedMACs, i.RootSizeBytes = false, "/dev/vda", nil, 480103981056
		},
		"a serial without its size": func(i *Installation) { i.RootDevice, i.RootSerial = "", "ZC1A2B3C" },
		"an unaligned size":         func(i *Installation) { i.RootDevice, i.RootSerial, i.RootSizeBytes = "", "ZC1A2B3C", 480103981057 },
		"a size past 64 TiB":        func(i *Installation) { i.RootSizeBytes = 64<<40 + 4096 },
		"a wwn without its path": func(i *Installation) {
			i.RootDevice, i.RootSerial, i.RootSizeBytes, i.RootWWN = "", "ZC1A2B3C", 480103981056, "0x5000c500a1b2c3d4"
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := physicalInstallation()
			change(&input)
			if _, err := RenderKickstart(input); err == nil {
				t.Fatal("the renderer carried a root identity its pre-installation script cannot compare")
			}
		})
	}
}

// serialDisk is one block device under the synthetic /sys/class/block: its
// serial number as a native attribute, or none, its removable and read-only
// flags, whether it is a partition and the content of its hidden attribute, or
// none (a hidden NVMe path node reads 1).
type serialDisk struct {
	name, serial, removable, ro string
	partition                   bool
	hidden                      string
}

func serialHost(t *testing.T, disks []serialDisk, size string) installerHost {
	t.Helper()
	declared := []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}}
	return installerHost{nics: declared, root: "/dev/unused", prepare: func(t *testing.T, blocks, _ string) {
		for _, disk := range disks {
			node := filepath.Join(blocks, disk.name)
			if err := os.MkdirAll(filepath.Join(node, "device"), 0o755); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{"removable": disk.removable + "\n", "ro": disk.ro + "\n"}
			if disk.serial != "" {
				files["device/serial"] = disk.serial + "\n"
			}
			if disk.partition {
				files["partition"] = "1\n"
			}
			if disk.hidden != "" {
				files["hidden"] = disk.hidden + "\n"
			}
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(node, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}, mocks: `
[() { if builtin [ "$1 $2" = "! -b" ]; then case "$3" in /dev/sd*|/dev/nvme*|/dev/vd*) return 1;; *) return 0;; esac; fi; builtin [ "$@"; }
lsblk() { case "${@: -1}" in /dev/sd*|/dev/nvme*|/dev/vd*) echo disk;; esac; }
blockdev() { printf '%s\n' "$size"; }
`, env: []string{"size=" + size}}
}

// A root disk declared by serial number and size is resolved on the booted
// machine to exactly one fixed, writable sd or nvme whole disk of exactly that
// size, and only then does the %pre write the include naming its kernel name;
// none, two, a removable, read-only or virtual disk, and another size each
// stop it before any storage directive with no include written. A partition
// and a device whose serial cannot be read are never candidates.
func TestThePhysicalTargetProofResolvesASerialRootToExactlyOneDisk(t *testing.T) {
	const serial, size = "ZC1A2B3C", "480103981056"
	input := physicalInstallation()
	input.RootDevice, input.RootSerial, input.RootSizeBytes = "", serial, 480103981056
	fixed := func(name, reported string) serialDisk { return serialDisk{name, reported, "0", "0", false, ""} }
	const (
		absent    = "no whole disk here reports the declared root serial number " + serial
		ambiguous = "2 disks here report the declared root serial number " + serial
		kind      = "the disk reporting the declared root serial number " + serial + " is not a fixed, writable sd or nvme disk"
		sized     = "the disk reporting the declared root serial number " + serial + " reports a size other than " + size + " bytes"
	)
	for name, test := range map[string]struct {
		disks      []serialDisk
		size, disk string
		refusal    string
	}{
		"one SCSI disk": {[]serialDisk{fixed("sda", serial), fixed("sdb", "OTHER-01")}, size, "sda", ""},
		"one NVMe disk": {[]serialDisk{fixed("nvme0n1", serial), fixed("sda", "OTHER-01")}, size, "nvme0n1", ""},
		"a partition and an unreadable sibling beside it": {[]serialDisk{fixed("sda", serial), {"sda1", serial, "0", "0", true, ""}, fixed("sdb", ""), fixed("loop0", "")}, size, "sda", ""},
		"a dual-port NVMe disk and its hidden path node":  {[]serialDisk{fixed("nvme0n1", serial), {"nvme0c0n1", serial, "0", "0", false, "1"}}, size, "nvme0n1", ""},
		"a hidden path node of an absent head node":       {[]serialDisk{{"nvme0c0n1", serial, "0", "0", false, "1"}}, size, "", absent},
		"a hidden path node beside two visible disks":     {[]serialDisk{fixed("nvme0n1", serial), fixed("nvme1n1", serial), {"nvme0c0n1", serial, "0", "0", false, "1"}}, size, "", ambiguous},
		"a second disk that is not hidden":                {[]serialDisk{fixed("nvme0n1", serial), {"nvme0c0n1", serial, "0", "0", false, ""}}, size, "", ambiguous},
		"a second disk whose hidden flag reads 0":         {[]serialDisk{fixed("nvme0n1", serial), {"nvme0c0n1", serial, "0", "0", false, "0"}}, size, "", ambiguous},
		"a second disk whose hidden flag is not 0 or 1":   {[]serialDisk{fixed("nvme0n1", serial), {"nvme0c0n1", serial, "0", "0", false, "yes"}}, size, "", ambiguous},
		"no disk":                               {[]serialDisk{fixed("sda", "OTHER-01")}, size, "", absent},
		"two paths of a multipath disk":         {[]serialDisk{fixed("sda", serial), fixed("sdb", serial)}, size, "", ambiguous},
		"two namespaces of one NVMe controller": {[]serialDisk{fixed("nvme0n1", serial), fixed("nvme0n2", serial)}, size, "", ambiguous},
		"a removable disk":                      {[]serialDisk{{"sda", serial, "1", "0", false, ""}}, size, "", kind},
		"a read-only disk":                      {[]serialDisk{{"sda", serial, "0", "1", false, ""}}, size, "", kind},
		"a virtual disk":                        {[]serialDisk{fixed("vda", serial)}, size, "", kind},
		"another size":                          {[]serialDisk{fixed("sda", serial)}, "480103985152", "", sized},
	} {
		t.Run(name, func(t *testing.T) {
			var include string
			host := serialHost(t, test.disks, test.size)
			host.include = &include
			output, err := runTargetProof(t, input, host)
			if test.refusal == "" {
				want := "ignoredisk --only-use=" + test.disk + "\nclearpart --all --initlabel --drives=" + test.disk + "\n"
				if err != nil || include != want {
					t.Fatalf("the declared disk was not resolved: %v, include %q\n%s", err, include, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, "Bootwright: "+test.refusal+"; refusing before any disk is touched.") || include != "" {
				t.Fatalf("%s was not refused before storage: %v, include %q\n%s", name, err, include, output)
			}
		})
	}
}

// Whatever names its root disk, a physical Kickstart partitions only the disk
// its %pre proved: no clearpart without --drives, and a root declared by
// serial number reaches storage only through the include the %pre writes.
func TestAPhysicalKickstartPartitionsOnlyTheDiskItsPreProved(t *testing.T) {
	bySerial := physicalInstallation()
	bySerial.RootDevice, bySerial.RootSerial, bySerial.RootSizeBytes = "", "ZC1A2B3C", 480103981056
	encrypted := bySerial
	encrypted.DiskEncryption = &KickstartEncryption{}
	cleared := bySerial
	cleared.ClearTargets = []ClearTarget{{Intervals: MetadataClearRegions(107374182400), SerialNumber: "DATA-01", SizeBytes: 107374182400}}
	for name, input := range map[string]Installation{"by path": physicalInstallation(), "by serial": bySerial, "encrypted by serial": encrypted, "clearing by serial": cleared} {
		rendered, err := RenderKickstart(input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, line := range strings.Split(rendered, "\n") {
			if strings.HasPrefix(line, "clearpart") && !strings.Contains(line, "--drives=") {
				t.Fatalf("%s renders %q", name, line)
			}
		}
		if input.RootDevice == "" && (!strings.Contains(rendered, "\n%include "+diskInclude+"\n") || strings.Contains(rendered, "\nclearpart ") || strings.Contains(rendered, "\nignoredisk ")) {
			t.Fatalf("%s does not partition through the %%pre's include:\n%s", name, rendered)
		}
	}
}
