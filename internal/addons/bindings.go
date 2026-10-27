package addons

import (
	"fmt"
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
	if owner.Spec().Get("profileRefs").Len()+owner.Spec().Get("addonRefs").Len()+owner.Spec().Get("packages").Len() == 0 {
		return nil, []api.Issue{addonIssue("api.invariant", "spec", "the binding selects no profile, add-on or package", "add a package to spec.packages, a profile to spec.profileRefs or an add-on to spec.addonRefs")}, false
	}
	type frame struct {
		object api.Object
		refs   []string
		next   int
		root   int
	}
	stack := []frame{{object: owner, refs: owner.Spec().Get("profileRefs").Strings(), root: -1}}
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
			root := current.root
			if root < 0 {
				root = current.next
			}
			current.next++
			if active[name] {
				complete = false
				issues = append(issues, addonIssue("api.invariant", indexed("spec.profileRefs", root), "profile expansion returns to ClusterAddonProfile/"+name, "remove the reference to "+name+" from this list or from a profile it expands"))
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
				if current.object.Name() != owner.Name() {
					issues = append(issues, addonIssue("api.reference", indexed("spec.profileRefs", root), "profile "+owner.Spec().Get("profileRefs").Strings()[root]+" expands to ClusterAddonProfile/"+name+", which is not in the selected input", "declare ClusterAddonProfile/"+name+", add its file to the selection, or remove it from ClusterAddonProfile/"+current.object.Name()))
				}
				continue
			}
			active[name] = true
			stack = append(stack, frame{object: profile, refs: profile.Spec().Get("profileRefs").Strings(), root: root})
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
			if !c.Undecodable(api.ClusterAddon, name) && current.root >= 0 {
				issues = append(issues, addonIssue("api.reference", indexed("spec.profileRefs", current.root), "profile "+owner.Spec().Get("profileRefs").Strings()[current.root]+" expands to ClusterAddon/"+name+", which is not in the selected input", "declare ClusterAddon/"+name+", add its file to the selection, or remove it from ClusterAddonProfile/"+current.object.Name()))
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
			issues = append(issues, addonIssue("api.reference", indexed("spec.addonConfigs", i)+".addonRef", "ClusterAddon/"+config.Get("addonRef").Text()+" is configured but not selected by this binding", "select "+config.Get("addonRef").Text()+" through spec.addonRefs or a profile, or remove this spec.addonConfigs entry"))
		}
	}
	for _, addon := range selected {
		config := configsByName[addon.Name()]
		configField := "spec.addonConfigs"
		for i, candidate := range configs.Items() {
			if candidate.Get("addonRef").Text() == addon.Name() {
				configField = indexed(configField, i) + ".inputs"
				break
			}
		}
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
					issues = append(issues, addonIssue("api.required", configField, "required input "+accepted.Get("name").Text()+" of ClusterAddon/"+addon.Name()+" is not supplied", "add an input named "+accepted.Get("name").Text()+" to the spec.addonConfigs entry of "+addon.Name()+", naming a "+inputKind(accepted)))
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
				issues = append(issues, addonIssue("api.reference", inputField(configField, inputs, accepted.Get("name").Text())+".value", fmt.Sprintf("input %s of ClusterAddon/%s names %s, which is not a %s in the selected input", accepted.Get("name").Text(), addon.Name(), value, inputKind(accepted)), "set the value to the name of a "+inputKind(accepted)+" declared in the selected input"))
			}
		}
		for j, supplied := range inputs.Items() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			if _, declared := acceptedByName[supplied.Get("name").Text()]; !declared {
				issues = append(issues, addonIssue("api.field", indexed(configField, j)+".name", "ClusterAddon/"+addon.Name()+" declares no input named "+supplied.Get("name").Text(), undeclaredRemedy(addon)))
			}
		}
	}
	cluster := binding.Spec().Get("clusterRef").Text()
	for i, ref := range binding.Spec().Get("afterRefs").Strings() {
		if other, found := c.Find(api.ClusterAddonBinding, ref); found && other.Spec().Get("clusterRef").Text() != cluster {
			issues = append(issues, addonIssue("api.invariant", indexed("spec.afterRefs", i), "spec.afterRefs names ClusterAddonBinding/"+ref+", which binds OpenShiftCluster/"+other.Spec().Get("clusterRef").Text()+", not OpenShiftCluster/"+cluster, "name a binding of OpenShiftCluster/"+cluster+", or remove "+ref+" from spec.afterRefs"))
		}
	}
	return boundedIssues(issues)
}

type Attachment struct{ ClusterRef, ConsumerRef string }

// StorageAttachments returns only relationships declared by selected typed
// storage inputs and their effects. It does not interpret arbitrary strings.
func StorageAttachments(c api.Catalog) []Attachment {
	seen := map[Attachment]bool{}
	profiles, definitions := uniqueObjects(c.OfKind(api.ClusterAddonProfile)), uniqueObjects(c.OfKind(api.ClusterAddon))
	for _, binding := range c.OfKind(api.ClusterAddonBinding) {
		if ref := binding.Spec().Get("packages", "fdf", "cephConsumerRef"); ref.Type() == api.String && ref.Text() != "" {
			seen[Attachment{ClusterRef: binding.Spec().Get("clusterRef").Text(), ConsumerRef: ref.Text()}] = true
		}
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
				if !unique || input.Get("resourceKind").Text() != string(api.CephConsumer) || input.Has("secretType") || !hasEffect(input, "cephConsumerAttachment") {
					continue
				}
				value, supplied := suppliedInputs[input.Get("name").Text()]
				if supplied && value.Get("value").Type() == api.String && value.Get("value").Text() != "" {
					seen[Attachment{ClusterRef: binding.Spec().Get("clusterRef").Text(), ConsumerRef: value.Get("value").Text()}] = true
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
		return strings.Compare(a.ConsumerRef, b.ConsumerRef)
	})
	return out
}

func inputKind(v api.Value) string {
	if v.Has("secretType") {
		return "Secret of type " + v.Get("secretType").Text()
	}
	return v.Get("resourceKind").Text()
}
func inputField(base string, inputs api.Value, name string) string {
	for i, v := range inputs.Items() {
		if v.Get("name").Text() == name {
			return indexed(base, i)
		}
	}
	return base
}
func undeclaredRemedy(addon api.Object) string {
	names := []string{}
	for _, v := range addon.Spec().Get("inputs").Items() {
		names = append(names, v.Get("name").Text())
	}
	slices.Sort(names)
	if len(names) == 0 {
		return "remove the input"
	}
	suffix := ""
	if len(names) > 8 {
		suffix = fmt.Sprintf(" and %d more", len(names)-8)
		names = names[:8]
	}
	return "remove the input, or use an input " + addon.Name() + " declares: " + strings.Join(names, ", ") + suffix
}
