package converge

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func SplitReclaimDevices(devices string) []string {
	var out []string
	for _, raw := range strings.Split(devices, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func DeclaredOwnedOSDDevices(state v1alpha1.State, owned []string) []string {
	ownedSet := map[string]bool{}
	for _, name := range owned {
		ownedSet[name] = true
	}
	declared := map[string]bool{}
	for _, sc := range state.StorageClusters {
		if !ownedSet[sc.Metadata.Name] || sc.Spec.Ceph == nil {
			continue
		}
		for _, host := range sc.Spec.Ceph.Topology.Hosts {
			for _, dev := range host.Devices {
				if trimmed := strings.TrimSpace(dev); trimmed != "" {
					declared[trimmed] = true
				}
			}
		}
	}
	out := make([]string, 0, len(declared))
	for dev := range declared {
		out = append(out, dev)
	}
	sort.Strings(out)
	return out
}

func UnmatchedReclaimDevices(state v1alpha1.State, owned []string, devices string) (unmatched, declared []string) {
	requested := SplitReclaimDevices(devices)
	if len(requested) == 0 {
		return nil, nil
	}
	declared = DeclaredOwnedOSDDevices(state, owned)
	declaredSet := map[string]bool{}
	for _, dev := range declared {
		declaredSet[dev] = true
	}
	for _, entry := range requested {
		if !declaredSet[entry] {
			unmatched = append(unmatched, entry)
		}
	}
	return unmatched, declared
}
