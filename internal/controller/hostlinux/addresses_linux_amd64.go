//go:build linux && amd64

package hostlinux

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (i Inspector) HardwareAddresses(ctx context.Context) ([]string, error) {
	directory := filepath.Join(i.view.root, "sys", "class", "net")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, inspectionFailure(ctx, "controller.unsupported", "this host's network interfaces cannot be read")
	}
	var addresses []string
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(directory, entry.Name(), "address"))
		if err != nil || len(data) > 64 {
			continue
		}
		address := strings.ToLower(strings.TrimSpace(string(data)))
		if address != "" && strings.Trim(address, "0:") != "" {
			addresses = append(addresses, address)
		}
	}
	slices.Sort(addresses)
	return slices.Compact(addresses), ctx.Err()
}
