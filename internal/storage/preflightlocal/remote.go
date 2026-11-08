package preflightlocal

import (
	"bytes"
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

type RemoteCommands interface {
	Run(ctx context.Context, contextName, machine string, argv []string, bound int) ([]byte, int, error)
}

const (
	remoteDomainDirectories = 2
	remoteEntryBound        = 1024
	remotePathDepthBound    = 32
	remotePathBound         = 4096
	remoteNameBound         = 255
	remoteFactBound         = 64 << 10
	remoteListingBound      = remoteDomainDirectories * remoteEntryBound * (remotePathBound + 1)
	remoteDomainReadBound   = 16 << 20
	remoteMissing           = 1
	remoteReuseUnproved     = "remote reuse credit is not observed"
	remoteInventoryFailure  = "the libvirt domain inventory of the remote host could not be read"
	remoteFactsFailure      = "one or more bounded remote capacity facts could not be read"
)

var remoteDomainRoots = [remoteDomainDirectories]string{"/etc/libvirt/qemu", "/run/libvirt/qemu"}

func observeRemote(ctx context.Context, remote RemoteCommands, target preflight.Target) (preflight.Facts, error) {
	facts := preflight.Facts{Reusable: map[string]preflight.Quantities{}, Unproved: map[string]string{}}
	if target.Context == "" || target.Host == "" {
		facts.Reason = remoteUnobservable(target.Host)
		return facts, ctx.Err()
	}
	run := func(bound int, argv ...string) ([]byte, int, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		return remote.Run(ctx, target.Context, target.Host, argv, bound)
	}
	if target.DiskOnly {
		if disk, identity, ok := remoteDisk(ctx, run, target); ok {
			facts.Available.Disk, facts.DiskIdentity, facts.DiskKnown = disk, "ssh/"+target.Host+"/"+identity, true
		} else {
			facts.Reason = remoteFactsFailure
		}
		return facts, ctx.Err()
	}
	inventoryFailed := false
	if out, status, err := run(remoteFactBound, "/usr/bin/nproc"); err == nil && status == 0 {
		if cpus, ok := parseCPUCount(out); ok {
			facts.Available.CPU, facts.CPUKnown = cpus, true
		}
	}
	if out, status, err := run(remoteFactBound, "/usr/bin/cat", "/proc/meminfo"); err == nil && status == 0 {
		facts.Available.Memory, facts.MemoryKnown = parseMemory(out)
	}
	if disk, identity, ok := remoteDisk(ctx, run, target); ok {
		facts.Available.Disk, facts.DiskIdentity, facts.DiskKnown = disk, "ssh/"+target.Host+"/"+identity, true
	}
	if facts.CPUKnown {
		foreign, err := remoteForeignCPUs(ctx, run, target.Allocations)
		if err != nil {
			facts.CPUKnown, inventoryFailed = false, true
		} else {
			facts.Available.CPU = max(0, facts.Available.CPU-foreign)
		}
	}
	for _, allocation := range target.Allocations {
		if allocation.Allocated {
			facts.Unproved[allocation.Machine] = remoteReuseUnproved
		}
	}
	switch {
	case facts.CPUKnown && facts.MemoryKnown && facts.DiskKnown:
	case !facts.CPUKnown && !facts.MemoryKnown && !facts.DiskKnown && !inventoryFailed:
		facts.Reason = remoteUnobservable(target.Host)
	case inventoryFailed && facts.MemoryKnown && facts.DiskKnown:
		facts.Reason = remoteInventoryFailure
	default:
		facts.Reason = remoteFactsFailure
	}
	return facts, ctx.Err()
}

func remoteUnobservable(host string) string {
	return "verified remote capacity observation of " + host + " is unavailable"
}

func parseCPUCount(out []byte) (int64, bool) {
	text, found := strings.CutSuffix(string(out), "\n")
	if !found || text == "" || strings.TrimLeft(text, "0123456789") != "" {
		return 0, false
	}
	count, err := strconv.ParseInt(text, 10, 64)
	return count, err == nil && count > 0
}

type remoteRun func(bound int, argv ...string) ([]byte, int, error)

func remoteDisk(ctx context.Context, run remoteRun, target preflight.Target) (int64, string, bool) {
	path := target.Pool
	if !remotePathUsable(path) || strings.Count(path, "/") > remotePathDepthBound {
		return 0, "", false
	}
	for {
		out, status, err := run(remoteFactBound, "/usr/bin/stat", "--file-system", "--format=%a %S %i", "--", path)
		if err == nil && status == 0 {
			return parseFilesystem(out)
		}
		if err != nil || status != remoteMissing || path == "/" || ctx.Err() != nil {
			return 0, "", false
		}
		path = filepath.Dir(path)
	}
}

func remotePathUsable(path string) bool {
	return len(path) <= remotePathBound && utf8.ValidString(path) && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsFunc(path, unicode.IsControl)
}

func parseFilesystem(out []byte) (int64, string, bool) {
	text, found := strings.CutSuffix(string(out), "\n")
	fields := strings.Split(text, " ")
	if !found || len(fields) != 3 {
		return 0, "", false
	}
	blocks, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || strings.TrimLeft(fields[0], "0123456789") != "" {
		return 0, "", false
	}
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || size <= 0 || strings.TrimLeft(fields[1], "0123456789") != "" || blocks > uint64(math.MaxInt64)/uint64(size) {
		return 0, "", false
	}
	if id := fields[2]; id == "" || len(id) > 32 || strings.TrimLeft(id, "0123456789abcdef") != "" {
		return 0, "", false
	}
	return int64(blocks) * size, fields[2], true
}

func remoteForeignCPUs(ctx context.Context, run remoteRun, allocations []preflight.Allocation) (int64, error) {
	listing, status, err := run(remoteListingBound, "/usr/bin/find", remoteDomainRoots[0], remoteDomainRoots[1], "-maxdepth", "1", "-type", "f", "-name", "*.xml", "-print")
	if err != nil || status != 0 {
		return 0, errors.New("the domain listing did not complete")
	}
	paths, err := parseDomainListing(listing)
	if err != nil {
		return 0, err
	}
	domains := map[string]domainXML{}
	if len(paths) > 0 {
		read, status, err := run(remoteDomainReadBound, append([]string{"/usr/bin/tail", "-v", "-n", "+1", "--"}, paths...)...)
		if err != nil || status != 0 {
			return 0, errors.New("the domain files did not read")
		}
		files, err := splitTail(read, paths)
		if err != nil {
			return 0, err
		}
		for _, data := range files {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if mergeDomain(domains, data) != nil {
				return 0, errors.New("a domain file is not a proved domain")
			}
		}
	}
	return foreignBudget(domains, allocations)
}

func parseDomainListing(out []byte) ([]string, error) {
	if len(out) == 0 {
		return nil, nil
	}
	text, found := strings.CutSuffix(string(out), "\n")
	if !found {
		return nil, errors.New("the domain listing is not line terminated")
	}
	var paths []string
	perDirectory := map[string]int{}
	for line := range strings.SplitSeq(text, "\n") {
		base, name := filepath.Split(line)
		base = strings.TrimSuffix(base, "/")
		if !slices.Contains(remoteDomainRoots[:], base) || !remoteDomainName(name) || line != base+"/"+name {
			return nil, errors.New("the domain listing names an unexpected path")
		}
		if perDirectory[base]++; perDirectory[base] > remoteEntryBound {
			return nil, errors.New("libvirt inventory exceeds its domain bound")
		}
		paths = append(paths, line)
	}
	slices.Sort(paths)
	if len(slices.Compact(slices.Clone(paths))) != len(paths) {
		return nil, errors.New("the domain listing repeats a path")
	}
	return paths, nil
}

func remoteDomainName(name string) bool {
	return len(name) > len(".xml") && len(name) <= remoteNameBound && strings.HasSuffix(name, ".xml") && utf8.ValidString(name) &&
		!strings.ContainsFunc(name, func(r rune) bool { return r == '/' || unicode.IsControl(r) })
}

func splitTail(out []byte, paths []string) ([][]byte, error) {
	files := make([][]byte, 0, len(paths))
	rest := out
	for index, path := range paths {
		header := []byte("==> " + path + " <==\n")
		if index > 0 {
			header = append([]byte("\n"), header...)
		}
		if !bytes.HasPrefix(rest, header) {
			return nil, errors.New("the domain file output does not match its request")
		}
		rest = rest[len(header):]
		content := rest
		if index+1 < len(paths) {
			next := []byte("\n==> " + paths[index+1] + " <==\n")
			end := bytes.Index(rest, next)
			if end < 0 {
				return nil, errors.New("the domain file output does not match its request")
			}
			content, rest = rest[:end], rest[end:]
		}
		if len(content) > domainProofBound || bytes.Contains(content, []byte("\n==> ")) || bytes.HasPrefix(content, []byte("==> ")) {
			return nil, errors.New("the domain file output does not match its request")
		}
		files = append(files, content)
	}
	return files, nil
}
