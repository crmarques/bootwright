package installation

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func clearTarget(hex, serial string, size int64) ClearTarget {
	return ClearTarget{DeviceName: "/dev/disk/by-id/wwn-0x" + hex, Intervals: MetadataClearRegions(size), SerialNumber: serial, SizeBytes: size, WWN: "0x" + hex}
}

func TestTheRequestFreezesClearTargetsInDeviceNameOrder(t *testing.T) {
	target := func(hex, wwn, serial string) api.Value {
		return api.MapValue(api.FieldValue{Name: "deviceName", Value: api.StringValue("/dev/disk/by-id/wwn-0x" + hex)},
			api.FieldValue{Name: "wwn", Value: api.StringValue(wwn)}, api.FieldValue{Name: "serialNumber", Value: api.StringValue(serial)},
			api.FieldValue{Name: "sizeBytes", Value: api.IntegerValue("2000398934016")})
	}
	machine := api.NewObject(api.Machine, "metal-01", api.MapValue(), api.MapValue()).WithSpec(api.MapValue().WithPath(api.MapValue(api.FieldValue{Name: "targets",
		Value: api.ListValue(target("5000000000000002", "0X5000000000000002", "B"), target("5000000000000001", "5000000000000001", "A"))}), "hardware", "clearOnInstall"))
	got := clearTargetsFor(machine)
	want := []ClearTarget{clearTarget("5000000000000001", "A", 2000398934016), clearTarget("5000000000000002", "B", 2000398934016)}
	if len(got) != 2 || !slices.EqualFunc(got, want, func(a, b ClearTarget) bool {
		return a.DeviceName == b.DeviceName && a.WWN == b.WWN && a.SerialNumber == b.SerialNumber && a.SizeBytes == b.SizeBytes && slices.Equal(a.Intervals, b.Intervals)
	}) {
		t.Fatalf("froze %+v, want %+v", got, want)
	}
}

func clearInstallation(targets ...ClearTarget) Installation {
	input := physicalInstallation()
	input.ClearTargets = targets
	return input
}

// Without a target the Kickstart renders no clear, and with one every target
// is proved before the first release or write, each interval is a literal,
// and nothing writes before the last proof.
func TestTheClearRendersOnlyDeclaredTargetsAfterEveryProof(t *testing.T) {
	plain, err := RenderKickstart(physicalInstallation())
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"bootwright_clear", "dmsetup", "dd if=", "blockdev", "udevadm", "bootwright_identity"} {
		if strings.Contains(plain, absent) {
			t.Fatalf("a Machine declaring no clear target renders %q:\n%s", absent, plain)
		}
	}
	first, second := clearTarget("5000000000000001", "CEPH-01", 2000398934016), clearTarget("5000000000000002", "CEPH-02", 16000900661248)
	rendered, err := RenderKickstart(clearInstallation(first, second))
	if err != nil {
		t.Fatal(err)
	}
	effects := len(rendered)
	for _, effect := range []string{"dmsetup remove", "dd if=/dev/zero", "blockdev --rereadpt", "udevadm settle"} {
		if at := strings.Index(rendered, effect); at >= 0 && at < effects {
			effects = at
		}
	}
	for _, target := range []ClearTarget{first, second} {
		for _, refusal := range []string{"is not a block device here", "is not a whole disk", "is the root disk", "reports no readable WWN",
			"reports a WWN other than " + target.WWN, "reports no readable serial number", "reports a serial number other than " + target.SerialNumber,
			"reports no readable size", "reports a size other than " + strconv.FormatInt(target.SizeBytes, 10) + " bytes"} {
			line := "Bootwright: the declared clear target " + target.DeviceName + " " + refusal + "; refusing before any disk is touched."
			at := strings.Index(rendered, line)
			if at < 0 || at > effects {
				t.Fatalf("the proof %q is missing or follows an effect:\n%s", line, rendered)
			}
		}
		for _, interval := range target.Intervals {
			offset, length := strconv.FormatInt(interval.Offset, 10), strconv.FormatInt(interval.Length, 10)
			requireLine(t, rendered, `if ! dd if=/dev/zero of="${bootwright_clear_`+strconv.Itoa(slices.IndexFunc([]ClearTarget{first, second}, func(c ClearTarget) bool { return c.DeviceName == target.DeviceName }))+`}" bs=1M seek=`+offset+` count=`+length+` oflag=seek_bytes,direct iflag=count_bytes conv=notrunc,fsync status=none; then`)
		}
	}
	if strings.Index(rendered, "%pre --erroronfail") > effects || strings.Index(rendered, "ignoredisk") < strings.LastIndex(rendered, "udevadm settle") {
		t.Fatalf("the clear is not inside the target proof, before storage:\n%s", rendered)
	}
	if !strings.Contains(rendered, "seek=16000766443520 count=134217728") || !strings.Contains(rendered, "seek=2000264716288 count=134217728") {
		t.Fatal("a tail window is not a literal offset")
	}
	requireLine(t, rendered, "ignoredisk --only-use=disk/by-id/wwn-0x5000c500a1b2c3d4")
	requireLine(t, rendered, "clearpart --all --initlabel --drives=disk/by-id/wwn-0x5000c500a1b2c3d4")
}

func TestTheKickstartRefusesAClearTargetItCannotProve(t *testing.T) {
	good := clearTarget("5000000000000001", "CEPH-01", 8192)
	for name, targets := range map[string][]ClearTarget{
		"a kernel name":             {{DeviceName: "/dev/sdb", WWN: good.WWN, SerialNumber: good.SerialNumber, SizeBytes: 8192, Intervals: good.Intervals}},
		"a WWN naming another disk": {{DeviceName: good.DeviceName, WWN: "0x5000000000000009", SerialNumber: good.SerialNumber, SizeBytes: 8192, Intervals: good.Intervals}},
		"a serial with a backquote": {{DeviceName: good.DeviceName, WWN: good.WWN, SerialNumber: "CEPH`01", SizeBytes: 8192, Intervals: good.Intervals}},
		"an unaligned size":         {{DeviceName: good.DeviceName, WWN: good.WWN, SerialNumber: good.SerialNumber, SizeBytes: 8193, Intervals: MetadataClearRegions(8193)}},
		"intervals of another size": {{DeviceName: good.DeviceName, WWN: good.WWN, SerialNumber: good.SerialNumber, SizeBytes: 8192, Intervals: MetadataClearRegions(4096)}},
		"the root disk":             {clearTarget("5000c500a1b2c3d4", "CEPH-01", 8192)},
		"a duplicate serial":        {good, clearTarget("5000000000000002", "CEPH-01", 8192)},
		"out of order":              {clearTarget("5000000000000002", "CEPH-02", 8192), good},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := RenderKickstart(clearInstallation(targets...)); err == nil {
				t.Fatal("the renderer carried a clear target its pre-installation script cannot prove")
			}
		})
	}
	guest := clearInstallation(good)
	guest.Physical, guest.RootDevice, guest.ExpectedMACs = false, "/dev/vda", nil
	if _, err := RenderKickstart(guest); err == nil {
		t.Fatal("a guest installation carried a clear target")
	}
	sixteen := []ClearTarget{}
	for index := range 16 {
		sixteen = append(sixteen, clearTarget("50000000000000"+strconv.FormatInt(int64(index+16), 16), "CEPH-"+strconv.Itoa(index+10), 8192))
	}
	if _, err := RenderKickstart(clearInstallation(sixteen...)); err == nil {
		t.Fatal("the renderer carried sixteen clear targets")
	}
}

func TestEveryClearTargetIsAPlannedImpact(t *testing.T) {
	request := Request{Identity: Identity{Object: "metal-01"}, Target: Target{Physical: true, Hardware: &Hardware{ClearOnInstall: []ClearTarget{
		clearTarget("5000000000000001", "A", 8192), clearTarget("5000000000000002", "B", 8192)}}}}
	got := impacts(reconciliation.Apply, request)
	for _, want := range []string{"clear-disk metal-01 0x5000000000000001", "clear-disk metal-01 0x5000000000000002"} {
		if !slices.Contains(got, want) {
			t.Fatalf("the apply impacts %v omit %s", got, want)
		}
	}
	for _, impact := range impacts(reconciliation.Destroy, request) {
		if strings.HasPrefix(impact, "clear-disk ") {
			t.Fatalf("the removal undoes a clear: %v", impact)
		}
	}
}

// clearDisk is one disk of the synthetic installer host: its kernel name, the
// by-id path that resolves to it, its sysfs identity, its size and what is
// stacked on it.
type clearDisk struct {
	name, byID, wwid, serial string
	size                     int64
}

// clearHost builds the block tree, the proc tree and the mocks the clear runs
// against: block devices are /dev/sd? disks and their partitions, every
// command with an effect logs to $log, and a dd read answers zeros unless
// $readback asks otherwise.
func clearHost(t *testing.T, disks []clearDisk, stack func(blocks, proc string)) (installerHost, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "log")
	resolve := []string{}
	sizes := []string{}
	for _, disk := range disks {
		resolve = append(resolve, disk.byID+") echo /dev/"+disk.name+";;")
		sizes = append(sizes, "/dev/"+disk.name+") echo "+strconv.FormatInt(disk.size, 10)+";;")
	}
	mocks := `
readlink() { case "${@: -1}" in ` + strings.Join(resolve, " ") + ` /dev/disk/by-id/wwn-0x5000c500a1b2c3d4) echo /dev/sda;; /dev/*) printf '%s\n' "${@: -1}";; *) command readlink "$@";; esac; }
[() { if builtin [ "$1 $2" = "! -b" ]; then case "$3" in /dev/sd[a-z]|/dev/sd[a-z][0-9]) return 1;; *) return 0;; esac; fi; builtin [ "$@"; }
lsblk() { case "${@: -1}" in /dev/sd[a-z]) echo disk;; *) echo part;; esac; }
blockdev() { case "$1" in --getsize64) case "$2" in ` + strings.Join(sizes, " ") + ` *) return 1;; esac;; --rereadpt) echo "rereadpt $2" >> "$log";; *) return 1;; esac; }
udevadm() { echo "udevadm $*" >> "$log"; if builtin [ -e "$blocks/reappear" ] && grep -q '^remove ' "$log"; then : > "$blocks/sdb/holders/dm-0"; fi; }
dmsetup() {
local command="$1" major='' minor='' dm found=''
shift
while builtin [ "$#" -gt 0 ]; do case "$1" in -j) major=$2; shift;; -m) minor=$2; shift;; -c|--noheadings) ;; -o) shift;; *) return 1;; esac; shift; done
for dm in "$blocks"/dm-*; do if builtin [ "$(cat "$dm/dev")" = "$major:$minor" ]; then found=$dm; fi; done
builtin [ -n "$found" ] || return 1
local name; name=$(cat "$found/dm/name")
case "$command" in
info) builtin [ -e "$blocks/open-$name" ] && cat "$blocks/open-$name" || printf '\t0\n';;
remove) echo "remove $name" >> "$log"; builtin [ -e "$blocks/fail-remove" ] && return 1
for slave in "$found"/slaves/*; do rm -f "$blocks/$(basename "$slave")/holders/$(basename "$found")"; done
if builtin [ -e "$blocks/rename-after-remove" ]; then for dm in "$blocks"/dm-*; do echo renamed > "$dm/dm/name"; done; fi;;
*) return 1;;
esac; }
dd() {
local arg input='' output='' count=0 seek=0 skip=0
for arg in "$@"; do case "$arg" in if=*) input=${arg#if=};; of=*) output=${arg#of=};; count=*) count=${arg#count=};; seek=*) seek=${arg#seek=};; skip=*) skip=${arg#skip=};; esac; done
if builtin [ -n "$output" ]; then echo "write $output $seek $count" >> "$log"; builtin [ -e "$blocks/fail-write" ] && return 1; return 0; fi
echo "read $input $skip $count" >> "$log"
case "${readback:-zero}" in zero) head -c "$count" /dev/zero;; short) head -c $((count - 4096)) /dev/zero;; nonzero) head -c $((count - 1)) /dev/zero; printf x;; esac
}
`
	host := installerHost{
		nics: []installerNIC{{"eno1", "52:54:00:9a:1b:01", "e1000e", ""}, {"eno2", "52:54:00:9a:1b:02", "e1000e", ""}},
		root: "/dev/sda", mocks: mocks, env: []string{"log=" + log},
		prepare: func(t *testing.T, blocks, proc string) {
			write := func(path, content string) {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for index, disk := range append([]clearDisk{{name: "sda"}}, disks...) {
				write(filepath.Join(blocks, disk.name, "dev"), "8:"+strconv.Itoa(16*index)+"\n")
				write(filepath.Join(blocks, disk.name, "device", "wwid"), disk.wwid+"\n")
				write(filepath.Join(blocks, disk.name, "device", "serial"), disk.serial+"\n")
				if err := os.MkdirAll(filepath.Join(blocks, disk.name, "holders"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(proc, "1", "mountinfo"), "22 1 253:9 / / rw - xfs /dev/loop0 rw\n")
			write(filepath.Join(proc, "swaps"), "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n")
			if stack != nil {
				stack(blocks, proc)
			}
		},
	}
	return host, log
}

// stackDevice places a device under the block tree: a partition of a disk, or
// a device-mapper or other holder over its slaves, each linked as both see it.
func stackDevice(t *testing.T, blocks, name, dev, dmName string, partitionOf string, slaves ...string) {
	t.Helper()
	directory := filepath.Join(blocks, name)
	for _, made := range []error{os.MkdirAll(filepath.Join(directory, "holders"), 0o755), os.MkdirAll(filepath.Join(directory, "slaves"), 0o755),
		os.WriteFile(filepath.Join(directory, "dev"), []byte(dev+"\n"), 0o644)} {
		if made != nil {
			t.Fatal(made)
		}
	}
	if partitionOf != "" {
		inner := filepath.Join(blocks, partitionOf, name)
		if err := os.MkdirAll(inner, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(inner, "partition"), []byte("1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if dmName != "" {
		if err := os.MkdirAll(filepath.Join(directory, "dm"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "dm", "name"), []byte(dmName+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, slave := range slaves {
		for _, link := range []string{filepath.Join(directory, "slaves", slave), filepath.Join(blocks, slave, "holders", name)} {
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(link, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func readLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

var clearDisks = []clearDisk{
	{name: "sdb", byID: "/dev/disk/by-id/wwn-0x5000000000000001", wwid: "naa.5000000000000001", serial: "CEPH-01", size: 8192},
	{name: "sdc", byID: "/dev/disk/by-id/wwn-0x5000000000000002", wwid: "naa.5000000000000002", serial: "CEPH-02", size: 2*MetadataClearWindow + 4096},
}

func clearInput() Installation {
	return clearInstallation(clearTarget("5000000000000001", "CEPH-01", 8192), clearTarget("5000000000000002", "CEPH-02", 2*MetadataClearWindow+4096))
}

// An auto-activated ceph-volume LV with a LUKS mapping over it and an LV over
// a partition are released deepest first, and only then is every interval
// written and read back, before the partition tables are reread.
func TestTheClearReleasesItsOwnStackThenZeroesAndReadsBack(t *testing.T) {
	host, log := clearHost(t, clearDisks, func(blocks, proc string) {
		stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
		stackDevice(t, blocks, "dm-1", "253:1", "CRYPT-a", "", "dm-0")
		stackDevice(t, blocks, "sdc1", "8:33", "", "sdc")
		stackDevice(t, blocks, "dm-2", "253:2", "ceph--c-osd--block--d", "", "sdc1")
	})
	output, err := runTargetProof(t, clearInput(), host)
	if err != nil {
		t.Fatalf("the declared stack was refused: %v\n%s", err, output)
	}
	want := []string{
		"udevadm settle", "remove CRYPT-a", "remove ceph--a-osd--block--b", "remove ceph--c-osd--block--d", "udevadm settle",
		"write /dev/sdb 0 8192", "read /dev/sdb 0 8192", "read /dev/sdb 0 8192", "rereadpt /dev/sdb",
		"write /dev/sdc 0 134217728", "read /dev/sdc 0 134217728", "read /dev/sdc 0 134217728",
		"write /dev/sdc 134221824 134217728", "read /dev/sdc 134221824 134217728", "read /dev/sdc 134221824 134217728", "rereadpt /dev/sdc",
		"udevadm settle",
	}
	if got := readLog(t, log); !slices.Equal(got, want) {
		t.Fatalf("the clear ran %q, want %q", got, want)
	}
}

func TestTheClearRefusesBeforeAnyWriteUnlessEveryTargetAndHolderIsProved(t *testing.T) {
	for name, test := range map[string]struct {
		disks   []clearDisk
		stack   func(t *testing.T, blocks, proc string)
		refusal string
	}{
		"another WWN": {append([]clearDisk{clearDisks[0]}, clearDisk{"sdc", clearDisks[1].byID, "naa.5000000000000009", "CEPH-02", clearDisks[1].size}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 reports a WWN other than 0x5000000000000002"},
		"another serial": {append([]clearDisk{clearDisks[0]}, clearDisk{"sdc", clearDisks[1].byID, clearDisks[1].wwid, "CEPH-09", clearDisks[1].size}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 reports a serial number other than CEPH-02"},
		"another size": {append([]clearDisk{clearDisks[0]}, clearDisk{"sdc", clearDisks[1].byID, clearDisks[1].wwid, "CEPH-02", 4096}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 reports a size other than 268439552 bytes"},
		"no readable WWN": {append([]clearDisk{clearDisks[0]}, clearDisk{"sdc", clearDisks[1].byID, " ", "CEPH-02", clearDisks[1].size}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 reports no readable WWN"},
		"the root disk": {append([]clearDisk{clearDisks[0]}, clearDisk{"sda", clearDisks[1].byID, clearDisks[1].wwid, "CEPH-02", clearDisks[1].size}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 is the root disk"},
		"a partition": {append([]clearDisk{clearDisks[0]}, clearDisk{"sdc1", clearDisks[1].byID, clearDisks[1].wwid, "CEPH-02", clearDisks[1].size}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 is not a whole disk"},
		"not a block device": {append([]clearDisk{clearDisks[0]}, clearDisk{"nvme0n1", clearDisks[1].byID, clearDisks[1].wwid, "CEPH-02", clearDisks[1].size}), nil,
			"the declared clear target /dev/disk/by-id/wwn-0x5000000000000002 is not a block device here"},
		"a software RAID holder": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "md127", "9:127", "", "", "sdb", "sdc")
		}, "sdb, a declared clear target or a partition of one, is held by a device other than a device-mapper device of the declared clear targets alone"},
		"a device-mapper device over another disk": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "sdd", "8:48", "", "")
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb", "sdd")
		}, "sdb, a declared clear target or a partition of one, is held by a device other than a device-mapper device of the declared clear targets alone"},
		"a mapping over another disk's mapping": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "sdd", "8:48", "", "")
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
			stackDevice(t, blocks, "dm-5", "253:5", "foreign", "", "sdd")
			stackDevice(t, blocks, "dm-1", "253:1", "spanning", "", "dm-0", "dm-5")
		}, "sdb, a declared clear target or a partition of one, is held by a device other than a device-mapper device of the declared clear targets alone"},
		"a mounted mapping": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
			if err := os.WriteFile(filepath.Join(proc, "1", "mountinfo"), []byte("22 1 253:9 / / rw - xfs /dev/loop0 rw\n30 22 253:0 / /mnt rw - xfs /dev/dm-0 rw\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "dm-0, on a declared clear target, is mounted or cannot be read"},
		"a mounted partition": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "sdc1", "8:33", "", "sdc")
			if err := os.MkdirAll(filepath.Join(proc, "77"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(proc, "77", "mountinfo"), []byte("30 22 8:33 / /mnt rw - xfs /dev/sdc1 rw\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "sdc1, on a declared clear target, is mounted or cannot be read"},
		"a partition mounted with an anonymous device number": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "sdc1", "8:33", "", "sdc")
			if err := os.WriteFile(filepath.Join(proc, "1", "mountinfo"), []byte("22 1 253:9 / / rw - xfs /dev/loop0 rw\n31 22 0:45 / /data rw shared:9 - btrfs /dev/sdc1 rw,space_cache\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "sdc1, on a declared clear target, is mounted or cannot be read"},
		"a partition mounted by a by-id path": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "sdc1", "8:33", "", "sdc")
			if err := os.WriteFile(filepath.Join(proc, "1", "mountinfo"), []byte("22 1 253:9 / / rw - xfs /dev/loop0 rw\n31 22 0:45 / /data rw - btrfs /dev/disk/by-id/wwn-0x5000000000000002 rw\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "sdc, on a declared clear target, is mounted or cannot be read"},
		"a member of a btrfs filesystem": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "sdc1", "8:33", "", "sdc")
			if err := os.MkdirAll(filepath.Join(blocks, "..", "btrfs", "0f1e", "devices", "sdc1"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "sdc1, on a declared clear target, is a device of a mounted btrfs filesystem"},
		"no mount table": {clearDisks, func(t *testing.T, blocks, proc string) {
			if err := os.Remove(filepath.Join(proc, "1", "mountinfo")); err != nil {
				t.Fatal(err)
			}
		}, "the mount table cannot be read"},
		"swap on a mapping": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "swapper", "", "sdb")
			if err := os.WriteFile(filepath.Join(proc, "swaps"), []byte("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/dev/dm-0                               partition\t8388604\t\t0\t\t-2\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "dm-0, on a declared clear target, is active swap"},
		"no swap table": {clearDisks, func(t *testing.T, blocks, proc string) {
			if err := os.Remove(filepath.Join(proc, "swaps")); err != nil {
				t.Fatal(err)
			}
		}, "the swap table cannot be read"},
		"an open mapping": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
			if err := os.WriteFile(filepath.Join(blocks, "open-ceph--a-osd--block--b"), []byte("    1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "dm-0, on a declared clear target, is open"},
		"a failed release": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
			if err := os.WriteFile(filepath.Join(blocks, "fail-remove"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "dm-0, on a declared clear target, cannot be released"},
		"a mapping name it cannot pass": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "-f", "", "sdb")
		}, "dm-0, on a declared clear target, has no device number and device-mapper name it can be released by"},
		"a mapping renamed before its release": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
			stackDevice(t, blocks, "dm-1", "253:1", "CRYPT-a", "", "dm-0")
			if err := os.WriteFile(filepath.Join(blocks, "rename-after-remove"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "dm-0, on a declared clear target, changed its device number or device-mapper name before its release"},
		"a mapping that reappears after its release": {clearDisks, func(t *testing.T, blocks, proc string) {
			stackDevice(t, blocks, "dm-0", "253:0", "ceph--a-osd--block--b", "", "sdb")
			if err := os.WriteFile(filepath.Join(blocks, "reappear"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}, "sdb, on a declared clear target, is still held after its release"},
	} {
		t.Run(name, func(t *testing.T) {
			var stack func(blocks, proc string)
			if test.stack != nil {
				stack = func(blocks, proc string) { test.stack(t, blocks, proc) }
			}
			host, log := clearHost(t, test.disks, stack)
			output, err := runTargetProof(t, clearInput(), host)
			if err == nil || !strings.Contains(output, "Bootwright: "+test.refusal+"; refusing before any disk is touched.") {
				t.Fatalf("%s was not refused before any write: %v\n%s", name, err, output)
			}
			entries := readLog(t, log)
			if strings.HasPrefix(test.refusal, "the declared clear target ") && len(entries) != 0 {
				t.Fatalf("%s ran %q before every target was proved", name, entries)
			}
			for _, entry := range entries {
				if !strings.HasPrefix(entry, "remove ") && entry != "udevadm settle" {
					t.Fatalf("%s ran %q before refusing", name, entry)
				}
			}
		})
	}
}

func TestTheClearRefusesAnIntervalThatDoesNotReadBackAsZeros(t *testing.T) {
	for _, readback := range []string{"short", "nonzero"} {
		t.Run(readback, func(t *testing.T) {
			host, log := clearHost(t, clearDisks, nil)
			host.env = append(host.env, "readback="+readback)
			output, err := runTargetProof(t, clearInput(), host)
			if err == nil || !strings.Contains(output, "Bootwright: the declared clear target /dev/disk/by-id/wwn-0x5000000000000001 does not read back as zeros at byte 0; refusing before the installer touches storage.") {
				t.Fatalf("a %s readback was not refused: %v\n%s", readback, err, output)
			}
			if got := readLog(t, log); slices.Contains(got, "rereadpt /dev/sdb") || slices.Contains(got, "write /dev/sdc 0 134217728") {
				t.Fatalf("the clear went on after a failed readback: %q", got)
			}
		})
	}
	host, log := clearHost(t, clearDisks, func(blocks, proc string) {
		if err := os.WriteFile(filepath.Join(blocks, "fail-write"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	})
	output, err := runTargetProof(t, clearInput(), host)
	if err == nil || !strings.Contains(output, "the declared clear target /dev/disk/by-id/wwn-0x5000000000000001 cannot be cleared at byte 0; refusing before the installer touches storage.") {
		t.Fatalf("a failed write was not refused: %v\n%s", err, output)
	}
	if got := readLog(t, log); !slices.Equal(got, []string{"udevadm settle", "udevadm settle", "write /dev/sdb 0 8192"}) {
		t.Fatalf("the clear went on after a failed write: %q", got)
	}
}

// A Machine's declared targets reach the frozen request, its Kickstart and
// its plan's impacts, and the apply that clears them consumes data-loss.
func TestADeclaredClearReachesTheRequestKickstartAndPlan(t *testing.T) {
	catalog := physicalVariantCatalog(encryptingProfile(), "ecdsaP256")
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "metal-01" {
			target := api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000000000000001"), text("wwn", "0x5000000000000001"),
				text("serialNumber", "SYNTHETIC-CEPH-01"), field("sizeBytes", api.IntegerValue("2000398934016")))
			objects[index] = object.WithSpec(object.Spec().WithPath(api.MapValue(field("targets", api.ListValue(target))), "hardware", "clearOnInstall"))
		}
	}
	catalog = api.NewCatalog(objects)
	request, _ := onlyRequest(t, catalog)
	if clear := request.Target.Hardware.ClearOnInstall; len(clear) != 1 || clear[0].WWN != "0x5000000000000001" || len(clear[0].Intervals) != 6 {
		t.Fatalf("the request froze %+v", clear)
	}
	if !strings.Contains(request.Kickstart, "Bootwright: the declared clear target /dev/disk/by-id/wwn-0x5000000000000001 reports a WWN other than 0x5000000000000001") ||
		!strings.Contains(request.Kickstart, "seek=2000264716288 count=134217728") {
		t.Fatal("the Kickstart does not prove and clear the declared target")
	}
	input := planInput(reconciliation.Apply)
	input.State = compilation.NewState(catalog, catalog, nil)
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatal("physical plan", err)
	}
	if !slices.Contains(plan.Definitions[0].Impacts, "clear-disk metal-01 0x5000000000000001") || !slices.Contains(plan.Definitions[0].Consumes, reconciliation.AuthorizationDataLoss) {
		t.Fatalf("the plan shows %v consuming %v", plan.Definitions[0].Impacts, plan.Definitions[0].Consumes)
	}
}

// serialClearTarget is a clear target declared by serial number and size
// alone, which the %pre resolves on the booted machine.
func serialClearTarget(serial string, size int64) ClearTarget {
	return ClearTarget{Intervals: MetadataClearRegions(size), SerialNumber: serial, SizeBytes: size}
}

// A clear target declared without deviceName and wwn freezes before every
// target named by its path, in serialNumber order, and plans its clear as
// serial:<serialNumber>, since it has no WWN to name.
func TestASerialClearTargetFreezesFirstAndIsPlannedBySerialNumber(t *testing.T) {
	size := api.FieldValue{Name: "sizeBytes", Value: api.IntegerValue("8192")}
	serial := func(value string) api.FieldValue {
		return api.FieldValue{Name: "serialNumber", Value: api.StringValue(value)}
	}
	byPath := api.MapValue(api.FieldValue{Name: "deviceName", Value: api.StringValue("/dev/disk/by-id/wwn-0x5000000000000001")},
		api.FieldValue{Name: "wwn", Value: api.StringValue("0x5000000000000001")}, serial("A"), size)
	machine := api.NewObject(api.Machine, "metal-01", api.MapValue(), api.MapValue()).WithSpec(api.MapValue().WithPath(api.MapValue(api.FieldValue{Name: "targets",
		Value: api.ListValue(byPath, api.MapValue(serial("NVME-02"), size), api.MapValue(serial("NVME-01"), size))}), "hardware", "clearOnInstall"))
	got := clearTargetsFor(machine)
	want := []ClearTarget{serialClearTarget("NVME-01", 8192), serialClearTarget("NVME-02", 8192), clearTarget("5000000000000001", "A", 8192)}
	if !slices.EqualFunc(got, want, func(a, b ClearTarget) bool {
		return a.DeviceName == b.DeviceName && a.WWN == b.WWN && a.SerialNumber == b.SerialNumber && a.SizeBytes == b.SizeBytes && slices.Equal(a.Intervals, b.Intervals)
	}) {
		t.Fatalf("froze %+v, want %+v", got, want)
	}
	if _, err := RenderKickstart(clearInstallation(got...)); err != nil {
		t.Fatalf("the frozen order was refused: %v", err)
	}
	if _, err := RenderKickstart(clearInstallation(got[2], got[0])); err == nil {
		t.Fatal("a serial target after a target named by its path was rendered")
	}
	for name, target := range map[string]ClearTarget{
		"a lone deviceName": {DeviceName: "/dev/disk/by-id/wwn-0x5000000000000003", SerialNumber: "NVME-03", SizeBytes: 8192, Intervals: MetadataClearRegions(8192)},
		"a lone wwn":        {WWN: "0x5000000000000003", SerialNumber: "NVME-03", SizeBytes: 8192, Intervals: MetadataClearRegions(8192)},
	} {
		if _, err := RenderKickstart(clearInstallation(target)); err == nil {
			t.Fatalf("%s was rendered", name)
		}
	}
	request := Request{Identity: Identity{Object: "metal-01"}, Target: Target{Physical: true, Hardware: &Hardware{ClearOnInstall: got}}}
	impacts := impacts(reconciliation.Apply, request)
	for _, wantImpact := range []string{"clear-disk metal-01 serial:NVME-01", "clear-disk metal-01 serial:NVME-02", "clear-disk metal-01 0x5000000000000001"} {
		if !slices.Contains(impacts, wantImpact) {
			t.Fatalf("the apply impacts %v omit %s", impacts, wantImpact)
		}
	}
}

// A clear target declared by serial number is the one fixed, writable whole
// disk reporting it; the %pre clears it only after proving it is not the root
// disk and has its size, and refuses before any write when none, several or
// the root disk reports the serial.
func TestTheClearResolvesASerialTargetToExactlyOneDisk(t *testing.T) {
	disks := []clearDisk{
		{name: "sdb", byID: "/dev/disk/by-id/unused-b", serial: "NVME-01", size: 8192},
		clearDisks[1],
	}
	flags := func(names ...string) func(blocks, proc string) {
		return func(blocks, _ string) {
			for _, name := range names {
				for _, flag := range []string{"removable", "ro"} {
					if err := os.WriteFile(filepath.Join(blocks, name, flag), []byte("0\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	input := clearInstallation(serialClearTarget("NVME-01", 8192), clearTarget("5000000000000002", "CEPH-02", 2*MetadataClearWindow+4096))
	host, log := clearHost(t, disks, flags("sdb", "sdc"))
	if output, err := runTargetProof(t, input, host); err != nil || !slices.Contains(readLog(t, log), "write /dev/sdb 0 8192") {
		t.Fatalf("the serial target was not cleared: %v %v\n%s", err, readLog(t, log), output)
	}
	for name, test := range map[string]struct {
		disks   []clearDisk
		flagged []string
		refusal string
	}{
		"no disk": {[]clearDisk{{name: "sdb", byID: "/dev/disk/by-id/unused-b", serial: "OTHER", size: 8192}, clearDisks[1]}, []string{"sdb", "sdc"},
			"the declared clear target serial number NVME-01 is reported by no whole disk here"},
		"two disks": {[]clearDisk{disks[0], clearDisks[1], {name: "sdd", byID: "/dev/disk/by-id/unused-d", serial: "NVME-01", size: 8192}}, []string{"sdb", "sdc", "sdd"},
			"the declared clear target serial number NVME-01 is reported by 2 disks here"},
		"the root disk": {[]clearDisk{{name: "sda", byID: "/dev/disk/by-id/unused-a", serial: "NVME-01", size: 8192}, clearDisks[1]}, []string{"sda", "sdc"},
			"the declared clear target serial number NVME-01 is the root disk"},
		"a removable disk": {disks, []string{"sdc"}, "the declared clear target serial number NVME-01 is not a fixed, writable sd or nvme disk"},
		"another size": {[]clearDisk{{name: "sdb", byID: "/dev/disk/by-id/unused-b", serial: "NVME-01", size: 12288}, clearDisks[1]}, []string{"sdb", "sdc"},
			"the declared clear target serial number NVME-01 reports a size other than 8192 bytes"},
	} {
		t.Run(name, func(t *testing.T) {
			host, log := clearHost(t, test.disks, flags(test.flagged...))
			output, err := runTargetProof(t, input, host)
			if err == nil || !strings.Contains(output, "Bootwright: "+test.refusal+"; refusing before any disk is touched.") {
				t.Fatalf("%s was not refused: %v\n%s", name, err, output)
			}
			for _, entry := range readLog(t, log) {
				if strings.HasPrefix(entry, "write ") || strings.HasPrefix(entry, "remove ") {
					t.Fatalf("%s wrote before refusing: %v", name, readLog(t, log))
				}
			}
		})
	}
}
