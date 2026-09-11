//go:build linux && amd64

package hostlinux

import (
	"context"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"golang.org/x/sys/unix"
)

func (f *heldFilesystem) kernelRoot(ctx context.Context, name string, expected int64) (*os.File, error) {
	file, stat, err := f.openPath(ctx, name, false)
	if err != nil {
		return nil, errEvidence
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 || f.filesystemType(file, name) != expected {
		file.Close()
		return nil, errEvidence
	}
	return file, nil
}

func (f *heldFilesystem) filesystemType(file *os.File, name string) int64 {
	var stat unix.Statfs_t
	if unix.Fstatfs(int(file.Fd()), &stat) != nil {
		return -1
	}
	if f.view.filesystem != nil {
		return f.view.filesystem(name, stat.Type)
	}
	return stat.Type
}

func (f *heldFilesystem) sameMountNamespace(ctx context.Context, proc *os.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var stats [2]unix.Stat_t
	for index, name := range []string{"self/ns/mnt", "1/ns/mnt"} {
		// Only these fixed kernel namespace links may be followed. Generic
		// trusted-path reads never follow a procfs magic link.
		fd, err := unix.Openat(int(proc.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			return errEvidence
		}
		file := os.NewFile(uintptr(fd), "/proc/"+name)
		err = unix.Fstat(fd, &stats[index])
		kind := f.filesystemType(file, "/proc/"+name)
		file.Close()
		if err != nil || kind != unix.NSFS_MAGIC {
			return errEvidence
		}
	}
	if stats[0].Dev != stats[1].Dev || stats[0].Ino != stats[1].Ino {
		return errEvidence
	}
	return nil
}

func (f *heldFilesystem) readKernel(ctx context.Context, root *os.File, name string, maximum int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(root.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errEvidence
	}
	file := os.NewFile(uintptr(fd), "/proc/"+name)
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != f.view.owner || stat.Mode&0022 != 0 || f.filesystemType(file, "/proc/"+name) != unix.PROC_SUPER_MAGIC {
		return nil, errEvidence
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, maximum+1))
	if err != nil || len(data) > int(maximum) {
		return nil, errEvidence
	}
	return data, nil
}

type mountedRoot struct {
	deviceMajor uint32
	deviceMinor uint32
	kind        string
	source      string
}

func rootMount(data []byte) (mountedRoot, error) {
	var root mountedRoot
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		left, right, found := strings.Cut(line, " - ")
		fields, target := strings.Fields(left), strings.Fields(right)
		if !found || len(fields) < 6 || len(target) < 3 {
			return mountedRoot{}, errEvidence
		}
		if fields[4] != "/" {
			continue
		}
		count++
		major, minor, found := strings.Cut(fields[2], ":")
		majorValue, majorErr := strconv.ParseUint(major, 10, 32)
		minorValue, minorErr := strconv.ParseUint(minor, 10, 32)
		if !found || majorErr != nil || minorErr != nil || !strings.HasPrefix(target[1], "/dev/") || path.Clean(target[1]) != target[1] || strings.ContainsAny(target[1], "\\\x00") {
			return mountedRoot{}, errEvidence
		}
		if target[0] != "xfs" && target[0] != "ext4" && target[0] != "btrfs" {
			return mountedRoot{}, errEvidence
		}
		// A bind-mounted subdirectory of ext4/XFS is not the installed root.
		if target[0] != "btrfs" && fields[3] != "/" {
			return mountedRoot{}, errEvidence
		}
		root = mountedRoot{deviceMajor: uint32(majorValue), deviceMinor: uint32(minorValue), kind: target[0], source: target[1]}
	}
	if count != 1 {
		return mountedRoot{}, errEvidence
	}
	return root, nil
}

func (f *heldFilesystem) filesystemUUID(ctx context.Context, mount mountedRoot) (string, error) {
	expected := map[string]int64{"ext4": unix.EXT4_SUPER_MAGIC, "xfs": unix.XFS_SUPER_MAGIC, "btrfs": unix.BTRFS_SUPER_MAGIC}[mount.kind]
	if f.filesystemType(f.root, "/") != expected {
		return "", errEvidence
	}
	source, device, err := f.openPath(ctx, mount.source, false)
	if err != nil {
		return "", errEvidence
	}
	source.Close()
	if device.Mode&unix.S_IFMT != unix.S_IFBLK {
		return "", errEvidence
	}
	if mount.kind != "btrfs" && (unix.Major(device.Rdev) != mount.deviceMajor || unix.Minor(device.Rdev) != mount.deviceMinor) {
		return "", errEvidence
	}
	directory, stat, err := f.openPath(ctx, "/dev/disk/by-uuid", false)
	if err != nil {
		return "", errEvidence
	}
	defer directory.Close()
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
		return "", errEvidence
	}
	fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", errEvidence
	}
	entries := os.NewFile(uintptr(fd), "/dev/disk/by-uuid")
	defer entries.Close()
	names, err := entries.Readdirnames(1025)
	if err != nil && err != io.EOF || len(names) > 1024 {
		return "", errEvidence
	}
	result := ""
	for _, name := range names {
		if !canonicalUUID(name) {
			continue
		}
		candidate, candidateStat, err := f.openPath(ctx, "/dev/disk/by-uuid/"+name, false)
		if err != nil {
			return "", errEvidence
		}
		candidate.Close()
		if candidateStat.Mode&unix.S_IFMT != unix.S_IFBLK || candidateStat.Rdev != device.Rdev {
			continue
		}
		if result != "" {
			return "", errEvidence
		}
		result = name
	}
	if result == "" {
		return "", errEvidence
	}
	return result, nil
}

func canonicalUUID(value string) bool {
	_, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, "1234567890abcdef1234567890abcdef", "12345678-90ab-cdef-1234-567890abcdef", value)
	return err == nil
}

func nestedPIDNamespace(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "NSpid:") {
			return len(strings.Fields(line)) != 2
		}
	}
	return true
}

func containerCgroup(data []byte) bool {
	value := strings.ToLower(string(data))
	for _, marker := range []string{"docker", "libpod", "kubepods", "containerd", "lxc"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
