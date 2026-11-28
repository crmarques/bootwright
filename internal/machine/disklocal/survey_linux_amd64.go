package disklocal

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"golang.org/x/sys/unix"
)

var (
	wholeName   = regexp.MustCompile(`^(sd[a-z]+|nvme[0-9]+n[0-9]+)$`)
	ignoredName = regexp.MustCompile(`^(fd|loop|ram|sr|zram)[0-9]+$`)
	usbPath     = regexp.MustCompile(`/usb[0-9]+/`)
)

// Survey reads kernel attributes and opens block devices read-only.
func Survey(ctx context.Context, serial bool) (inventory.DiskSnapshot, error) {
	cmdline, err := readBounded("/proc/cmdline", 32768)
	if err != nil {
		return inventory.DiskSnapshot{}, kernelRefusal("the Linux kernel command line could not be read")
	}
	if hasAutomaticInstallArguments(string(cmdline)) {
		return inventory.DiskSnapshot{}, kernelRefusal("the kernel command line contains automatic RHCOS installation arguments")
	}
	return snapshot(ctx, serial), ctx.Err()
}

func hasAutomaticInstallArguments(cmdline string) bool {
	for _, flag := range strings.Fields(cmdline) {
		if strings.HasPrefix(flag, "coreos.inst.") {
			return true
		}
	}
	return false
}

func kernelRefusal(message string) error {
	return diagnostics.NewFailureWithRemediation("runtime.internal", message, "", "run the disk check from a live Linux boot prepared for inspection")
}

func snapshot(ctx context.Context, serial bool) inventory.DiskSnapshot {
	boot, err := readBounded("/proc/sys/kernel/random/boot_id", 64)
	result := inventory.DiskSnapshot{Boot: strings.TrimSpace(string(boot))}
	if err != nil || result.Boot == "" {
		return result
	}
	dir, err := os.Open("/sys/class/block")
	if err != nil {
		return result
	}
	entries, err := dir.ReadDir(4*diskselection.MaxDisks + 1)
	_ = dir.Close()
	if (err != nil && !errors.Is(err, io.EOF)) || len(entries) > 4*diskselection.MaxDisks {
		return result
	}
	complete := true
	for _, entry := range entries {
		if ctx.Err() != nil || len(result.Disks) == diskselection.MaxDisks {
			return result
		}
		name := entry.Name()
		if !wholeName.MatchString(name) {
			if unsupportedWhole("/sys/class/block", name) {
				result.Disks = append(result.Disks, inventory.DiskObservation{Device: name})
				complete = false
			}
			continue
		}
		disk, ok := readDisk(name, serial)
		result.Disks = append(result.Disks, disk)
		complete = complete && ok
	}
	slices.SortFunc(result.Disks, func(a, b inventory.DiskObservation) int { return strings.Compare(a.Device, b.Device) })
	end, err := readBounded("/proc/sys/kernel/random/boot_id", 64)
	result.Complete = complete && err == nil && result.Boot == strings.TrimSpace(string(end))
	return result
}

// An unsupported whole device could hold another instance of a requested
// identifier. Optical drives, RAM-backed devices and partitions are not
// candidates for this physical disk check.
func unsupportedWhole(root, name string) bool {
	if ignoredName.MatchString(name) {
		return false
	}
	uevent, err := readBounded(filepath.Join(root, name, "uevent"), 4096)
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(uevent), "\n") {
		if line == "DEVTYPE=partition" {
			return false
		}
	}
	return true
}

func readDisk(name string, serial bool) (inventory.DiskObservation, bool) {
	row := inventory.DiskObservation{Device: name}
	sys := filepath.Join("/sys/class/block", name)
	hidden := scalar(sys, "hidden")
	if hidden != "" && hidden != "0" && hidden != "1" {
		return row, false
	}
	fd, err := unix.Open(filepath.Join("/dev", name), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return row, false
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil || info.Mode&unix.S_IFMT != unix.S_IFBLK {
		return row, false
	}
	number := strconv.FormatUint(uint64(unix.Major(info.Rdev)), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(info.Rdev)), 10)
	seq, err := unix.IoctlGetUint64(fd, unix.BLKGETDISKSEQ)
	if err != nil || seq == 0 || scalar(sys, "dev") != number {
		return row, false
	}
	_, partition := os.Lstat(filepath.Join(sys, "partition"))
	sysPath, linkErr := filepath.EvalSymlinks(sys)
	ro, rm := scalar(sys, "ro"), scalar(sys, "removable")
	if !errors.Is(partition, os.ErrNotExist) || linkErr != nil || (ro != "0" && ro != "1") || (rm != "0" && rm != "1") {
		return row, false
	}
	size, err := unix.IoctlGetUint64(fd, unix.BLKGETSIZE64)
	if err != nil || size > uint64(diskselection.MaxBytes) || !diskselection.ValidSize(int64(size)) {
		return row, false
	}
	row.SizeBytes = int64(size)
	row.WWN = readWWN(sys)
	if serial {
		row.SerialNumber = readSerial(sys)
	}
	after, err := unix.IoctlGetUint64(fd, unix.BLKGETDISKSEQ)
	if err != nil || after != seq || scalar(sys, "diskseq") != strconv.FormatUint(seq, 10) || scalar(sys, "dev") != number {
		return row, false
	}
	row.Sequence = seq
	row.Eligible = ro == "0" && rm == "0" && hidden != "1" && !usbPath.MatchString(sysPath)
	return row, true
}

func readWWN(sys string) string {
	contents, err := readBounded(filepath.Join(sys, "wwid"), 258)
	if errors.Is(err, os.ErrNotExist) {
		contents, err = readBounded(filepath.Join(sys, "device/wwid"), 258)
	}
	wwn := strings.ToLower(strings.TrimSpace(string(contents)))
	if strings.HasPrefix(wwn, "naa.") {
		wwn = "0x" + strings.TrimPrefix(wwn, "naa.")
	}
	if err != nil || !diskselection.ValidWWN(wwn) {
		return ""
	}
	return wwn
}

func readSerial(sys string) string {
	contents, err := readBounded(filepath.Join(sys, "device/serial"), 258)
	if errors.Is(err, os.ErrNotExist) {
		contents, err = readBounded(filepath.Join(sys, "device/vpd_pg80"), 260)
		if err != nil || len(contents) < 4 || contents[1] != 0x80 || int(binary.BigEndian.Uint16(contents[2:4])) != len(contents)-4 {
			return ""
		}
		contents = contents[4:]
	}
	value := strings.TrimSpace(string(contents))
	if err != nil || !diskselection.ValidSerial(value) {
		return ""
	}
	return value
}

func scalar(sys, name string) string {
	value, err := readBounded(filepath.Join(sys, name), 258)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(value))
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(value)) > limit {
		return nil, errors.New("kernel attribute exceeds its read bound")
	}
	return value, err
}
