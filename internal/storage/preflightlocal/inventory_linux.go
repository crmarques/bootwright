package preflightlocal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

func foreignCPUs(ctx context.Context, allocations []preflight.Allocation) (int64, error) {
	domains := map[string]domainXML{}
	for _, directory := range []string{"/etc/libvirt/qemu", "/run/libvirt/qemu"} {
		if err := readDomainDirectory(ctx, directory, readRootProof, domains); err != nil {
			return 0, err
		}
	}
	return foreignBudget(domains, allocations)
}
func readDomainDirectory(ctx context.Context, directory string, read func(string, int64) ([]byte, error), domains map[string]domainXML) error {
	file, err := openSafe(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := file.ReadDir(1025)
	file.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 1024 {
		return errors.New("libvirt inventory exceeds its domain bound")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasSuffix(entry.Name(), ".xml") || entry.IsDir() {
			continue
		}
		data, err := read(directory+"/"+entry.Name(), domainProofBound)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := mergeDomain(domains, data); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
	}
	return nil
}
