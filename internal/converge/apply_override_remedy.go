package converge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

type ApplyOverrideProtectionReason string

const (
	ApplyOverrideProtectionEnvironment ApplyOverrideProtectionReason = "environment"
	ApplyOverrideProtectionKinds       ApplyOverrideProtectionReason = "protected-kinds"
	ApplyOverrideProtectionManagedRHSM ApplyOverrideProtectionReason = "managed-rhsm"
)

type ApplyOverrideDestroyProtectionError struct {
	Reason                ApplyOverrideProtectionReason
	Destructive           []string
	ProtectedEnvironments []string
	ManagedRHSMClusters   []string
	MachineLayer          []string
	ClusterLayer          []string
	MachineRoots          []string
	ClusterRoots          []string
}

func (e *ApplyOverrideDestroyProtectionError) Error() string {
	switch e.Reason {
	case ApplyOverrideProtectionEnvironment:
		return fmt.Sprintf("apply --mode rebuild would destructively rebuild protected resource(s) %s in Environment %s; protected destruction must cross the explicit destroy boundary before re-applying (drifted reconfigure-only services do not trip this — align their desired state or let --mode rebuild reconcile them in place)", strings.Join(e.Destructive, ", "), strings.Join(e.ProtectedEnvironments, ", "))
	case ApplyOverrideProtectionManagedRHSM:
		protectedLayers := "protected machine-layer destruction"
		if len(e.ClusterLayer) > 0 {
			protectedLayers = fmt.Sprintf("protected machine-layer destruction and protected cluster-layer work %s", strings.Join(e.ClusterLayer, ", "))
		}
		return fmt.Sprintf("apply --mode rebuild would reimage managed-RHSM storage node(s) of Ceph cluster(s) %s in place, stranding their Satellite registration so the reused host DMI UUID blocks re-registration; %s must cross the explicit destroy boundary before re-applying — destroy unregisters the node from RHSM before wiping it", strings.Join(e.ManagedRHSMClusters, ", "), protectedLayers)
	default:
		return fmt.Sprintf("apply --mode rebuild would destructively rebuild %s, protected by spec.safety.protectedKinds; protected destruction must cross the explicit destroy boundary before re-applying (drifted reconfigure-only services do not trip this)", strings.Join(e.Destructive, ", "))
	}
}

func (e *ApplyOverrideDestroyProtectionError) Remedy() remedy.Request {
	var targets []remedy.Target
	if len(e.MachineLayer) > 0 {
		targets = append(targets, remedy.Target{Role: remedy.TargetRoleMachineLayer})
		for _, root := range sortedUniqueNonempty(e.MachineRoots) {
			targets = append(targets, remedy.Target{Role: remedy.TargetRoleMachineLayerRoot, Name: root})
		}
	}
	if len(e.ClusterLayer) > 0 {
		targets = append(targets, remedy.Target{Role: remedy.TargetRoleClusterLayer})
		for _, root := range sortedUniqueNonempty(e.ClusterRoots) {
			targets = append(targets, remedy.Target{Role: remedy.TargetRoleClusterLayerRoot, Name: root})
		}
	}
	return remedy.Request{Action: remedy.ActionDestroyProtectedLayersThenRebuildSameSelection, Targets: targets}
}

func sortedUniqueNonempty(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
