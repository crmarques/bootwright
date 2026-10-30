package addons

import (
	"math"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

var (
	reservedGroups        = []string{"k8s.io", "kubernetes.io", "openshift.io", "coreos.com", "bootwright.io", "open-cluster-management.io", "cert-manager.io", "argoproj.io"}
	reservedLabelPrefixes = []string{"bootwright.io", "openshift.io", "kubernetes.io", "k8s.io"}
)

const (
	reservedLabelPrefix = "bootwright.io/"
	crdAPIVersion       = "apiextensions.k8s.io/v1"
	crdKind             = "CustomResourceDefinition"
	defaultCatalogRank  = -100
	minPollInterval     = time.Minute
)

func validateOLMNamespace(olm api.Value) []api.Issue {
	issues := []api.Issue{}
	if name := olm.Get("namespace", "name").Text(); reservedNamespace(name) {
		issues = append(issues, addonIssue("api.invariant", "spec.olm.namespace.name", "Add-on namespace "+name+" is reserved.", "choose a namespace other than default, openshift, openshift-* and kube-*"))
	}
	for _, label := range olm.Get("namespace", "labels").Fields() {
		if reservedLabelKey(label.Name) {
			issues = append(issues, addonIssue("api.invariant", "spec.olm.namespace.labels", "Namespace label "+label.Name+" is under a prefix that belongs to Bootwright, Kubernetes or OpenShift.", "remove the label; keys under bootwright.io/, openshift.io/, kubernetes.io/ and k8s.io/, and their subdomains, are reserved"))
		}
	}
	return issues
}

func reservedNamespace(name string) bool {
	return name == "default" || name == "openshift" || strings.HasPrefix(name, "openshift-") || strings.HasPrefix(name, "kube-")
}

func reservedLabelKey(key string) bool {
	prefix, _, found := strings.Cut(strings.ToLower(key), "/")
	if !found {
		return false
	}
	for _, reserved := range reservedLabelPrefixes {
		if prefix == reserved || strings.HasSuffix(prefix, "."+reserved) {
			return true
		}
	}
	return false
}

func validateCatalogSource(olm api.Value) []api.Issue {
	issues := []api.Issue{}
	if priority := olm.Get("catalogSource", "priority"); priority.Present() {
		if n, ok := priority.Int64(); !ok || n >= defaultCatalogRank || n < math.MinInt32 {
			issues = append(issues, addonIssue("api.value", "spec.olm.catalogSource.priority", "An authored catalog priority must be an integer below -100, the priority of redhat-operators, within 32 bits.", "set catalogSource.priority between -2147483648 and -101, so the catalog never outranks redhat-operators when OLM resolves dependencies"))
		}
	}
	if poll := olm.Get("catalogSource", "pollInterval"); poll.Present() {
		if interval, err := time.ParseDuration(poll.Text()); err == nil && interval > 0 && interval < minPollInterval {
			issues = append(issues, addonIssue("api.value", "spec.olm.catalogSource.pollInterval", "Catalog pollInterval must be at least 1m.", "set catalogSource.pollInterval to 1m or more, or remove it"))
		}
	}
	return issues
}

func validateCustomResources(olm api.Value, partial bool) []api.Issue {
	issues := []api.Issue{}
	namespace := olm.Get("namespace", "name").Text()
	subscription := effectiveSubscription(olm)
	for i, entry := range olm.Get("customResources").Items() {
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		field := indexed("spec.olm.customResources", i)
		gate := entry.Get("gate")
		if resource := entry.Get("resource"); resource.Present() {
			issues = append(issues, validateCustomResource(resource, gate, namespace, field, partial)...)
		}
		issues = append(issues, validateChecks(gate, field+".gate", namespace, subscription, true)...)
	}
	return issues
}

func validateCustomResource(resource, gate api.Value, namespace, field string, partial bool) []api.Issue {
	issues := []api.Issue{}
	field += ".resource"
	apiVersion, kind, metadata := resource.Get("apiVersion"), resource.Get("kind"), resource.Get("metadata")
	for _, member := range []string{"apiVersion", "kind"} {
		value := resource.Get(member)
		if (!partial || value.Present()) && (value.Type() != api.String || value.Text() == "") {
			issues = append(issues, addonIssue("api.value", field+"."+member, "A custom resource requires a non-empty string identity.", "set apiVersion and kind of the custom resource to non-empty strings"))
		}
	}
	name := metadata.Get("name")
	if (!partial || name.Present()) && (name.Type() != api.String || name.Text() == "") {
		issues = append(issues, addonIssue("api.value", field+".metadata.name", "A custom resource requires a non-empty string name.", "set metadata.name of the custom resource to a non-empty string"))
	}
	for _, member := range metadata.Fields() {
		switch member.Name {
		case "name", "namespace":
		case "labels", "annotations":
			for _, key := range member.Value.Fields() {
				if strings.HasPrefix(key.Name, reservedLabelPrefix) {
					issues = append(issues, addonIssue("api.invariant", field+".metadata."+member.Name, "Custom resource "+member.Name+" key "+key.Name+" is under the Bootwright-owned bootwright.io/ prefix.", "remove the key; keys under bootwright.io/ belong to Bootwright"))
				}
			}
		default:
			issues = append(issues, addonIssue("api.invariant", field+".metadata."+member.Name, "Custom resource metadata may hold only name, namespace, labels and annotations, not "+member.Name+".", "remove metadata."+member.Name+" from the custom resource"))
		}
	}
	ns := metadata.Get("namespace")
	switch {
	case !ns.Present() && !partial:
		issues = append(issues, addonIssue("api.required", field+".metadata.namespace", "A custom resource requires metadata.namespace.", "set metadata.namespace to olm.namespace.name"))
	case ns.Present() && namespace != "" && (ns.Type() != api.String || ns.Text() != namespace):
		issues = append(issues, addonIssue("api.invariant", field+".metadata.namespace", "A custom resource must live in the add-on namespace "+namespace+".", "set metadata.namespace to "+namespace))
	}
	group := ""
	if apiVersion.Type() == api.String {
		if prefix, _, found := strings.Cut(apiVersion.Text(), "/"); found {
			group = prefix
		}
		if apiVersion.Text() != "" && !validGroup(group) {
			issues = append(issues, addonIssue("api.invariant", field+".apiVersion", "Custom resource API group "+quotedGroup(group)+" is not an add-on API group.", "use a custom resource whose group has a dot and is not "+strings.Join(reservedGroups[:len(reservedGroups)-1], ", ")+" or "+reservedGroups[len(reservedGroups)-1]+", or a subdomain of one of them"))
			group = ""
		}
	}
	if kind.Type() == api.String && strings.EqualFold(kind.Text(), "Secret") {
		issues = append(issues, addonIssue("api.invariant", field+".kind", "A custom resource cannot be a Secret.", "remove the Secret custom resource; an authored add-on places no Secret"))
	}
	if group != "" && (gate.Present() || !partial) && !gateNamesCRD(gate, group) {
		issues = append(issues, addonIssue("api.invariant", strings.TrimSuffix(field, ".resource")+".gate", "The gate of a "+group+" custom resource does not prove its CustomResourceDefinition.", "add a resourceExists check of "+crdAPIVersion+" "+crdKind+" named <plural>."+group+" to the gate"))
	}
	complete := apiVersion.Type() == api.String && kind.Type() == api.String && name.Type() == api.String && ns.Type() == api.String && namespace != "" && ns.Text() == namespace
	if len(issues) == 0 && complete && apiVersion.Text() != "" && kind.Text() != "" && name.Text() != "" {
		ref := clusterobjects.Ref{APIVersion: apiVersion.Text(), Kind: kind.Text(), Namespace: ns.Text(), Name: name.Text(), Namespaced: true}
		if _, err := clusterobjects.NewObjectEntry(clusterobjects.Object{Ref: ref, Body: resource}); err != nil {
			issues = append(issues, addonIssue("api.invariant", field, "Custom resource "+kind.Text()+"/"+name.Text()+" fails the cluster object content rules: "+err.Error()+".", "remove status, managedFields, owner marks and template delimiters from the custom resource, and keep its identity consistent"))
		}
	}
	return issues
}

func validGroup(group string) bool {
	if group == "" || !strings.Contains(group, ".") {
		return false
	}
	group = strings.ToLower(group)
	for _, reserved := range reservedGroups {
		if group == reserved || strings.HasSuffix(group, "."+reserved) {
			return false
		}
	}
	return true
}

func quotedGroup(group string) string {
	if group == "" {
		return "(core)"
	}
	return group
}

func gateNamesCRD(gate api.Value, group string) bool {
	for _, check := range gate.Items() {
		exists := check.Get("resourceExists")
		if exists.Get("apiVersion").Text() != crdAPIVersion || exists.Get("kind").Text() != crdKind {
			continue
		}
		plural, found := strings.CutSuffix(exists.Get("name").Text(), "."+group)
		if found && plural != "" && !strings.Contains(plural, ".") {
			return true
		}
	}
	return false
}

func effectiveSubscription(olm api.Value) string {
	if name := olm.Get("subscription", "name").Text(); name != "" {
		return name
	}
	return olm.Get("subscription", "package").Text()
}

func validateReadiness(s api.Value, partial bool) []api.Issue {
	issues := []api.Issue{}
	olm := s.Get("olm")
	if timeout := s.Get("readiness", "timeout"); timeout.Present() {
		budget, err := time.ParseDuration(timeout.Text())
		switch {
		case err != nil || budget <= 0 || budget > MaxBudget:
			issues = append(issues, addonIssue("api.value", "spec.readiness.timeout", "Readiness timeout must be positive and at most "+budgetCeiling+".", "set readiness.timeout to a positive duration no greater than "+budgetCeiling))
		case budget%time.Second != 0:
			issues = append(issues, addonIssue("api.value", "spec.readiness.timeout", "Readiness timeout must be a whole number of seconds.", "set readiness.timeout to a whole number of seconds, such as 90s"))
		}
	}
	own, subscription := "", ""
	if olm.Present() {
		own, subscription = olm.Get("namespace", "name").Text(), effectiveSubscription(olm)
	}
	issues = append(issues, validateChecks(s.Get("readiness", "checks"), "spec.readiness.checks", own, subscription, olm.Present())...)
	for i, step := range s.Get("steps").Items() {
		issues = append(issues, validateChecks(step.Get("requires"), indexed("spec.steps", i)+".requires", "", "", false)...)
	}
	return issues
}

func validateChecks(checks api.Value, field, namespace, subscription string, own bool) []api.Issue {
	issues := []api.Issue{}
	for i, check := range checks.Items() {
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		at := indexed(field, i)
		if secretCheck(check) {
			issues = append(issues, addonIssue("api.invariant", at, "A readiness check cannot name a Secret.", "remove the check; Bootwright never reads a Secret on behalf of an authored add-on"))
			continue
		}
		if csv := check.Get("csvSucceeded"); own && csv.Present() && (namespace != "" && csv.Get("namespace").Text() != namespace || subscription != "" && csv.Get("subscription").Text() != subscription) {
			issues = append(issues, addonIssue("api.invariant", at+".csvSucceeded", "A csvSucceeded check must name the add-on's own namespace "+namespace+" and subscription "+subscription+".", "set csvSucceeded.namespace to olm.namespace.name and csvSucceeded.subscription to the effective olm.subscription.name"))
			continue
		}
		if own {
			if err := clusterobjects.ValidateCheck(programCheck(check)); err != nil {
				issues = append(issues, addonIssue("api.value", at, "Readiness check is invalid: "+err.Error()+".", "name a valid resource and a True, False or Unknown condition status in the check"))
			}
		}
	}
	return issues
}

func secretCheck(check api.Value) bool {
	for _, arm := range []string{"condition", "resourceExists"} {
		if resource := check.Get(arm); strings.EqualFold(resource.Get("kind").Text(), "Secret") && resource.Get("apiVersion").Text() == "v1" {
			return true
		}
	}
	return false
}

func programCheck(check api.Value) clusterobjects.Check {
	ref := func(v api.Value) clusterobjects.Ref {
		return clusterobjects.Ref{APIVersion: v.Get("apiVersion").Text(), Kind: v.Get("kind").Text(), Namespace: v.Get("namespace").Text(), Name: v.Get("name").Text(), Namespaced: v.Get("namespace").Text() != ""}
	}
	switch {
	case check.Has("csvSucceeded"):
		csv := check.Get("csvSucceeded")
		return clusterobjects.Check{CSVSucceeded: &clusterobjects.CSVCheck{Namespace: csv.Get("namespace").Text(), Subscription: csv.Get("subscription").Text()}}
	case check.Has("condition"):
		condition := check.Get("condition")
		return clusterobjects.Check{Condition: &clusterobjects.ConditionCheck{Ref: ref(condition), Type: condition.Get("condition", "type").Text(), Status: condition.Get("condition", "status").Text()}}
	case check.Has("resourceExists"):
		target := ref(check.Get("resourceExists"))
		return clusterobjects.Check{ResourceExists: &target}
	}
	return clusterobjects.Check{}
}
