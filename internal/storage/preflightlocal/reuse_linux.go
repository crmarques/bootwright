package preflightlocal

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
	"golang.org/x/sys/unix"
)

type domainXML struct {
	Name  string `xml:"name"`
	UUID  string `xml:"uuid"`
	VCPU  int64  `xml:"vcpu"`
	Disks []struct {
		Type   string `xml:"type,attr"`
		Device string `xml:"device,attr"`
		Source struct {
			File string `xml:"file,attr"`
		} `xml:"source"`
	} `xml:"devices>disk"`
}
type domainStatusXML struct {
	PID    int       `xml:"pid,attr"`
	Domain domainXML `xml:"domain"`
}

type reuseProofs struct {
	config        func(string, int64) ([]byte, error)
	disk, runtime func(preflight.Allocation) (int64, error)
}

func reusable(ctx context.Context, allocation preflight.Allocation) (preflight.Quantities, error) {
	return reuseProofs{config: readRootProof, disk: provedDisk, runtime: runtimeMemory}.reusable(ctx, allocation)
}
func (proofs reuseProofs) reusable(ctx context.Context, allocation preflight.Allocation) (preflight.Quantities, error) {
	if !allocation.Allocated {
		return preflight.Quantities{}, nil
	}
	if allocation.Domain == "" || filepath.Base(allocation.Domain) != allocation.Domain || allocation.UUID == "" || len(allocation.Disks) == 0 {
		return preflight.Quantities{}, errors.New("missing projected domain identity")
	}
	config, err := proofs.config("/etc/libvirt/qemu/"+allocation.Domain+".xml", domainProofBound)
	if os.IsNotExist(err) {
		return preflight.Quantities{}, ctx.Err()
	}
	if err != nil {
		return preflight.Quantities{}, err
	}
	var domain domainXML
	if err := xml.Unmarshal(config, &domain); err != nil || !domainMatches(domain, allocation) {
		return preflight.Quantities{}, errors.New("the domain does not prove its projected identity and disks")
	}
	credit, err := proofs.disk(allocation)
	if err != nil {
		return preflight.Quantities{}, err
	}
	memory, err := proofs.runtime(allocation)
	quantities, err := reuseCredit(allocation, credit, memory, err)
	if err != nil {
		return quantities, err
	}
	return quantities, ctx.Err()
}
func provedDisk(allocation preflight.Allocation) (int64, error) {
	_, identity, err := availableDisk(allocation.Pool)
	if err != nil {
		return 0, err
	}
	return diskCredit(allocation.Disks, identity)
}
func runtimeMemory(allocation preflight.Allocation) (int64, error) {
	runtime, err := readRootProof("/run/libvirt/qemu/"+allocation.Domain+".xml", domainProofBound)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var status domainStatusXML
	if err := xml.Unmarshal(runtime, &status); err != nil || status.PID <= 0 || !domainMatches(status.Domain, allocation) {
		return 0, errors.New("the runtime domain identity is unproved")
	}
	return processCredit(status.PID, allocation)
}
func domainMatches(domain domainXML, allocation preflight.Allocation) bool {
	if domain.Name != allocation.Domain || domain.UUID != allocation.UUID {
		return false
	}
	disks := []string{}
	for _, disk := range domain.Disks {
		if disk.Device == "disk" {
			if disk.Type != "file" || disk.Source.File == "" {
				return false
			}
			disks = append(disks, disk.Source.File)
		}
	}
	expected := slices.Clone(allocation.Disks)
	slices.Sort(expected)
	slices.Sort(disks)
	return slices.Equal(disks, expected)
}
func diskCredit(paths []string, filesystem string) (int64, error) {
	total := int64(0)
	inodes := map[[2]uint64]bool{}
	for _, path := range paths {
		file, err := openSafe(path)
		if err != nil {
			return 0, err
		}
		var stat unix.Stat_t
		err = unix.Fstat(int(file.Fd()), &stat)
		var fs unix.Statfs_t
		fsErr := unix.Fstatfs(int(file.Fd()), &fs)
		file.Close()
		if fsErr != nil || fmt.Sprintf("%v", fs.Fsid) != filesystem {
			return 0, errors.New("projected disk is on another filesystem")
		}
		if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Blocks < 0 || stat.Blocks > math.MaxInt64/512 {
			return 0, errors.New("a projected disk is not a unique regular file")
		}
		identity := [2]uint64{uint64(stat.Dev), stat.Ino}
		if inodes[identity] {
			return 0, errors.New("projected disks alias one file")
		}
		inodes[identity] = true
		size := stat.Blocks * 512
		if total > math.MaxInt64-size {
			return 0, errors.New("disk allocation overflows")
		}
		total += size
	}
	return total, nil
}
func processCredit(pid int, allocation preflight.Allocation) (int64, error) {
	prefix := "/proc/" + strconv.Itoa(pid) + "/"
	before, err := readSafe(prefix+"stat", 64<<10)
	if err != nil {
		return 0, err
	}
	command, err := readSafe(prefix+"cmdline", 64<<10)
	if err != nil {
		return 0, err
	}
	words := strings.Split(string(command), "\x00")
	clear(command)
	uuid := false
	name := false
	for i, word := range words {
		if i+1 >= len(words) {
			continue
		}
		if word == "-uuid" && words[i+1] == allocation.UUID {
			uuid = true
		}
		if word == "-name" && (words[i+1] == allocation.Domain || strings.HasPrefix(words[i+1], "guest="+allocation.Domain+",")) {
			name = true
		}
	}
	if !uuid || !name {
		return 0, errors.New("the runtime PID does not name the expected domain")
	}
	memory, err := readSafe(prefix+"smaps_rollup", 64<<10)
	if err != nil {
		return 0, err
	}
	amount, known := parseMemoryKey(memory, "Pss_Anon:")
	if !known {
		return 0, errors.New("anonymous resident memory is unknown")
	}
	after, err := readSafe(prefix+"stat", 64<<10)
	if err != nil {
		return 0, err
	}
	if processStart(before) == "" || processStart(before) != processStart(after) {
		return 0, errors.New("the runtime process changed during observation")
	}
	return amount, nil
}
func processStart(data []byte) string {
	_, tail, found := strings.Cut(string(data), ") ")
	if !found {
		return ""
	}
	fields := strings.Fields(tail)
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}
func readSafe(path string, limit int64) ([]byte, error) {
	file, err := openSafe(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("the capacity proof is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("capacity proof exceeds its byte bound")
	}
	return data, nil
}
func openSafe(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("capacity proof path is not absolute and clean")
	}
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if path == "/" {
		return os.NewFile(uintptr(descriptor), path), nil
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(descriptor, part, flags, 0)
		unix.Close(descriptor)
		if err != nil {
			return nil, err
		}
		descriptor = next
	}
	return os.NewFile(uintptr(descriptor), path), nil
}

func readRootProof(path string, limit int64) ([]byte, error) {
	file, err := openSafe(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Uid != 0 || stat.Mode&0022 != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("domain proof is not protected root-owned state")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("domain proof exceeds its byte bound")
	}
	return data, nil
}

type runtimeUnproved struct{ error }

func reuseCredit(allocation preflight.Allocation, disk, memory int64, runtime error) (preflight.Quantities, error) {
	credit := preflight.Quantities{Disk: min(disk, allocation.Quantities.Disk)}
	if runtime != nil {
		return credit, runtimeUnproved{runtime}
	}
	credit.Memory = min(memory, allocation.Quantities.Memory)
	return credit, nil
}
