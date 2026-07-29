package desiredstate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var provisioningTokenRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

var provisioningControllerGroups = map[string]bool{
	"localhost":                   true,
	"127.0.0.1":                   true,
	"bootwright_ocp_hosts":        true,
	"bootwright_controller_hosts": true,
}

func validateCustomPlaybooks(state v1alpha1.State) []string {
	var errs []string
	machines := indexMachines(state.Machines)
	containers := indexContainerClusters(state.ContainerClusters)
	storage := indexStorageClusters(state.StorageClusters)
	secrets := indexSecrets(state.Secrets)

	for _, p := range state.CustomPlaybooks {
		prefix := fmt.Sprintf("CustomPlaybook/%s spec", p.Metadata.Name)
		if msg := validateName(v1alpha1.KindCustomPlaybook, p.Metadata.Name); msg != "" {
			errs = append(errs, msg)
		}
		errs = append(errs, validateCustomPlaybookAnchor(prefix, p)...)
		if p.Spec.Run != "" && p.Spec.Run != v1alpha1.PlaybookRunOnChange && p.Spec.Run != v1alpha1.PlaybookRunAlways {
			errs = append(errs, fmt.Sprintf("%s.run %q must be %q or %q", prefix, p.Spec.Run, v1alpha1.PlaybookRunOnChange, v1alpha1.PlaybookRunAlways))
		}
		if p.Spec.OnFailure != "" && p.Spec.OnFailure != v1alpha1.PlaybookFailureFail && p.Spec.OnFailure != v1alpha1.PlaybookFailureContinue {
			errs = append(errs, fmt.Sprintf("%s.onFailure %q must be %q or %q", prefix, p.Spec.OnFailure, v1alpha1.PlaybookFailureFail, v1alpha1.PlaybookFailureContinue))
		}
		errs = append(errs, validateCustomPlaybookTimeout(prefix, p)...)

		errs = append(errs, validatePlaybookSource(prefix, p.Spec.Source, secrets)...)
		if v1alpha1.PlaybookSourceIsSet(p.Spec.Source) {
			errs = append(errs, validateSourcedContent(prefix, p.Spec.Source, p.Spec.Playbook, p.Spec.RolesPath, p.Spec.CollectionsPath)...)
		} else {
			baseDir := filepath.Dir(p.SourcePath)
			errs = append(errs, validateContainedFile(prefix+".playbook", baseDir, p.Spec.Playbook, true)...)
			if p.Spec.Playbook != "" && !hookPathHasSegment(p.Spec.Playbook, "playbooks") {
				errs = append(errs, fmt.Sprintf("%s.playbook %q must live under a playbooks/ directory so the loader does not parse it as desired state", prefix, p.Spec.Playbook))
			}
			if p.Spec.RolesPath != "" {
				errs = append(errs, validateContainedDir(prefix+".rolesPath", baseDir, p.Spec.RolesPath)...)
			}
			if p.Spec.CollectionsPath != "" {
				errs = append(errs, validateContainedDir(prefix+".collectionsPath", baseDir, p.Spec.CollectionsPath)...)
			}
		}

		errs = append(errs, validateCustomPlaybookTags(prefix, p)...)
		errs = append(errs, validateReservedExtraVars(prefix+".extraVars", p.Spec.ExtraVars)...)
		errs = append(errs, validateCustomPlaybookTarget(prefix, p, machines, containers, storage)...)
	}

	errs = append(errs, validateCustomPlaybookOrdering(state.CustomPlaybooks)...)
	return errs
}

func validateCustomPlaybookTimeout(prefix string, p v1alpha1.CustomPlaybook) []string {
	if p.Spec.Timeout == "" {
		return nil
	}
	d, err := time.ParseDuration(p.Spec.Timeout)
	if err != nil {
		return []string{fmt.Sprintf("%s.timeout %q is not a valid duration", prefix, p.Spec.Timeout)}
	}
	if d <= 0 {
		return []string{fmt.Sprintf("%s.timeout %q must be greater than 0", prefix, p.Spec.Timeout)}
	}
	return nil
}

func validateCustomPlaybookAnchor(prefix string, p v1alpha1.CustomPlaybook) []string {
	anchors := strings.Join(v1alpha1.CustomPlaybookAnchors(), ", ")
	switch {
	case p.Spec.Gates != "" && p.Spec.Follows != "":
		return []string{fmt.Sprintf("%s must set exactly one of gates or follows, not both", prefix)}
	case p.Spec.Gates == "" && p.Spec.Follows == "":
		return []string{fmt.Sprintf("%s must set one of gates or follows to one of %s", prefix, anchors)}
	}
	anchor, gating := v1alpha1.CustomPlaybookAnchor(p)
	key := "follows"
	if gating {
		key = "gates"
	}
	if !slices.Contains(v1alpha1.CustomPlaybookAnchors(), anchor) {
		return []string{fmt.Sprintf("%s.%s %q must be one of %s", prefix, key, anchor, anchors)}
	}
	if gating && p.Spec.OnFailure == v1alpha1.PlaybookFailureContinue {
		return []string{fmt.Sprintf("%s.gates cannot be combined with onFailure continue: a gate that lets the stage proceed on failure is not a gate; use follows instead", prefix)}
	}
	return nil
}

func validateCustomPlaybookTags(prefix string, p v1alpha1.CustomPlaybook) []string {
	var errs []string
	selected := map[string]bool{}
	for _, field := range []struct {
		name string
		list []string
	}{
		{"tags", p.Spec.Tags},
		{"skipTags", p.Spec.SkipTags},
	} {
		seen := map[string]bool{}
		for i, tag := range field.list {
			switch {
			case strings.TrimSpace(tag) == "":
				errs = append(errs, fmt.Sprintf("%s.%s[%d] is empty", prefix, field.name, i))
				continue
			case strings.TrimSpace(tag) != tag:
				errs = append(errs, fmt.Sprintf("%s.%s[%d] %q must not contain leading or trailing whitespace", prefix, field.name, i, tag))
				continue
			case !provisioningTokenRe.MatchString(tag):
				errs = append(errs, fmt.Sprintf("%s.%s[%d] %q is not a valid Ansible tag; tags are joined with commas, so each must be a single token", prefix, field.name, i, tag))
				continue
			case seen[tag]:
				errs = append(errs, fmt.Sprintf("%s.%s[%d] %q is listed twice", prefix, field.name, i, tag))
				continue
			}
			seen[tag] = true
			if field.name == "tags" {
				selected[tag] = true
			} else if selected[tag] {
				errs = append(errs, fmt.Sprintf("%s lists %q in both tags and skipTags, so nothing it marks would ever run", prefix, tag))
			}
		}
	}
	return errs
}

func validateCustomPlaybookTarget(prefix string, p v1alpha1.CustomPlaybook, machines map[string]v1alpha1.Machine, containers map[string]v1alpha1.ContainerCluster, storage map[string]v1alpha1.StorageCluster) []string {
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

func validateCustomPlaybookOrdering(playbooks []v1alpha1.CustomPlaybook) []string {
	var errs []string
	type bucketKey struct {
		anchor string
		gating bool
	}
	buckets := map[bucketKey][]v1alpha1.CustomPlaybook{}
	for _, p := range playbooks {
		if !v1alpha1.CustomPlaybookIsEnabled(p) {
			continue
		}
		anchor, gating := v1alpha1.CustomPlaybookAnchor(p)
		key := bucketKey{anchor, gating}
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
					errs = append(errs, fmt.Sprintf("CustomPlaybook/%s spec.provides %q is already provided by CustomPlaybook/%s at the same anchor", p.Metadata.Name, cap, owner))
					continue
				}
				provider[cap] = p.Metadata.Name
			}
		}
		for _, p := range bucket {
			for _, cap := range p.Spec.Requires {
				if _, ok := provider[cap]; !ok {
					errs = append(errs, fmt.Sprintf("CustomPlaybook/%s spec.requires %q is not provided by any playbook at the same anchor", p.Metadata.Name, cap))
				}
			}
		}
		errs = append(errs, provisioningOrderingCycles(bucket, provider)...)
	}
	return errs
}

func provisioningOrderingCycles(bucket []v1alpha1.CustomPlaybook, provider map[string]string) []string {
	byName := map[string]v1alpha1.CustomPlaybook{}
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
				errs = append(errs, fmt.Sprintf("CustomPlaybook provides/requires form a cycle involving %q at the same anchor", name))
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
		return "", []string{fmt.Sprintf("%s %q must be a relative path", owner, value)}
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", []string{fmt.Sprintf("%s %q must stay within the directory of the file that declares it", owner, value)}
	}
	return clean, nil
}
