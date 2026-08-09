package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

const ReclaimDevicesAll = "all"

type ReclaimDevicesMixedAllError struct {
	Entries []string
}

func (e *ReclaimDevicesMixedAllError) Error() string {
	return fmt.Sprintf("--reclaim-devices cannot combine %s with explicit device paths: choose %s alone or name only explicit paths", ReclaimDevicesAll, ReclaimDevicesAll)
}

type ReclaimAllNoDeclaredDevicesError struct {
	Clusters                   []string
	EffectiveUnboundedClusters []string
}

func (e *ReclaimAllNoDeclaredDevicesError) Error() string {
	return fmt.Sprintf("--reclaim-devices %s matched no device: selected Ceph cluster(s) %s declare no static OSD device path", ReclaimDevicesAll, strings.Join(e.Clusters, ", "))
}

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
		for _, host := range sc.Spec.Ceph.Topology.Nodes {
			for _, dev := range topology.OSDHostAllStaticDevices(sc, host) {
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

func ReclaimDevicesRequestsAll(devices string) bool {
	entries := SplitReclaimDevices(devices)
	return len(entries) == 1 && entries[0] == ReclaimDevicesAll
}

func ValidateReclaimDevicesFlag(devices string) error {
	entries := SplitReclaimDevices(devices)
	if len(entries) < 2 {
		return nil
	}
	for _, entry := range entries {
		if entry == ReclaimDevicesAll {
			return &ReclaimDevicesMixedAllError{Entries: append([]string(nil), entries...)}
		}
	}
	return nil
}

func ResolveReclaimDevices(state v1alpha1.State, owned []string, devices string) (string, error) {
	if !ReclaimDevicesRequestsAll(devices) || len(owned) == 0 {
		return devices, nil
	}
	declared := DeclaredOwnedOSDDevices(state, owned)
	if len(declared) == 0 {
		return "", reclaimAllNoneDeclaredError(state, owned)
	}
	return strings.Join(declared, ","), nil
}

func reclaimAllNoneDeclaredError(state v1alpha1.State, owned []string) error {
	return &ReclaimAllNoDeclaredDevicesError{
		Clusters:                   append([]string(nil), owned...),
		EffectiveUnboundedClusters: allDevicesSelectionClusters(state, owned),
	}
}

func allDevicesSelectionClusters(state v1alpha1.State, owned []string) []string {
	ownedSet := map[string]bool{}
	for _, name := range owned {
		ownedSet[name] = true
	}
	var out []string
	for _, sc := range state.StorageClusters {
		if ownedSet[sc.Metadata.Name] && sc.Spec.Ceph != nil && topology.ClusterHasAllDevicesOSDHost(sc) {
			out = append(out, sc.Metadata.Name)
		}
	}
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
