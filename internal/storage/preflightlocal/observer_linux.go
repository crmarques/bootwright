package preflightlocal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
	"golang.org/x/sys/unix"
)

// Observer reads existing kernel facts without invoking tools or writing files.
// A remote target is observed only through Remote and is never read locally.
type Observer struct{ Remote RemoteCommands }

func (o Observer) Observe(ctx context.Context, target preflight.Target) (preflight.Facts, error) {
	if err := ctx.Err(); err != nil {
		return preflight.Facts{}, err
	}
	if !target.Local && o.Remote != nil {
		return observeRemote(ctx, o.Remote, target)
	}
	if !target.Local {
		return preflight.Facts{Reason: "verified remote capacity observation is unavailable"}, nil
	}
	if target.DiskOnly {
		return diskFacts(target.Pool), ctx.Err()
	}
	facts := preflight.Facts{}
	inventoryFailure := ""
	var cpus unix.CPUSet
	if err := unix.SchedGetaffinity(0, &cpus); err == nil {
		facts.Available.CPU = int64(cpus.Count())
		facts.CPUKnown = facts.Available.CPU > 0
		foreign, err := foreignCPUs(ctx, target.Allocations)
		if err != nil {
			facts.CPUKnown = false
			inventoryFailure = inventoryReason(err)
		} else {
			facts.Available.CPU = max(0, facts.Available.CPU-foreign)
		}
	}
	memory, err := readBounded("/proc/meminfo", 64<<10)
	if err == nil {
		facts.Available.Memory, facts.MemoryKnown = parseMemory(memory)
	}
	disk, identity, err := availableDisk(target.Pool)
	if err == nil {
		facts.Available.Disk = disk
		facts.DiskIdentity = identity
		facts.DiskKnown = true
	}
	facts.Reason = unreadReason(facts, inventoryFailure)
	facts.Reusable = map[string]preflight.Quantities{}
	facts.Unproved = map[string]string{}
	for _, allocation := range target.Allocations {
		if allocation.Allocated {
			credit, err := reusable(ctx, allocation)
			recordReuse(&facts, allocation.Machine, credit, err)
		}
	}
	return facts, ctx.Err()
}
func diskFacts(path string) preflight.Facts {
	disk, identity, err := filesystemOf(path)
	if err != nil {
		return preflight.Facts{Reason: "the bounded local filesystem capacity fact could not be read"}
	}
	return preflight.Facts{Available: preflight.Quantities{Disk: disk}, DiskIdentity: identity, DiskKnown: true}
}
func unreadReason(facts preflight.Facts, inventoryFailure string) string {
	if facts.CPUKnown && facts.MemoryKnown && facts.DiskKnown {
		return ""
	}
	if inventoryFailure != "" && facts.MemoryKnown && facts.DiskKnown {
		return inventoryFailure
	}
	return "one or more bounded local kernel capacity facts could not be read"
}
func readBounded(path string, limit int64) ([]byte, error) { return readSafe(path, limit) }

func availableDisk(path string) (int64, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return 0, "", errors.New("capacity pool path is not absolute and clean")
	}

	for {
		file, err := openSafe(path)
		if err == nil {
			defer file.Close()
			info, err := file.Stat()
			if err != nil || !info.IsDir() {
				return 0, "", errors.New("pool ancestor is not a readable directory")
			}
			return filesystemCapacity(int(file.Fd()))
		}
		if !os.IsNotExist(err) || path == "/" {
			return 0, "", err
		}
		path = filepath.Dir(path)
	}
}
func recordReuse(facts *preflight.Facts, machine string, credit preflight.Quantities, err error) {
	var runtime runtimeUnproved
	if err == nil || errors.As(err, &runtime) {
		facts.Reusable[machine] = credit
	}
	if err != nil {
		reason := err.Error()
		if len(reason) > 160 {
			reason = strings.ToValidUTF8(reason[:160], "")
		}
		facts.Unproved[machine] = reason
	}
}

// A filesystem identity is used only for in-memory aggregation.
func filesystemCapacity(descriptor int) (int64, string, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(descriptor, &stat); err != nil {
		return 0, "", err
	}
	if stat.Bsize <= 0 || stat.Bavail > uint64(math.MaxInt64)/uint64(stat.Bsize) {
		return 0, "", errors.New("invalid filesystem capacity")
	}
	return int64(stat.Bavail) * int64(stat.Bsize), fmt.Sprintf("%v", stat.Fsid), nil
}

func filesystemOf(path string) (int64, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return 0, "", errors.New("capacity write path is not absolute and clean")
	}
	for {
		descriptor, err := openPathSafe(path)
		if err == nil {
			defer unix.Close(descriptor)
			var info unix.Stat_t
			if err := unix.Fstat(descriptor, &info); err != nil {
				return 0, "", err
			}
			if info.Mode&unix.S_IFMT == unix.S_IFLNK {
				return 0, "", errors.New("capacity write path is a symbolic link")
			}
			return filesystemCapacity(descriptor)
		}
		if !os.IsNotExist(err) || path == "/" {
			return 0, "", err
		}
		path = filepath.Dir(path)
	}
}

func openPathSafe(path string) (int, error) {
	descriptor, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil || path == "/" {
		return descriptor, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		flags := unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(descriptor, part, flags, 0)
		unix.Close(descriptor)
		if err != nil {
			return -1, err
		}
		descriptor = next
	}
	return descriptor, nil
}
