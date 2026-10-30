package addons

import (
	"fmt"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type Provider struct{ Binding, Package, Addon string }
type SelectedInstance struct {
	Package   Package
	Instance  Instance
	Providers []Provider
}
type SelectionIssue struct {
	Binding string
	Issue   api.Issue
}
type selectedMember struct {
	binding, name, field string
	pkg                  *Package
	config               api.Value
	provides, requires   []string
	afterRefs            []string
	last                 bool
}

func (m selectedMember) subject() string {
	if m.pkg != nil {
		return "package " + m.name
	}
	return "ClusterAddon/" + m.name
}

func Instances(c api.Catalog, r Registry) ([]SelectedInstance, []SelectionIssue) {
	bindings := c.OfKind(api.ClusterAddonBinding)
	slices.SortFunc(bindings, func(a, b api.Object) int { return strings.Compare(a.Name(), b.Name()) })
	clusters := map[string][]selectedMember{}
	ordered := map[string][]api.Object{}
	for _, b := range bindings {
		cluster := b.Spec().Get("clusterRef").Text()
		ordered[cluster] = append(ordered[cluster], b)
	}
	incomplete := map[string]bool{}
	issues := []SelectionIssue{}
	for _, b := range bindings {
		cluster := b.Spec().Get("clusterRef").Text()
		selected, _, complete := expandIndexed(b, c, uniqueObjects(c.OfKind(api.ClusterAddonProfile)), uniqueObjects(c.OfKind(api.ClusterAddon)))
		if !complete {
			incomplete[cluster] = true
		}
		for _, a := range selected {
			clusters[cluster] = append(clusters[cluster], selectedMember{binding: b.Name(), name: a.Name(), field: selectionField(b, a.Name(), c), provides: a.Spec().Get("provides").Strings(), requires: a.Spec().Get("requires").Strings(), afterRefs: b.Spec().Get("afterRefs").Strings()})
		}
		for _, f := range b.Spec().Get("packages").Fields() {
			p, ok := r.Find(f.Name)
			if !ok {
				continue
			}
			req := slices.Clone(p.Requires)
			if p.RequiresFor != nil {
				req = append(req, p.RequiresFor(Instance{Cluster: cluster, Binding: b.Name(), Config: f.Value})...)
			}
			clusters[cluster] = append(clusters[cluster], selectedMember{binding: b.Name(), name: p.Name, field: "spec.packages." + p.Name, pkg: &p, config: f.Value, provides: p.Provides, requires: req, afterRefs: b.Spec().Get("afterRefs").Strings(), last: p.Last})
		}
	}
	names := []string{}
	for n := range clusters {
		names = append(names, n)
	}
	slices.Sort(names)
	result := []SelectedInstance{}
	for _, cluster := range names {
		if incomplete[cluster] {
			continue
		}
		members := clusters[cluster]
		slices.SortStableFunc(members, func(a, b selectedMember) int {
			if n := strings.Compare(a.binding, b.binding); n != 0 {
				return n
			}
			return strings.Compare(a.name, b.name)
		})
		add := func(m selectedMember, code, message, remedy string) {
			issue := addonIssue("api.invariant", m.field, message, remedy)
			switch code {
			case "api.reference":
				issue.Code = "api.reference"
			case "api.duplicate":
				issue.Code = "api.duplicate"
			}
			issues = append(issues, SelectionIssue{Binding: m.binding, Issue: issue})
		}
		validateMemberUniqueness(cluster, members, add)
		after, cycle := authoredOrder(cluster, ordered[cluster])
		if cycle != nil {
			issues = append(issues, *cycle)
		}
		result = append(result, orderMembers(cluster, members, after, add)...)
	}
	return result, issues
}
func selectionField(b api.Object, name string, c api.Catalog) string {
	for i, profile := range b.Spec().Get("profileRefs").Strings() {
		p, ok := c.Find(api.ClusterAddonProfile, profile)
		if !ok {
			continue
		}
		selected, _ := expandSelection(p, c)
		for _, a := range selected {
			if a.Name() == name {
				return indexed("spec.profileRefs", i)
			}
		}
	}
	for i, a := range b.Spec().Get("addonRefs").Strings() {
		if a == name {
			return indexed("spec.addonRefs", i)
		}
	}
	return "spec"
}
func NormalizePackages(o api.Object, r Registry) (api.Object, []api.Issue) {
	if o.Kind() != api.ClusterAddonBinding {
		return o, nil
	}
	spec := o.Spec()
	configs := spec.Get("packages")
	for _, f := range configs.Fields() {
		p, ok := r.Find(f.Name)
		if !ok {
			continue
		}
		v := f.Value.Default("timeout", api.StringValue(p.Budget.String()))
		if p.OLM != nil {
			if sub := v.Get("subscription"); sub.Present() {
				source := p.OLM.DefaultSource
				if catalog := sub.Get("catalogSource"); catalog.Present() {
					source = catalog.Get("name").Text()
					catalog = catalog.Default("securityContextConfig", api.StringValue("restricted"))
					if image := catalog.Get("image"); image.Present() {
						catalog = catalog.With("image", api.StringValue(api.CanonicalImage(image.Text())))
					}
					sub = sub.With("catalogSource", catalog)
				}
				if source != "" {
					sub = sub.Default("source", api.StringValue(source))
				}
				v = v.With("subscription", sub.Default("sourceNamespace", api.StringValue("openshift-marketplace")))
			}
		}
		if p.Name == "metallb" {
			pools := v.Get("pools").Items()
			for i := range pools {
				pools[i] = pools[i].Default("autoAssign", api.BoolValue(true))
			}
			v = v.With("pools", sortedNamed(api.ListValue(pools...), "name"))
		}
		if p.Normalize != nil {
			v = p.Normalize(v)
		}
		configs = configs.With(f.Name, v)
	}
	if configs.Present() {
		spec = spec.With("packages", configs)
	}
	return o.WithSpec(spec), nil
}
func ValidatePackages(o api.Object, c api.Catalog, r Registry, facts ClusterFacts) []api.Issue {
	if o.Kind() != api.ClusterAddonBinding {
		return nil
	}
	issues := []api.Issue{}
	if ref := o.Spec().Get("packages", "fdf", "cephConsumerRef").Text(); ref != "" {
		bindings := c.OfKind(api.ClusterAddonBinding)
		slices.SortFunc(bindings, func(a, b api.Object) int { return strings.Compare(a.Name(), b.Name()) })
		for _, earlier := range bindings {
			if earlier.Name() >= o.Name() {
				break
			}
			if earlier.Spec().Get("packages", "fdf", "cephConsumerRef").Text() == ref {
				issues = append(issues, api.Issue{Code: "api.duplicate", Field: "$.spec.packages.fdf.cephConsumerRef", Message: "CephConsumer/" + ref + " is already consumed by package fdf of ClusterAddonBinding/" + earlier.Name(), Remediation: "give each consuming cluster its own CephConsumer"})
				break
			}
		}
	}
	_, selection := Instances(c, r)
	for _, i := range selection {
		if i.Binding == o.Name() {
			issues = append(issues, i.Issue)
		}
	}
	for _, f := range o.Spec().Get("packages").Fields() {
		p, ok := r.Find(f.Name)
		if !ok {
			continue
		}
		base := "spec.packages." + f.Name
		add := func(field, message, remedy string) {
			issues = append(issues, addonIssue("api.value", base+"."+field, message, remedy))
		}
		v := f.Value
		budget, err := time.ParseDuration(v.Get("timeout").Text())
		if err != nil || budget <= 0 || budget > 5*time.Hour+50*time.Minute {
			add("timeout", "package "+p.Name+" timeout must be positive and at most 5h50m", "set timeout to a positive duration no greater than 5h50m")
		} else if budget%time.Second != 0 {
			add("timeout", "package "+p.Name+" timeout must be a whole number of seconds", "set timeout to a whole number of seconds, such as 90s")
		}
		if p.OLM != nil {
			sub := v.Get("subscription")
			source := sub.Get("source").Text()
			if source == "" {
				add("subscription.source", "package "+p.Name+" has no catalog source", "name the catalog in subscription.source")
			}
			if catalog := sub.Get("catalogSource"); catalog.Present() {
				name := catalog.Get("name").Text()
				if slices.Contains([]string{"redhat-operators", "certified-operators", "community-operators", "redhat-marketplace"}, name) {
					add("subscription.catalogSource.name", "owned catalog "+name+" has the name of a default catalog", "choose a catalog name other than a default catalog")
				}
				if source != name {
					add("subscription.source", "subscription source differs from owned catalog "+name, "set subscription.source to "+name)
				}
				if sub.Get("sourceNamespace").Text() != "openshift-marketplace" {
					add("subscription.sourceNamespace", "owned catalogs belong to openshift-marketplace", "set subscription.sourceNamespace to openshift-marketplace")
				}
				if poll := catalog.Get("pollInterval"); poll.Present() {
					d, e := time.ParseDuration(poll.Text())
					if e != nil || d <= 0 {
						add("subscription.catalogSource.pollInterval", "catalog poll interval must be positive", "set pollInterval to a positive duration")
					}
				}
			}
		}
		if p.Admit != nil {
			for _, issue := range p.Admit(Instance{Cluster: facts.Name, Binding: o.Name(), Config: v}, facts) {
				relative := strings.TrimPrefix(issue.Field, "$")
				if relative != "" && !strings.HasPrefix(relative, ".") && !strings.HasPrefix(relative, "[") {
					relative = "." + relative
				}
				issue.Field = "$." + base + relative
				issues = append(issues, issue)
			}
		}
	}
	return boundedIssues(issues)
}

func ValidateWithRegistry(o api.Object, c api.Catalog, r Registry, facts ClusterFacts) []api.Issue {
	if o.Kind() != api.ClusterAddonBinding {
		return Validate(o, c)
	}
	issues := validateBinding(o, c)
	return boundedIssues(append(issues, ValidatePackages(o, c, r, facts)...))
}

func validateMemberUniqueness(cluster string, members []selectedMember, add selectionReporter) {
	seen := map[string]int{}
	last := -1
	for i, m := range members {
		key := m.subject()
		if previous, ok := seen[key]; ok {
			first := members[previous]
			remedy := fmt.Sprintf("bind %s to %s through one ClusterAddonBinding only", m.name, cluster)
			if m.pkg != nil {
				remedy = fmt.Sprintf("select package %s for %s in one ClusterAddonBinding only", m.name, cluster)
			}
			add(m, "api.duplicate", fmt.Sprintf("%s is already bound to OpenShiftCluster/%s by ClusterAddonBinding/%s", key, cluster, first.binding), remedy)
		} else {
			seen[key] = i
		}
		if m.last {
			if last >= 0 {
				add(m, "api.invariant", fmt.Sprintf("packages %s and %s both run last on OpenShiftCluster/%s", members[last].name, m.name, cluster), fmt.Sprintf("select one of %s and %s for %s", members[last].name, m.name, cluster))
			}
			last = i
		}
	}
}

type selectionReporter func(selectedMember, string, string, string)

func authoredOrder(cluster string, bindings []api.Object) (map[string][]string, *SelectionIssue) {
	after := map[string][]string{}
	names := []string{}
	for _, b := range bindings {
		names = append(names, b.Name())
	}
	for _, b := range bindings {
		for _, ref := range b.Spec().Get("afterRefs").Strings() {
			if slices.Contains(names, ref) {
				after[b.Name()] = append(after[b.Name()], ref)
			}
		}
	}
	reaches := func(from, to string) bool {
		seen := map[string]bool{}
		queue := slices.Clone(after[from])
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if current == to {
				return true
			}
			if !seen[current] {
				seen[current] = true
				queue = append(queue, after[current]...)
			}
		}
		return false
	}
	for _, b := range bindings {
		first := b.Name()
		if !reaches(first, first) {
			continue
		}
		cycle := []string{}
		for _, name := range names {
			if name == first || reaches(first, name) && reaches(name, first) {
				cycle = append(cycle, "ClusterAddonBinding/"+name)
			}
		}
		refs := b.Spec().Get("afterRefs").Strings()
		at := slices.IndexFunc(refs, func(ref string) bool {
			return ref == first || reaches(ref, first)
		})
		list := strings.Join(cycle, ", ")
		issue := addonIssue("api.invariant", indexed("spec.afterRefs", at), fmt.Sprintf("spec.afterRefs of the bindings of OpenShiftCluster/%s form a cycle through %s", cluster, list), "remove one spec.afterRefs entry among "+list+" to break the cycle")
		return map[string][]string{}, &SelectionIssue{Binding: first, Issue: issue}
	}
	return after, nil
}

func orderMembers(cluster string, members []selectedMember, after map[string][]string, add selectionReporter) []SelectedInstance {
	result := []SelectedInstance{}
	deps := make([]map[int]bool, len(members))
	authored := make([]map[int]bool, len(members))
	remaining := make([]int, len(members))
	done := make([]bool, len(members))
	for i, m := range members {
		deps[i] = map[int]bool{}
		authored[i] = map[int]bool{}
		for _, token := range m.requires {
			found := false
			for j, p := range members {
				if i != j && slices.Contains(p.provides, token) {
					deps[i][j] = true
					found = true
				}
			}
			if !found {
				add(m, "api.reference", fmt.Sprintf("%s requires capability %s, which nothing else selected for OpenShiftCluster/%s provides", m.subject(), token, cluster), fmt.Sprintf("select a package or add-on that provides %s for OpenShiftCluster/%s, or remove %s from the selection", token, cluster, m.subject()))
			}
		}
		if m.last {
			for j := range members {
				if j != i {
					deps[i][j] = true
				}
			}
		}
		for j, p := range members {
			if j != i && !deps[i][j] && slices.Contains(after[m.binding], p.binding) {
				deps[i][j], authored[i][j] = true, true
			}
		}
		remaining[i] = len(deps[i])
	}
	for count := 0; count < len(members); count++ {
		next := -1
		for i := range members {
			if !done[i] && remaining[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			derived := make([]map[int]bool, len(members))
			for i := range members {
				derived[i] = map[int]bool{}
				for j := range deps[i] {
					if !authored[i][j] {
						derived[i][j] = true
					}
				}
			}
			if first, cycle := cycleMembers(members, derived, done); first >= 0 {
				list := strings.Join(cycle, ", ")
				add(members[first], "api.invariant", fmt.Sprintf("capability requirements of the packages and add-ons selected for OpenShiftCluster/%s form a cycle through %s", cluster, list), "remove one requires entry among "+list+" to break the cycle")
				break
			}
			first, cycle := cycleMembers(members, deps, done)
			list := strings.Join(cycle, ", ")
			subject := members[first]
		search:
			for i, m := range members {
				for j, p := range members {
					if authored[i][j] && slices.Contains(cycle, m.subject()) && slices.Contains(cycle, p.subject()) {
						subject = m
						subject.field = indexed("spec.afterRefs", slices.Index(m.afterRefs, p.binding))
						break search
					}
				}
			}
			add(subject, "api.invariant", fmt.Sprintf("ordering of the packages and add-ons selected for OpenShiftCluster/%s forms a cycle through %s once spec.afterRefs applies", cluster, list), "remove one spec.afterRefs or requires entry among "+list+" to break the cycle")
			break
		}
		done[next] = true
		m := members[next]
		if m.pkg != nil {
			providers := []Provider{}
			for j, p := range members {
				if deps[next][j] {
					v := Provider{Binding: p.binding}
					if p.pkg == nil {
						v.Addon = p.name
					} else {
						v.Package = p.name
					}
					providers = append(providers, v)
				}
			}
			result = append(result, SelectedInstance{Package: clonePackage(*m.pkg), Instance: Instance{Cluster: cluster, Binding: m.binding, Config: m.config}, Providers: providers})
		}
		for i := range members {
			if deps[i][next] {
				remaining[i]--
			}
		}
	}
	return result
}

func cycleMembers(members []selectedMember, deps []map[int]bool, done []bool) (int, []string) {
	reaches := func(start, target int) bool {
		seen := map[int]bool{}
		queue := []int{}
		for j := range members {
			if deps[start][j] {
				queue = append(queue, j)
			}
		}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if current == target {
				return true
			}
			if seen[current] {
				continue
			}
			seen[current] = true
			for j := range members {
				if deps[current][j] {
					queue = append(queue, j)
				}
			}
		}
		return false
	}
	first := -1
	cycle := []string{}
	for i, m := range members {
		if done[i] || !reaches(i, i) {
			continue
		}
		if first < 0 {
			first = i
		}
		if reaches(first, i) && reaches(i, first) {
			cycle = append(cycle, m.subject())
		}
	}
	return first, cycle
}
