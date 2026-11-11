package addons

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// ExpandBinding returns definitions in authored depth-first selection order,
// retaining the first occurrence of each add-on.
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
	_, issues, _ := expandIndexed(binding, c, profiles, definitions)
	cluster := binding.Spec().Get("clusterRef").Text()
	for i, ref := range binding.Spec().Get("afterRefs").Strings() {
		if other, found := c.Find(api.ClusterAddonBinding, ref); found && other.Spec().Get("clusterRef").Text() != cluster {
			issues = append(issues, addonIssue("api.invariant", indexed("spec.afterRefs", i), "spec.afterRefs names ClusterAddonBinding/"+ref+", which binds OpenShiftCluster/"+other.Spec().Get("clusterRef").Text()+", not OpenShiftCluster/"+cluster, "name a binding of OpenShiftCluster/"+cluster+", or remove "+ref+" from spec.afterRefs"))
		}
	}
	return boundedIssues(issues)
}

type Attachment struct{ ClusterRef, ConsumerRef string }

func StorageAttachments(c api.Catalog, r Registry) []Attachment {
	seen := map[Attachment]bool{}
	for _, binding := range c.OfKind(api.ClusterAddonBinding) {
		for _, use := range r.consumption(binding) {
			seen[Attachment{ClusterRef: binding.Spec().Get("clusterRef").Text(), ConsumerRef: use.Name}] = true
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
