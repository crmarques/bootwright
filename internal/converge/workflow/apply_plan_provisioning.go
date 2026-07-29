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
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func RemovePlaybookRecordsForClusters(runsDir string, state v1alpha1.State, tasks []ApplyTask, clusters []string) []error {
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
		if task.Entry.Kind != ApplyTaskKindPlaybook {
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

func planPlaybookActivities(graph *ActivityGraph, state v1alpha1.State, phaseSet map[string]bool, target ApplyTarget) error {
	playbooks := selectedPlaybooks(state)
	if len(playbooks) == 0 {
		return nil
	}
	index := phaseTaskIndex(graph)
	containers, storage := clusterKindSets(state)

	for _, p := range playbooks {
		anchor, gating := v1alpha1.CustomPlaybookAnchor(p)
		if !phaseSet[anchor] {
			continue
		}
		limit, orderClusters, fleetWide, inScope, err := resolveProvisioningTarget(state, target, p, containers, storage)
		if err != nil {
			return err
		}
		if !inScope {
			continue
		}
		id := "playbook." + p.Metadata.Name
		contentRoot, resolved := playbookContentRoot(p, target.GitSourceRoots)
		if !resolved {
			continue
		}
		hashVars, err := provisioningDesiredHashVars(p, contentRoot)
		if err != nil {
			return fmt.Errorf("playbook/%s: %w; fix or remove the unreadable content so bootwright can prove what would run", p.Metadata.Name, err)
		}
		extraVarPairs, err := provisioningExtraVarPairs(p, anchor, gating)
		if err != nil {
			return err
		}

		activity := Activity{
			ID:       id,
			Provides: provisioningProvidesCapabilities(p, anchor, gating),
			Requires: provisioningRequiresCapabilities(p, anchor, gating),
			Task: ApplyTask{
				Entry: TaskLedgerEntry{
					ID:      id,
					Kind:    ApplyTaskKindPlaybook,
					Label:   provisioningPlaybookLabel(p, anchor, gating),
					Cluster: provisioningPlaybookCluster(p, orderClusters),
					Status:  TaskStatusPending,
				},
				Playbook:          provisioningPlaybookPath(p, contentRoot),
				Limit:             limit,
				RolesPath:         provisioningVendoredPath(contentRoot, p.Spec.RolesPath),
				CollectionsPath:   provisioningVendoredPath(contentRoot, p.Spec.CollectionsPath),
				ExtraVarPairs:     extraVarPairs,
				Tags:              p.Spec.Tags,
				SkipTags:          p.Spec.SkipTags,
				Timeout:           provisioningPlaybookTimeout(p),
				State:             state,
				DesiredHashVars:   hashVars,
				SkipWhenConverged: v1alpha1.CustomPlaybookRunMode(p) == v1alpha1.PlaybookRunOnChange,
			},
		}

		anchorIDs := phaseTaskIDsInScope(index, anchor, orderClusters, fleetWide)
		if !gating {
			activity.ExplicitDependencies = append(activity.ExplicitDependencies, anchorIDs...)
		} else if prev, ok := previousProvisioningStage(anchor); ok {
			activity.ExplicitDependencies = append(activity.ExplicitDependencies, phaseTaskIDsInScope(index, prev, orderClusters, fleetWide)...)
		}
		if err := graph.Add(activity); err != nil {
			return err
		}
		if gating {
			for _, anchorID := range anchorIDs {
				if err := graph.AddDependency(anchorID, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func selectedPlaybooks(state v1alpha1.State) []v1alpha1.CustomPlaybook {
	out := make([]v1alpha1.CustomPlaybook, 0, len(state.CustomPlaybooks))
	for _, p := range state.CustomPlaybooks {
		if v1alpha1.CustomPlaybookIsEnabled(p) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		anchorA, gatingA := v1alpha1.CustomPlaybookAnchor(a)
		anchorB, gatingB := v1alpha1.CustomPlaybookAnchor(b)
		if anchorA != anchorB {
			return anchorA < anchorB
		}
		if gatingA != gatingB {
			return gatingA && !gatingB
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
	case ApplyTaskKindMachineInfraPrepare, ApplyTaskKindClusterInstall, ApplyTaskKindMachineInfraFinalize, ApplyTaskKindManagedMachineOS, ApplyTaskKindStorageNodeAccess, ApplyTaskKindMachineRegistration, ApplyTaskKindMachineRepositories:
		return ApplyPhaseMachines, true
	case ApplyTaskKindStorageInfra, ApplyTaskKindClusterISO, ApplyTaskKindHostVirtctl:
		return ApplyPhaseDeps, true
	case ApplyTaskKindStorageCluster, ApplyTaskKindNodeBoot, ApplyTaskKindBootstrapWait, ApplyTaskKindInstallWait:
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
	stages := v1alpha1.CustomPlaybookAnchors()
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

func resolveProvisioningTarget(state v1alpha1.State, target ApplyTarget, p v1alpha1.CustomPlaybook, containers, storage map[string]bool) (limit string, orderClusters []string, fleetWide bool, inScope bool, err error) {
	var tokens []string
	var unresolvedMachines []string
	deferredByScope := false
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
		case storage[name]:
			if !storageClusterSelectedForTarget(target, name) {
				deferredByScope = true
				continue
			}
			tokens = append(tokens, render.StorageClusterGroupName(name))
			addCluster(name)
		}
	}
	for _, name := range p.Spec.Target.Machines {
		hosts := render.MachineInventoryHosts(state, name)
		if len(hosts) == 0 {
			unresolvedMachines = append(unresolvedMachines, name)
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
	if len(unresolvedMachines) > 0 {
		return "", nil, false, false, fmt.Errorf(
			"playbook/%s spec.target.machines %s resolve to no inventory host because they are not nodes of any container cluster or managed storage cluster; target them with spec.target.hostGroups instead, or remove them",
			p.Metadata.Name, strings.Join(unresolvedMachines, ", "))
	}
	tokens = dedupeStrings(tokens)
	if len(tokens) == 0 {
		if deferredByScope {
			return "", nil, false, false, nil
		}
		return "", nil, false, false, fmt.Errorf(
			"playbook/%s spec.target resolves to no hosts; refusing to run it with an empty --limit, which would target every host in the fleet",
			p.Metadata.Name)
	}
	if target.MachineScoped() {
		tokens, orderClusters = narrowProvisioningTargetToMachines(state, target, tokens, orderClusters)
		if len(tokens) == 0 {
			return "", nil, false, false, nil
		}
	}
	return strings.Join(tokens, ":"), orderClusters, fleetWide, true, nil
}

func narrowProvisioningTargetToMachines(state v1alpha1.State, target ApplyTarget, tokens, orderClusters []string) ([]string, []string) {
	hostMachines := provisioningInventoryHostMachines(state)
	groups := render.HostGroupMembers(state)
	var hosts []string
	seenHost := map[string]bool{}
	selectedMachines := map[string]bool{}
	for _, host := range limitTargetHosts(strings.Join(tokens, ":"), groups) {
		if seenHost[host] || !provisioningHostInScope(target, host, hostMachines[host]) {
			continue
		}
		seenHost[host] = true
		if machine := hostMachines[host]; machine != "" {
			selectedMachines[machine] = true
		}
		hosts = append(hosts, host)
	}
	if len(hosts) == 0 {
		return nil, nil
	}
	return hosts, provisioningClustersOwningMachines(state, orderClusters, selectedMachines)
}

func provisioningHostInScope(target ApplyTarget, host, machine string) bool {
	if machine != "" && target.MachineIncluded(machine) {
		return true
	}
	return target.FabricHosts != nil && target.FabricHosts[host]
}

func provisioningInventoryHostMachines(state v1alpha1.State) map[string]string {
	out := map[string]string{}
	for _, machine := range state.Machines {
		name := machine.Metadata.Name
		out[name] = name
		for _, host := range render.MachineInventoryHosts(state, name) {
			out[host] = name
		}
	}
	return out
}

func provisioningClustersOwningMachines(state v1alpha1.State, clusters []string, machines map[string]bool) []string {
	if len(clusters) == 0 {
		return nil
	}
	owning := map[string]bool{}
	for machine := range machines {
		for _, owner := range machineOwningClusters(state, machine) {
			owning[owner] = true
		}
	}
	out := make([]string, 0, len(clusters))
	for _, name := range clusters {
		if owning[name] {
			out = append(out, name)
		}
	}
	return out
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

func provisioningPlaybookTimeout(p v1alpha1.CustomPlaybook) time.Duration {
	d, err := time.ParseDuration(v1alpha1.CustomPlaybookTimeout(p))
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

func provisioningPlaybookLabel(p v1alpha1.CustomPlaybook, anchor string, gating bool) string {
	return "playbook " + p.Metadata.Name + " (" + playbookAnchorKey(gating) + " " + anchor + ")"
}

func playbookAnchorKey(gating bool) string {
	if gating {
		return "gates"
	}
	return "follows"
}

func provisioningPlaybookCluster(p v1alpha1.CustomPlaybook, orderClusters []string) string {
	if len(orderClusters) == 1 && len(p.Spec.Target.HostGroups) == 0 {
		return orderClusters[0]
	}
	return ""
}

func playbookContentRoot(p v1alpha1.CustomPlaybook, gitRoots map[string]string) (string, bool) {
	if v1alpha1.PlaybookSourceIsGit(p.Spec.Source) {
		root, ok := gitRoots[GitSourceRootKeyForPlaybook(p.Metadata.Name)]
		return root, ok
	}
	if v1alpha1.PlaybookSourceIsSet(p.Spec.Source) {
		return p.Spec.Source.Path, true
	}
	return filepath.Dir(p.SourcePath), true
}

func GitSourceRootKeyForPlaybook(name string) string { return "playbook/" + name }

func GitSourceRootKeyForStep(addon, step string) string { return "step/" + addon + "/" + step }

func provisioningPlaybookPath(p v1alpha1.CustomPlaybook, root string) string {
	return filepath.Join(root, p.Spec.Playbook)
}

func provisioningVendoredPath(root, rel string) string {
	if strings.TrimSpace(rel) == "" {
		return ""
	}
	return filepath.Join(root, rel)
}

func provisioningExtraVarPairs(p v1alpha1.CustomPlaybook, anchor string, gating bool) ([]string, error) {
	pairs := []string{
		"bootwright_playbook_name=" + p.Metadata.Name,
		"bootwright_playbook_anchor=" + anchor,
		"bootwright_playbook_anchor_key=" + playbookAnchorKey(gating),
	}
	if len(p.Spec.ExtraVars) > 0 {
		data, err := json.Marshal(p.Spec.ExtraVars)
		if err != nil {
			return nil, fmt.Errorf("playbook/%s spec.extraVars cannot be encoded: %w", p.Metadata.Name, err)
		}
		pairs = append(pairs, string(data))
	}
	return pairs, nil
}

func provisioningProvidesCapabilities(p v1alpha1.CustomPlaybook, anchor string, gating bool) []CapabilityRef {
	return provisioningCapabilities(p.Spec.Provides, anchor, playbookAnchorKey(gating))
}

func provisioningRequiresCapabilities(p v1alpha1.CustomPlaybook, anchor string, gating bool) []CapabilityRef {
	return provisioningCapabilities(p.Spec.Requires, anchor, playbookAnchorKey(gating))
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
	Anchor          string                        `json:"anchor"`
	Gates           bool                          `json:"gates"`
	Run             string                        `json:"run"`
	OnFailure       string                        `json:"onFailure"`
	Playbook        string                        `json:"playbook"`
	RolesPath       string                        `json:"rolesPath,omitempty"`
	CollectionsPath string                        `json:"collectionsPath,omitempty"`
	Tags            []string                      `json:"tags,omitempty"`
	SkipTags        []string                      `json:"skipTags,omitempty"`
	Order           int                           `json:"order,omitempty"`
	Provides        []string                      `json:"provides,omitempty"`
	Requires        []string                      `json:"requires,omitempty"`
	Target          v1alpha1.CustomPlaybookTarget `json:"target"`
	ExtraVars       map[string]any                `json:"extraVars,omitempty"`
	SecretRefs      []string                      `json:"secretRefs,omitempty"`
	ContentDigest   string                        `json:"contentDigest,omitempty"`
}

func provisioningDesiredHashVars(p v1alpha1.CustomPlaybook, contentRoot string) (provisioningPlaybookHashVars, error) {
	secretNames := make([]string, 0, len(p.Spec.SecretRefs))
	for _, ref := range p.Spec.SecretRefs {
		secretNames = append(secretNames, ref.Name)
	}
	digest, err := provisioningContentDigest(p, contentRoot)
	if err != nil {
		return provisioningPlaybookHashVars{}, err
	}
	anchor, gating := v1alpha1.CustomPlaybookAnchor(p)
	return provisioningPlaybookHashVars{
		Anchor:          anchor,
		Gates:           gating,
		Run:             v1alpha1.CustomPlaybookRunMode(p),
		OnFailure:       v1alpha1.CustomPlaybookFailureMode(p),
		Playbook:        p.Spec.Playbook,
		RolesPath:       p.Spec.RolesPath,
		CollectionsPath: p.Spec.CollectionsPath,
		Tags:            p.Spec.Tags,
		SkipTags:        p.Spec.SkipTags,
		Order:           p.Spec.Order,
		Provides:        p.Spec.Provides,
		Requires:        p.Spec.Requires,
		Target:          p.Spec.Target,
		ExtraVars:       p.Spec.ExtraVars,
		SecretRefs:      secretNames,
		ContentDigest:   digest,
	}, nil
}

func provisioningContentDigest(p v1alpha1.CustomPlaybook, contentRoot string) (string, error) {
	base := contentRoot
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
