package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func RemoveProvisioningPlaybookRecordsForClusters(runsDir string, state v1alpha1.State, tasks []ApplyTask, clusters []string) []error {
	if len(clusters) == 0 || strings.TrimSpace(runsDir) == "" {
		return nil
	}
	rebuiltMachines := map[string]bool{}
	for _, name := range clusters {
		for _, machine := range clusterMachineNames(state, name) {
			rebuiltMachines[machine] = true
		}
	}
	if len(rebuiltMachines) == 0 {
		return nil
	}
	var problems []error
	for _, task := range tasks {
		if task.Entry.Kind != ApplyTaskKindProvisioningPlaybook {
			continue
		}
		targets := limitTargetHosts(task.Limit, render.HostGroupMembers(task.State))
		hostMachines := inventoryHostMachineNames(task.State)
		intersects := strings.TrimSpace(task.Limit) == ""
		for _, host := range targets {
			if machine := hostMachines[host]; machine != "" && rebuiltMachines[machine] {
				intersects = true
				break
			}
		}
		if !intersects {
			continue
		}
		if err := RemoveApplyTaskConvergeSafety(runsDir, task); err != nil {
			problems = append(problems, fmt.Errorf("reset provisioning playbook record %s after machine rebuild: %w", task.Entry.ID, err))
		}
	}
	return problems
}

func clusterMachineNames(state v1alpha1.State, cluster string) []string {
	var out []string
	for _, ocp := range state.ContainerClusters {
		if ocp.Metadata.Name != cluster {
			continue
		}
		if ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp); ok {
			for _, machine := range ci.Machines {
				out = append(out, machine.Name)
			}
		}
	}
	for _, sc := range state.StorageClusters {
		if sc.Metadata.Name != cluster || sc.Spec.Ceph == nil {
			continue
		}
		for _, host := range sc.Spec.Ceph.Topology.Nodes {
			if host.MachineRef.Name != "" {
				out = append(out, host.MachineRef.Name)
			}
		}
	}
	return out
}

func planProvisioningPlaybookActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, target ApplyTarget) error {
	playbooks := selectedProvisioningPlaybooks(state)
	if len(playbooks) == 0 {
		return nil
	}
	index := phaseTaskIndex(graph)
	containers, storage := clusterKindSets(state)

	for _, p := range playbooks {
		stage := p.Spec.Stage
		if !phaseSet[stage] {
			continue
		}
		limit, orderClusters, fleetWide, ok := resolveProvisioningTarget(state, target, p, containers, storage)
		if !ok {
			continue
		}
		timing := v1alpha1.ProvisioningPlaybookTiming(p)
		id := "playbook." + p.Metadata.Name
		hashVars, err := provisioningDesiredHashVars(p)
		if err != nil {
			return fmt.Errorf("ProvisioningPlaybook/%s: %w; fix or remove the unreadable content so bootwright can prove what would run", p.Metadata.Name, err)
		}

		activity := Activity{
			ID:       id,
			Provides: provisioningProvidesCapabilities(p, timing),
			Requires: provisioningRequiresCapabilities(p, timing),
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:      id,
					Kind:    ApplyTaskKindProvisioningPlaybook,
					Label:   provisioningPlaybookLabel(p, stage, timing),
					Cluster: provisioningPlaybookCluster(p, orderClusters),
					Status:  TaskStatusPending,
				},
				Playbook:          provisioningPlaybookPath(p),
				Limit:             limit,
				RolesPath:         provisioningVendoredPath(p, p.Spec.RolesPath),
				CollectionsPath:   provisioningVendoredPath(p, p.Spec.CollectionsPath),
				ExtraVarPairs:     provisioningExtraVarPairs(p, stage, timing),
				State:             state,
				DesiredHashVars:   hashVars,
				SkipWhenConverged: v1alpha1.ProvisioningPlaybookRun(p) == v1alpha1.ProvisioningPlaybookRunOnChange,
			},
		}

		anchorIDs := phaseTaskIDsInScope(index, stage, orderClusters, fleetWide)
		if timing == v1alpha1.ProvisioningPlaybookTimingAfter {
			activity.ExplicitDependencies = append(activity.ExplicitDependencies, anchorIDs...)
		} else {
			if prev, ok := previousProvisioningStage(stage); ok {
				activity.ExplicitDependencies = append(activity.ExplicitDependencies, phaseTaskIDsInScope(index, prev, orderClusters, fleetWide)...)
			}
		}
		if err := graph.Add(activity); err != nil {
			return err
		}
		if timing == v1alpha1.ProvisioningPlaybookTimingBefore {
			hard := v1alpha1.ProvisioningPlaybookFailureMode(p) == v1alpha1.ProvisioningPlaybookFailureFail
			for _, anchorID := range anchorIDs {
				if hard {
					if err := graph.AddDependency(anchorID, id); err != nil {
						return err
					}
				} else {
					if err := graph.AddOrderingDependency(anchorID, id); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func selectedProvisioningPlaybooks(state v1alpha1.State) []v1alpha1.ProvisioningPlaybook {
	out := make([]v1alpha1.ProvisioningPlaybook, 0, len(state.ProvisioningPlaybooks))
	for _, p := range state.ProvisioningPlaybooks {
		if v1alpha1.ProvisioningPlaybookIsEnabled(p) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Spec.Stage != b.Spec.Stage {
			return a.Spec.Stage < b.Spec.Stage
		}
		ta, tb := v1alpha1.ProvisioningPlaybookTiming(a), v1alpha1.ProvisioningPlaybookTiming(b)
		if ta != tb {
			return ta < tb
		}
		if a.Spec.Order != b.Spec.Order {
			return a.Spec.Order < b.Spec.Order
		}
		return a.Metadata.Name < b.Metadata.Name
	})
	return out
}

func phaseTaskIndex(graph *ActivityGraph) map[string]map[string][]string {
	index := map[string]map[string][]string{}
	for _, act := range graph.ActivitySnapshot() {
		phase, ok := applyTaskKindPhase(act.Task.Entry.Kind)
		if !ok {
			continue
		}
		if index[phase] == nil {
			index[phase] = map[string][]string{}
		}
		cluster := act.Task.Entry.Cluster
		index[phase][cluster] = append(index[phase][cluster], act.ID)
	}
	return index
}

func applyTaskKindPhase(kind string) (string, bool) {
	switch kind {
	case ApplyTaskKindProvider, ApplyTaskKindInfraComponentServices:
		return ApplyPhaseFabric, true
	case ApplyTaskKindMachineInfraPrepare, ApplyTaskKindClusterInstall, ApplyTaskKindMachineInfraFinalize, ApplyTaskKindManagedMachineOS, ApplyTaskKindStorageNodeAccess, ApplyTaskKindMachineRegistration:
		return ApplyPhaseMachines, true
	case ApplyTaskKindStorageInfra, ApplyTaskKindClusterISO, ApplyTaskKindHostVirtctl:
		return ApplyPhaseDeps, true
	case ApplyTaskKindStorageCluster, ApplyTaskKindNodeBoot, ApplyTaskKindInstallWait:
		return ApplyPhaseBase, true
	case ApplyTaskKindClusterAddon, ApplyTaskKindNodeConfigApply:
		return ApplyPhaseAddons, true
	default:
		return "", false
	}
}

func phaseTaskIDsInScope(index map[string]map[string][]string, phase string, clusters []string, fleetWide bool) []string {
	byCluster := index[phase]
	if byCluster == nil {
		return nil
	}
	var ids []string
	ids = append(ids, byCluster[""]...)
	if fleetWide {
		keys := make([]string, 0, len(byCluster))
		for cluster := range byCluster {
			if cluster != "" {
				keys = append(keys, cluster)
			}
		}
		sort.Strings(keys)
		for _, cluster := range keys {
			ids = append(ids, byCluster[cluster]...)
		}
		return ids
	}
	for _, cluster := range clusters {
		ids = append(ids, byCluster[cluster]...)
	}
	return ids
}

func previousProvisioningStage(stage string) (string, bool) {
	stages := v1alpha1.ProvisioningStages()
	idx := -1
	for i, s := range stages {
		if s == stage {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return "", false
	}
	return stages[idx-1], true
}

func clusterKindSets(state v1alpha1.State) (containers, storage map[string]bool) {
	containers = map[string]bool{}
	storage = map[string]bool{}
	for _, c := range state.ContainerClusters {
		containers[c.Metadata.Name] = true
	}
	for _, c := range state.StorageClusters {
		storage[c.Metadata.Name] = true
	}
	return containers, storage
}

func resolveProvisioningTarget(state v1alpha1.State, target ApplyTarget, p v1alpha1.ProvisioningPlaybook, containers, storage map[string]bool) (limit string, orderClusters []string, fleetWide bool, ok bool) {
	var tokens []string
	clusterSet := map[string]bool{}
	addCluster := func(name string) {
		if !clusterSet[name] {
			clusterSet[name] = true
			orderClusters = append(orderClusters, name)
		}
	}
	for _, name := range p.Spec.Target.Clusters {
		switch {
		case containers[name]:
			tokens = append(tokens, render.AgentNodeGroupName(name))
			addCluster(name)
		case storage[name] && storageClusterSelectedForTarget(target, name):
			tokens = append(tokens, render.StorageClusterGroupName(name))
			addCluster(name)
		}
	}
	for _, name := range p.Spec.Target.Machines {
		hosts := render.MachineInventoryHosts(state, name)
		if len(hosts) == 0 {
			continue
		}
		tokens = append(tokens, hosts...)
		if owners := machineOwningClusters(state, name); len(owners) > 0 {
			for _, owner := range owners {
				if storage[owner] && !storageClusterSelectedForTarget(target, owner) {
					continue
				}
				addCluster(owner)
			}
		} else {
			fleetWide = true
		}
	}
	if len(p.Spec.Target.HostGroups) > 0 {
		tokens = append(tokens, p.Spec.Target.HostGroups...)
		fleetWide = true
	}
	tokens = dedupeStrings(tokens)
	if len(tokens) == 0 {
		return "", nil, false, false
	}
	return strings.Join(tokens, ":"), orderClusters, fleetWide, true
}

func machineOwningClusters(state v1alpha1.State, machine string) []string {
	var owners []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			owners = append(owners, name)
		}
	}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machine {
				add(cluster.Metadata.Name)
			}
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == machine {
				add(cluster.Metadata.Name)
			}
		}
	}
	return owners
}

func provisioningPlaybookLabel(p v1alpha1.ProvisioningPlaybook, stage, timing string) string {
	return "playbook " + p.Metadata.Name + " (" + timing + " " + stage + ")"
}

func provisioningPlaybookCluster(p v1alpha1.ProvisioningPlaybook, orderClusters []string) string {
	if len(orderClusters) == 1 && len(p.Spec.Target.HostGroups) == 0 {
		return orderClusters[0]
	}
	return ""
}

func provisioningPlaybookPath(p v1alpha1.ProvisioningPlaybook) string {
	return filepath.Join(filepath.Dir(p.SourcePath), p.Spec.Playbook)
}

func provisioningVendoredPath(p v1alpha1.ProvisioningPlaybook, rel string) string {
	if strings.TrimSpace(rel) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p.SourcePath), rel)
}

func provisioningExtraVarPairs(p v1alpha1.ProvisioningPlaybook, stage, timing string) []string {
	pairs := []string{
		"bootwright_playbook_name=" + p.Metadata.Name,
		"bootwright_playbook_stage=" + stage,
		"bootwright_playbook_timing=" + timing,
	}
	if len(p.Spec.ExtraVars) > 0 {
		if data, err := json.Marshal(p.Spec.ExtraVars); err == nil {
			pairs = append(pairs, string(data))
		}
	}
	return pairs
}

func provisioningProvidesCapabilities(p v1alpha1.ProvisioningPlaybook, timing string) []CapabilityRef {
	return provisioningCapabilities(p.Spec.Provides, p.Spec.Stage, timing)
}

func provisioningRequiresCapabilities(p v1alpha1.ProvisioningPlaybook, timing string) []CapabilityRef {
	return provisioningCapabilities(p.Spec.Requires, p.Spec.Stage, timing)
}

func provisioningCapabilities(names []string, stage, timing string) []CapabilityRef {
	out := make([]CapabilityRef, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		out = append(out, CapabilityRef{Kind: "provisioning.provides:" + name, Name: stage + "/" + timing})
	}
	return out
}

type provisioningPlaybookHashVars struct {
	Stage           string                              `json:"stage"`
	Timing          string                              `json:"timing"`
	Run             string                              `json:"run"`
	FailureMode     string                              `json:"failureMode"`
	Playbook        string                              `json:"playbook"`
	RolesPath       string                              `json:"rolesPath,omitempty"`
	CollectionsPath string                              `json:"collectionsPath,omitempty"`
	Order           int                                 `json:"order,omitempty"`
	Provides        []string                            `json:"provides,omitempty"`
	Requires        []string                            `json:"requires,omitempty"`
	Target          v1alpha1.ProvisioningPlaybookTarget `json:"target"`
	ExtraVars       map[string]any                      `json:"extraVars,omitempty"`
	SecretRefs      []string                            `json:"secretRefs,omitempty"`
	ContentDigest   string                              `json:"contentDigest,omitempty"`
}

func provisioningDesiredHashVars(p v1alpha1.ProvisioningPlaybook) (provisioningPlaybookHashVars, error) {
	secretNames := make([]string, 0, len(p.Spec.SecretRefs))
	for _, ref := range p.Spec.SecretRefs {
		secretNames = append(secretNames, ref.Name)
	}
	digest, err := provisioningContentDigest(p)
	if err != nil {
		return provisioningPlaybookHashVars{}, err
	}
	return provisioningPlaybookHashVars{
		Stage:           p.Spec.Stage,
		Timing:          v1alpha1.ProvisioningPlaybookTiming(p),
		Run:             v1alpha1.ProvisioningPlaybookRun(p),
		FailureMode:     v1alpha1.ProvisioningPlaybookFailureMode(p),
		Playbook:        p.Spec.Playbook,
		RolesPath:       p.Spec.RolesPath,
		CollectionsPath: p.Spec.CollectionsPath,
		Order:           p.Spec.Order,
		Provides:        p.Spec.Provides,
		Requires:        p.Spec.Requires,
		Target:          p.Spec.Target,
		ExtraVars:       p.Spec.ExtraVars,
		SecretRefs:      secretNames,
		ContentDigest:   digest,
	}, nil
}

func provisioningContentDigest(p v1alpha1.ProvisioningPlaybook) (string, error) {
	base := filepath.Dir(p.SourcePath)
	h := sha256.New()
	digestPath := func(rel string) error {
		if strings.TrimSpace(rel) == "" {
			return nil
		}
		root := filepath.Join(base, rel)
		if _, statErr := os.Lstat(root); errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("scan playbook content %s: %w", path, err)
			}
			if d.IsDir() {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read playbook content %s: %w", path, readErr)
			}
			relPath, _ := filepath.Rel(root, path)
			h.Write([]byte(relPath))
			h.Write([]byte{0})
			h.Write(data)
			h.Write([]byte{0})
			return nil
		})
	}
	for _, rel := range []string{p.Spec.Playbook, p.Spec.RolesPath, p.Spec.CollectionsPath} {
		if err := digestPath(rel); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func dedupeStrings(items []string) []string {
	seen := map[string]bool{}
	out := items[:0]
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
