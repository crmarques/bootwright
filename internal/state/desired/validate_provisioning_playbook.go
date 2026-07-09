package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var provisioningTokenRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var provisioningControllerGroups = map[string]bool{
	"localhost":                   true,
	"127.0.0.1":                   true,
	"bootwright_ocp_hosts":        true,
	"bootwright_controller_hosts": true,
}

func validateProvisioningPlaybooks(state v1alpha1.State) []string {
	var errs []string
	machines := indexMachines(state.Machines)
	containers := indexContainerClusters(state.ContainerClusters)
	storage := indexStorageClusters(state.StorageClusters)

	for _, p := range state.ProvisioningPlaybooks {
		prefix := fmt.Sprintf("ProvisioningPlaybook/%s spec", p.Metadata.Name)
		if msg := validateName(v1alpha1.KindProvisioningPlaybook, p.Metadata.Name); msg != "" {
			errs = append(errs, msg)
		}
		if !slices.Contains(v1alpha1.ProvisioningStages(), p.Spec.Stage) {
			errs = append(errs, fmt.Sprintf("%s.stage %q must be one of %s", prefix, p.Spec.Stage, strings.Join(v1alpha1.ProvisioningStages(), ", ")))
		}
		if p.Spec.Timing != "" && p.Spec.Timing != v1alpha1.ProvisioningPlaybookTimingBefore && p.Spec.Timing != v1alpha1.ProvisioningPlaybookTimingAfter {
			errs = append(errs, fmt.Sprintf("%s.timing %q must be %q or %q", prefix, p.Spec.Timing, v1alpha1.ProvisioningPlaybookTimingBefore, v1alpha1.ProvisioningPlaybookTimingAfter))
		}
		if p.Spec.Run != "" && p.Spec.Run != v1alpha1.ProvisioningPlaybookRunOnChange && p.Spec.Run != v1alpha1.ProvisioningPlaybookRunAlways {
			errs = append(errs, fmt.Sprintf("%s.run %q must be %q or %q", prefix, p.Spec.Run, v1alpha1.ProvisioningPlaybookRunOnChange, v1alpha1.ProvisioningPlaybookRunAlways))
		}
		if p.Spec.FailureMode != "" && p.Spec.FailureMode != v1alpha1.ProvisioningPlaybookFailureFail && p.Spec.FailureMode != v1alpha1.ProvisioningPlaybookFailureContinue {
			errs = append(errs, fmt.Sprintf("%s.failureMode %q must be %q or %q", prefix, p.Spec.FailureMode, v1alpha1.ProvisioningPlaybookFailureFail, v1alpha1.ProvisioningPlaybookFailureContinue))
		}

		baseDir := filepath.Dir(p.SourcePath)
		errs = append(errs, validateContainedFile(prefix+".playbook", baseDir, p.Spec.Playbook, true)...)
		if p.Spec.RolesPath != "" {
			errs = append(errs, validateContainedDir(prefix+".rolesPath", baseDir, p.Spec.RolesPath)...)
		}
		if p.Spec.CollectionsPath != "" {
			errs = append(errs, validateContainedDir(prefix+".collectionsPath", baseDir, p.Spec.CollectionsPath)...)
		}

		errs = append(errs, validateProvisioningPlaybookTarget(prefix, p, machines, containers, storage)...)
	}

	errs = append(errs, validateProvisioningPlaybookOrdering(state.ProvisioningPlaybooks)...)
	return errs
}

func validateProvisioningPlaybookTarget(prefix string, p v1alpha1.ProvisioningPlaybook, machines map[string]v1alpha1.Machine, containers map[string]v1alpha1.ContainerCluster, storage map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	t := p.Spec.Target
	if len(t.Clusters) == 0 && len(t.Machines) == 0 && len(t.HostGroups) == 0 {
		errs = append(errs, fmt.Sprintf("%s.target must select at least one of clusters, machines, or hostGroups", prefix))
	}
	for i, name := range t.Clusters {
		_, isContainer := containers[name]
		_, isStorage := storage[name]
		if !isContainer && !isStorage {
			errs = append(errs, fmt.Sprintf("%s.target.clusters[%d] %q does not match any ContainerCluster or StorageCluster", prefix, i, name))
		}
	}
	for i, name := range t.Machines {
		if _, ok := machines[name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.target.machines[%d] %q does not match any Machine", prefix, i, name))
		}
	}
	for i, group := range t.HostGroups {
		if strings.TrimSpace(group) == "" {
			errs = append(errs, fmt.Sprintf("%s.target.hostGroups[%d] is empty", prefix, i))
			continue
		}
		if provisioningControllerGroups[group] {
			errs = append(errs, fmt.Sprintf("%s.target.hostGroups[%d] %q targets the bootwright controller/localhost, which is not allowed", prefix, i, group))
			continue
		}
		if !provisioningTokenRe.MatchString(group) {
			errs = append(errs, fmt.Sprintf("%s.target.hostGroups[%d] %q is not a valid inventory group name", prefix, i, group))
		}
	}
	return errs
}

func validateProvisioningPlaybookOrdering(playbooks []v1alpha1.ProvisioningPlaybook) []string {
	var errs []string
	type bucketKey struct{ stage, timing string }
	buckets := map[bucketKey][]v1alpha1.ProvisioningPlaybook{}
	for _, p := range playbooks {
		if !v1alpha1.ProvisioningPlaybookIsEnabled(p) {
			continue
		}
		key := bucketKey{p.Spec.Stage, v1alpha1.ProvisioningPlaybookTiming(p)}
		buckets[key] = append(buckets[key], p)
	}
	for _, bucket := range buckets {
		provider := map[string]string{}
		for _, p := range bucket {
			for _, cap := range p.Spec.Provides {
				if cap == "" {
					continue
				}
				if owner, dup := provider[cap]; dup {
					errs = append(errs, fmt.Sprintf("ProvisioningPlaybook/%s spec.provides %q is already provided by ProvisioningPlaybook/%s in the same stage/timing", p.Metadata.Name, cap, owner))
					continue
				}
				provider[cap] = p.Metadata.Name
			}
		}
		for _, p := range bucket {
			for _, cap := range p.Spec.Requires {
				if _, ok := provider[cap]; !ok {
					errs = append(errs, fmt.Sprintf("ProvisioningPlaybook/%s spec.requires %q is not provided by any playbook in the same stage/timing", p.Metadata.Name, cap))
				}
			}
		}
		errs = append(errs, provisioningOrderingCycles(bucket, provider)...)
	}
	return errs
}

func provisioningOrderingCycles(bucket []v1alpha1.ProvisioningPlaybook, provider map[string]string) []string {
	byName := map[string]v1alpha1.ProvisioningPlaybook{}
	for _, p := range bucket {
		byName[p.Metadata.Name] = p
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var reported bool
	var errs []string
	var visit func(name string)
	visit = func(name string) {
		if color[name] == black || reported {
			return
		}
		color[name] = grey
		for _, cap := range byName[name].Spec.Requires {
			dep, ok := provider[cap]
			if !ok || dep == name {
				continue
			}
			if color[dep] == grey {
				errs = append(errs, fmt.Sprintf("ProvisioningPlaybook provides/requires form a cycle involving %q in the same stage/timing", name))
				reported = true
				return
			}
			visit(dep)
			if reported {
				return
			}
		}
		color[name] = black
	}
	for _, p := range bucket {
		visit(p.Metadata.Name)
		if reported {
			break
		}
	}
	return errs
}

func validateContainedFile(owner, baseDir, value string, requireYAML bool) []string {
	clean, errs := validateContainedPath(owner, value)
	if len(errs) > 0 || clean == "" {
		return errs
	}
	if requireYAML && !isYAMLFile(clean) {
		return []string{fmt.Sprintf("%s %q is not a .yaml or .yml file", owner, value)}
	}
	info, err := os.Lstat(filepath.Join(baseDir, clean))
	if err != nil {
		return []string{fmt.Sprintf("%s %q does not exist: %v", owner, value, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return []string{fmt.Sprintf("%s %q must not be a symlink", owner, value)}
	}
	if info.IsDir() {
		return []string{fmt.Sprintf("%s %q must name a file, got directory", owner, value)}
	}
	return nil
}

func validateContainedDir(owner, baseDir, value string) []string {
	clean, errs := validateContainedPath(owner, value)
	if len(errs) > 0 || clean == "" {
		return errs
	}
	if base := filepath.Base(clean); base == "vendor" || base == "node_modules" {
		return []string{fmt.Sprintf("%s %q must not be named vendor or node_modules (context init skips those directories)", owner, value)}
	}
	info, err := os.Lstat(filepath.Join(baseDir, clean))
	if err != nil {
		return []string{fmt.Sprintf("%s %q does not exist: %v", owner, value, err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return []string{fmt.Sprintf("%s %q must not be a symlink", owner, value)}
	}
	if !info.IsDir() {
		return []string{fmt.Sprintf("%s %q must name a directory", owner, value)}
	}
	return nil
}

func validateContainedPath(owner, value string) (string, []string) {
	if strings.TrimSpace(value) == "" {
		return "", []string{owner + " is required"}
	}
	if strings.TrimSpace(value) != value {
		return "", []string{fmt.Sprintf("%s %q must not contain leading or trailing whitespace", owner, value)}
	}
	if filepath.IsAbs(value) {
		return "", []string{fmt.Sprintf("%s %q must be relative to the ProvisioningPlaybook file", owner, value)}
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", []string{fmt.Sprintf("%s %q must stay within the ProvisioningPlaybook file directory", owner, value)}
	}
	return clean, nil
}
