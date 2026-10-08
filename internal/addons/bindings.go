package addons

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// ExpandBinding returns definitions in authored depth-first selection order,
// retaining the first occurrence of each add-on. Configurations select nothing.
func ExpandBinding(binding api.Object, c api.Catalog) ([]api.Object, []api.Issue) {
	if binding.Kind() != api.ClusterAddonBinding {
		return nil, nil
	}
	return expandSelection(binding, c)
}

func expandSelection(owner api.Object, c api.Catalog) ([]api.Object, []api.Issue) {
	selected, issues, _ := expandIndexed(owner, c, uniqueObjects(c.OfKind(api.ClusterAddonProfile)), uniqueObjects(c.OfKind(api.ClusterAddon)))
	return selected, issues
}

// expandIndexed reports whether the expansion is complete. A name of a
// document that failed decoding leaves it incomplete with no refusal of its
// own, because that document's diagnostics are the only ones.
func expandIndexed(owner api.Object, c api.Catalog, profiles, definitions map[string]api.Object) ([]api.Object, []api.Issue, bool) {
	if owner.Spec().Get("profileRefs").Len()+owner.Spec().Get("addonRefs").Len() == 0 {
		return nil, []api.Issue{addonIssue("api.invariant", "spec", "At least one profile or direct add-on must be selected.", "add a profile to spec.profileRefs or an add-on to spec.addonRefs")}, false
	}
	type frame struct {
		object api.Object
		refs   []string
		next   int
	}
	stack := []frame{{object: owner, refs: owner.Spec().Get("profileRefs").Strings()}}
	active, done, seen := map[string]bool{}, map[string]bool{}, map[string]bool{}
	if owner.Kind() == api.ClusterAddonProfile {
		active[owner.Name()] = true
	}
	result := []api.Object{}
	issues := []api.Issue{}
	complete := true
	for len(stack) != 0 {
		if len(issues) >= maxIssues {
			return result, issues[:maxIssues], false
		}
		current := &stack[len(stack)-1]
		if current.next < len(current.refs) {
			name := current.refs[current.next]
			current.next++
			if active[name] {
				complete = false
				issues = append(issues, addonIssue("api.invariant", "spec.profileRefs", "Add-on profile expansion contains a cycle.", "remove the profile reference that leads back to a profile already being expanded"))
				continue
			}
			if done[name] {
				continue
			}
			profile, found := profiles[name]
			if !found {
				complete = false
				if c.Undecodable(api.ClusterAddonProfile, name) {
					continue
				}
				issues = append(issues, addonIssue("api.reference", "spec.profileRefs", "A selected add-on profile does not resolve uniquely.", "declare each profile named in spec.profileRefs, and in the profiles it expands, exactly once as a ClusterAddonProfile, or remove the name"))
				continue
			}
			active[name] = true
			stack = append(stack, frame{object: profile, refs: profile.Spec().Get("profileRefs").Strings()})
			continue
		}
		for _, name := range current.object.Spec().Get("addonRefs").Strings() {
			if len(issues) >= maxIssues {
				return result, issues[:maxIssues], false
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			if addon, found := definitions[name]; found {
				result = append(result, addon)
				continue
			}
			complete = false
			if !c.Undecodable(api.ClusterAddon, name) {
				issues = append(issues, addonIssue("api.reference", "spec.addonRefs", "A selected add-on does not resolve uniquely.", "declare each add-on named in spec.addonRefs, and in the profiles this selection expands, exactly once as a ClusterAddon, or remove the name"))
			}
		}
		if current.object.Kind() == api.ClusterAddonProfile {
			delete(active, current.object.Name())
			done[current.object.Name()] = true
		}
		stack = stack[:len(stack)-1]
	}
	return result, issues, complete
}

func uniqueObjects(objects []api.Object) map[string]api.Object {
	unique := map[string]api.Object{}
	duplicates := map[string]bool{}
	for _, object := range objects {
		if _, exists := unique[object.Name()]; exists {
			duplicates[object.Name()] = true
		}
		unique[object.Name()] = object
	}
	for name := range duplicates {
		delete(unique, name)
	}
	return unique
}

func validateBinding(binding api.Object, c api.Catalog) []api.Issue {
	profiles, definitions := uniqueObjects(c.OfKind(api.ClusterAddonProfile)), uniqueObjects(c.OfKind(api.ClusterAddon))
	selected, issues, complete := expandIndexed(binding, c, profiles, definitions)
	selectedByName := uniqueObjects(selected)
	configs := binding.Spec().Get("addonConfigs")
	configsByName := uniqueNamedValues(configs, "addonRef")
	for i, config := range configs.Items() {
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		if _, found := selectedByName[config.Get("addonRef").Text()]; complete && !found {
			issues = append(issues, addonIssue("api.reference", indexed("spec.addonConfigs", i)+".addonRef", "Add-on configuration must name an expanded selection; it cannot select an add-on.", "select the add-on through spec.addonRefs or a profile, or remove this spec.addonConfigs entry"))
		}
	}
	for _, addon := range selected {
		config := configsByName[addon.Name()]
		inputs := config.Get("inputs")
		inputsByName := uniqueNamedValues(inputs, "name")
		acceptedByName := uniqueNamedValues(addon.Spec().Get("inputs"), "name")
		for _, accepted := range addon.Spec().Get("inputs").Items() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			supplied, present := inputsByName[accepted.Get("name").Text()]
			if !present {
				if !accepted.Has("required") || accepted.Get("required").Bool() {
					issues = append(issues, addonIssue("api.required", "spec.addonConfigs", "Every required add-on input must be supplied exactly once.", "supply each required input of the selected add-on once in its spec.addonConfigs entry"))
				}
				continue
			}
			kind := api.Kind(accepted.Get("resourceKind").Text())
			if accepted.Has("secretType") {
				kind = api.Secret
			}
			value := supplied.Get("value").Text()
			target, found := c.Find(kind, value)
			if !found && c.Undecodable(kind, value) {
				continue
			}
			if !found || kind == api.Secret && target.Spec().Get("type").Text() != accepted.Get("secretType").Text() {
				issues = append(issues, addonIssue("api.reference", "spec.addonConfigs", "A supplied add-on input does not resolve to its declared resource or Secret type.", "set the input's value to the name of a declared object of the input's resourceKind, or of a Secret of its secretType"))
			}
		}
		for _, supplied := range inputs.Items() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			if _, declared := acceptedByName[supplied.Get("name").Text()]; !declared {
				issues = append(issues, addonIssue("api.field", "spec.addonConfigs", "A supplied input is not declared by the selected add-on.", "remove the input, or use an input name the add-on declares in its spec.inputs"))
			}
		}
	}
	clusterSelected := slices.Clone(selected)
	clusterSeen := uniqueObjects(selected)
	clusterComplete := complete
	for _, other := range c.OfKind(api.ClusterAddonBinding) {
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		if other.Name() == binding.Name() || other.Spec().Get("clusterRef").Text() != binding.Spec().Get("clusterRef").Text() {
			continue
		}
		// Each other binding reports its own selection diagnostics.
		expanded, _, otherComplete := expandIndexed(other, c, profiles, definitions)
		clusterComplete = clusterComplete && otherComplete
		duplicate := false
		for _, addon := range expanded {
			if _, found := selectedByName[addon.Name()]; found {
				duplicate = true
			}
			if _, found := clusterSeen[addon.Name()]; !found {
				clusterSelected = append(clusterSelected, addon)
				clusterSeen[addon.Name()] = addon
			}
		}
		if duplicate {
			issues = append(issues, addonIssue("api.duplicate", "spec.clusterRef", "An add-on can be bound to a cluster only once after profile expansion.", "bind each add-on to the cluster through one ClusterAddonBinding only"))
		}
	}
	if clusterComplete {
		issues = append(issues, validateCapabilities(clusterSelected)...)
	}
	return boundedIssues(issues)
}

func validateCapabilities(selected []api.Object) []api.Issue {
	providers := map[string][]int{}
	for i, addon := range selected {
		for _, capability := range addon.Spec().Get("provides").Strings() {
			providers[capability] = append(providers[capability], i)
		}
	}
	issues := []api.Issue{}
	remaining := make([]int, len(selected))
	consumers := make([][]int, len(selected))
	for i, addon := range selected {
		dependencies := map[int]bool{}
		for _, requirement := range addon.Spec().Get("requires").Strings() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			found := false
			for _, provider := range providers[requirement] {
				if provider != i {
					found, dependencies[provider] = true, true
				}
			}
			if !found {
				issues = append(issues, addonIssue("api.reference", "spec", "A required capability must be provided by another selected add-on.", "select an add-on that provides the required capability, or remove the add-on that requires it"))
			}
		}
		for provider := range dependencies {
			remaining[i]++
			consumers[provider] = append(consumers[provider], i)
		}
	}
	queue := []int{}
	for i, count := range remaining {
		if count == 0 {
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, consumer := range consumers[queue[head]] {
			remaining[consumer]--
			if remaining[consumer] == 0 {
				queue = append(queue, consumer)
			}
		}
	}
	if len(queue) != len(selected) {
		issues = append(issues, addonIssue("api.invariant", "spec", "Selected add-on capability requirements form a cycle.", "remove a requires entry from one of the selected add-ons to break the cycle"))
	}
	return boundedIssues(issues)
}

type Attachment struct{ ClusterRef, ExportRef string }

// StorageAttachments returns only relationships declared by selected typed
// storage inputs and their effects. It does not interpret arbitrary strings.
func StorageAttachments(c api.Catalog) []Attachment {
	seen := map[Attachment]bool{}
	profiles, definitions := uniqueObjects(c.OfKind(api.ClusterAddonProfile)), uniqueObjects(c.OfKind(api.ClusterAddon))
	for _, binding := range c.OfKind(api.ClusterAddonBinding) {
		// Validation owns selection diagnostics; this projection grants no effects.
		selected, _, _ := expandIndexed(binding, c, profiles, definitions)
		configsByName := uniqueNamedValues(binding.Spec().Get("addonConfigs"), "addonRef")
		for _, addon := range selected {
			if !slices.Contains(addon.Spec().Get("provides").Strings(), "dataFoundation") {
				continue
			}
			config, found := configsByName[addon.Name()]
			if !found {
				continue
			}
			declaredInputs := uniqueNamedValues(addon.Spec().Get("inputs"), "name")
			suppliedInputs := uniqueNamedValues(config.Get("inputs"), "name")
			for _, input := range addon.Spec().Get("inputs").Items() {
				_, unique := declaredInputs[input.Get("name").Text()]
				if !unique || input.Get("resourceKind").Text() != string(api.StorageExport) || input.Has("secretType") || !hasEffect(input, "storageExportAttachment") {
					continue
				}
				value, supplied := suppliedInputs[input.Get("name").Text()]
				if supplied && value.Get("value").Type() == api.String && value.Get("value").Text() != "" {
					seen[Attachment{ClusterRef: binding.Spec().Get("clusterRef").Text(), ExportRef: value.Get("value").Text()}] = true
				}
			}
		}
	}
	out := make([]Attachment, 0, len(seen))
	for attachment := range seen {
		out = append(out, attachment)
	}
	slices.SortFunc(out, func(a, b Attachment) int {
		if result := strings.Compare(a.ClusterRef, b.ClusterRef); result != 0 {
			return result
		}
		return strings.Compare(a.ExportRef, b.ExportRef)
	})
	return out
}
