package preflightlocal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
	"golang.org/x/sys/unix"
)

// Observer reads existing kernel facts without invoking tools or writing files.
// Remote targets require a verified remote adapter and are never read locally.
type Observer struct{}

func (Observer) Observe(ctx context.Context, target preflight.Target) (preflight.Facts, error) {
	if err := ctx.Err(); err != nil {
		return preflight.Facts{}, err
	}
	if !target.Local {
		return preflight.Facts{Reason: "verified remote capacity observation is unavailable"}, nil
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

func parseMemory(data []byte) (int64, bool) { return parseMemoryKey(data, "MemAvailable:") }
func parseMemoryKey(data []byte, key string) (int64, bool) {
	var available int64
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != key {
			continue
		}
		if found || len(fields) != 3 || fields[2] != "kB" {
			return 0, false
		}
		number, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || number < 0 || number > math.MaxInt64/1024 {
			return 0, false
		}
		available = number * 1024
		found = true
	}
	return available, found
}
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
			var stat unix.Statfs_t
			if err := unix.Fstatfs(int(file.Fd()), &stat); err != nil {
				return 0, "", err
			}
			if stat.Bsize <= 0 || stat.Bavail > uint64(math.MaxInt64)/uint64(stat.Bsize) {
				return 0, "", errors.New("invalid filesystem capacity")
			}
			// A filesystem identity is used only for in-memory aggregation.
			return int64(stat.Bavail) * int64(stat.Bsize), fmt.Sprintf("%v", stat.Fsid), nil
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
