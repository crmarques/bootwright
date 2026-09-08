package addons

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
)

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	s := o.Spec()
	switch o.Kind() {
	case api.ClusterAddon:
		for _, field := range []string{"provides", "requires"} {
			if s.Has(field) {
				s = s.With(field, sortedStrings(s.Get(field)))
			}
		}
		if s.Has("inputs") {
			inputs := s.Get("inputs").Items()
			for i := range inputs {
				inputs[i] = inputs[i].Default("required", api.BoolValue(true))
			}
			s = s.With("inputs", sortedNamed(api.ListValue(inputs...), "name"))
		}
		if olm := s.Get("olm"); olm.Present() {
			if ns := olm.Get("namespace"); ns.Present() {
				olm = olm.With("namespace", ns.Default("management", api.StringValue("managed")))
			}
			namespace := olm.Get("namespace", "name").Text()
			if group := olm.Get("operatorGroup"); group.Present() && namespace != "" {
				olm = olm.With("operatorGroup", group.Default("name", api.StringValue(namespace)).Default("targetNamespaces", api.StringList(namespace)))
			}
			if sub := olm.Get("subscription"); sub.Present() {
				if packageName := sub.Get("package").Text(); packageName != "" {
					sub = sub.Default("name", api.StringValue(packageName))
				}
				if source := olm.Get("catalogSource", "name").Text(); source != "" {
					sub = sub.Default("source", api.StringValue(source))
				}
				sub = sub.Default("sourceNamespace", api.StringValue("openshift-marketplace")).Default("installPlanApproval", api.StringValue("Automatic"))
				olm = olm.With("subscription", sub)
			}
			s = s.With("olm", olm)
		}
		readiness := s.Get("readiness").Default("timeout", api.StringValue("30m"))
		if !readiness.Has("checks") {
			checks := api.ListValue()
			if s.Has("olm") {
				namespace, subscription := s.Get("olm", "namespace", "name"), s.Get("olm", "subscription", "name")
				if namespace.Text() != "" && subscription.Text() != "" {
					checks = api.ListValue(api.MapValue(api.FieldValue{Name: "csvSucceeded", Value: api.MapValue(api.FieldValue{Name: "namespace", Value: namespace}, api.FieldValue{Name: "subscription", Value: subscription})}))
				}
			}
			readiness = readiness.With("checks", checks)
		}
		s = s.With("readiness", readiness)
		if s.Has("steps") {
			steps := s.Get("steps").Items()
			for i := range steps {
				if steps[i].Has("secretRefs") {
					steps[i] = steps[i].With("secretRefs", sortedStrings(steps[i].Get("secretRefs")))
				}
			}
			s = s.With("steps", api.ListValue(steps...))
		}
	case api.ClusterAddonBinding:
		if s.Has("addonConfigs") {
			configs := s.Get("addonConfigs").Items()
			for i := range configs {
				if configs[i].Has("inputs") {
					configs[i] = configs[i].With("inputs", sortedNamed(configs[i].Get("inputs"), "name"))
				}
			}
			s = s.With("addonConfigs", sortedNamed(api.ListValue(configs...), "addonRef"))
		}
	default:
		return o, nil
	}
	return o.WithSpec(s), nil
}

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	return ValidatePartial(o, c)
}

// ValidatePartial rejects contradictions that do not need a complete object.
func ValidatePartial(o api.Object, _ api.Catalog) []api.Issue {
	if o.Kind() != api.ClusterAddon {
		return nil
	}
	issues := validateDeclaration(o, api.NewCatalog(nil), true)
	if o.Spec().Get("provides").Len() > 0 && o.Spec().Has("readiness", "checks") && o.Spec().Get("readiness", "checks").Len() == 0 {
		issues = append(issues, addonIssue("api.invariant", "spec.readiness.checks", "An add-on providing capabilities requires an effective readiness check."))
	}
	return boundedIssues(issues)
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	switch o.Kind() {
	case api.ClusterAddon:
		issues := validateDeclaration(o, c, false)
		if o.Spec().Get("provides").Len() > 0 && o.Spec().Get("readiness", "checks").Len() == 0 {
			issues = append(issues, addonIssue("api.invariant", "spec.readiness.checks", "An add-on providing capabilities requires an effective readiness check."))
		}
		return boundedIssues(issues)
	case api.ClusterAddonProfile:
		_, issues := expandSelection(o, c)
		return issues
	case api.ClusterAddonBinding:
		return validateBinding(o, c)
	}
	return nil
}

func validateDeclaration(o api.Object, c api.Catalog, partial bool) []api.Issue {
	s := o.Spec()
	issues := []api.Issue{}
	providesStorage := slices.Contains(s.Get("provides").Strings(), "dataFoundation")
	for i, input := range s.Get("inputs").Items() {
		for j, effect := range input.Get("effects").Items() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			field := indexed(indexed("spec.inputs", i)+".effects", j)
			wrongStorageKind := input.Get("resourceKind").Text() != string(api.StorageExport) && (!partial || input.Has("resourceKind") || input.Has("secretType"))
			missingStorageCapability := !providesStorage && (!partial || s.Has("provides"))
			if effect.Has("storageExportAttachment") && (wrongStorageKind || missingStorageCapability) {
				issues = append(issues, addonIssue("api.invariant", field, "Storage attachment requires a StorageExport input and the dataFoundation capability."))
			}
			if effect.Has("globalPullSecretMerge") && input.Get("secretType").Text() != "token" && (!partial || input.Has("resourceKind") || input.Has("secretType")) {
				issues = append(issues, addonIssue("api.invariant", field, "Global pull-secret merging requires a token Secret input."))
			}
		}
	}
	olm := s.Get("olm")
	if catalog := olm.Get("catalogSource"); catalog.Present() {
		if source := olm.Get("subscription", "source"); source.Present() && catalog.Get("name").Text() != "" && source.Text() != catalog.Get("name").Text() {
			issues = append(issues, addonIssue("api.invariant", "spec.olm.subscription.source", "Subscription source must match the shipped CatalogSource name."))
		}
		if (!partial || olm.Has("namespace", "management")) && olm.Get("namespace", "management").Text() != "external" && olm.Get("namespace", "name").Text() != "" && olm.Get("subscription", "sourceNamespace").Text() == olm.Get("namespace", "name").Text() {
			issues = append(issues, addonIssue("api.invariant", "spec.olm.subscription.sourceNamespace", "A shipped CatalogSource must use a namespace distinct from a managed operator namespace."))
		}
	}
	for _, label := range olm.Get("namespace", "labels").Fields() {
		if !validLabelKey(label.Name) || !validLabelValue(label.Value.Text()) {
			issues = append(issues, addonIssue("api.value", "spec.olm.namespace.labels", "Namespace labels must have valid Kubernetes keys and values."))
			break
		}
	}
	for i, resource := range olm.Get("customResources").Items() {
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		field := indexed("spec.olm.customResources", i)
		for _, member := range []string{"apiVersion", "kind"} {
			value := resource.Get(member)
			if (!partial || value.Present()) && (value.Type() != api.String || value.Text() == "") {
				issues = append(issues, addonIssue("api.value", field+"."+member, "A custom resource requires a non-empty string identity."))
			}
		}
		if name := resource.Get("metadata", "name"); (!partial || name.Present()) && (name.Type() != api.String || name.Text() == "") {
			issues = append(issues, addonIssue("api.value", field+".metadata.name", "A custom resource requires a non-empty string name."))
		}
		if resource.Get("kind").Text() == "Secret" && (resource.Has("data") || resource.Has("stringData")) {
			issues = append(issues, addonIssue("api.invariant", field, "Custom Secret resources cannot contain inline data or stringData."))
		}
	}
	issues = append(issues, validateManifests(s.Get("manifestSet", "manifests"), "spec.manifestSet.manifests", partial)...)
	inputsByName := uniqueNamedValues(s.Get("inputs"), "name")
	for i, step := range s.Get("steps").Items() {
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		field := indexed("spec.steps", i)
		if partial {
			issues = append(issues, customplaybooks.ValidatePartialContent(step, field, false)...)
		} else {
			issues = append(issues, customplaybooks.ValidateContent(step, field, c, false)...)
		}
		issues = append(issues, validateManifests(step.Get("manifests"), field+".manifests", partial)...)
		if !partial && !step.Has("playbook") && step.Get("manifests").Len() == 0 {
			issues = append(issues, addonIssue("api.invariant", field, "A step requires a playbook or at least one manifest."))
		}
		if step.Get("follows").Text() == "operatorReady" && !s.Has("olm") && (!partial || s.Has("manifestSet")) {
			issues = append(issues, addonIssue("api.invariant", field+".follows", "Only OLM add-ons have an operatorReady anchor."))
		}
		if !partial && step.Has("playbook") && !step.Has("target") {
			issues = append(issues, addonIssue("api.required", field+".target", "A playbook step requires an explicit target."))
		}
		if !partial && !step.Has("playbook") && (step.Has("target") || step.Has("outputs")) {
			issues = append(issues, addonIssue("api.invariant", field, "Targets and outputs require a playbook step."))
		}
		issues = append(issues, validateStepTarget(step.Get("target"), inputsByName, s.Has("inputs"), field+".target", c, partial)...)
		for j, output := range step.Get("outputs").Items() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			if output.Get("secret").Bool() && output.Get("format").Text() == "sha256" {
				issues = append(issues, addonIssue("api.invariant", indexed(field+".outputs", j), "A SHA-256 output cannot be marked secret."))
			}
		}
	}
	return boundedIssues(issues)
}

func validateStepTarget(target api.Value, inputs map[string]api.Value, inputsPresent bool, field string, c api.Catalog, partial bool) []api.Issue {
	issues := []api.Issue{}
	if static := target.Get("static"); static.Present() {
		if !partial && static.Get("clusters").Len()+static.Get("machines").Len() == 0 {
			issues = append(issues, addonIssue("api.invariant", field+".static", "A static target requires at least one cluster or Machine."))
		}
		for i, ref := range static.Get("machines").Items() {
			if len(issues) >= maxIssues {
				return issues[:maxIssues]
			}
			if machine, found := c.Find(api.Machine, ref.Text()); found && !machine.Spec().Has("access", "ssh") {
				issues = append(issues, addonIssue("api.reference", indexed(field+".static.machines", i), "Static playbook targets must select SSH-accessible Machines."))
			}
		}
	}
	if from := target.Get("fromInput"); from.Present() {
		if partial && (!inputsPresent || !from.Has("input")) {
			return issues
		}
		input, found := inputs[from.Get("input").Text()]
		validKind := slices.Contains([]string{string(api.StorageExport), string(api.StorageCluster), string(api.ContainerCluster), string(api.Machine)}, input.Get("resourceKind").Text())
		if !found || !validKind && (!partial || input.Has("resourceKind") || input.Has("secretType")) || input.Get("resourceKind").Text() == string(api.StorageExport) && !hasEffect(input, "storageExportAttachment") && (!partial || input.Has("effects")) {
			issues = append(issues, addonIssue("api.reference", field+".fromInput.input", "Input targets require a declared supported resource input and any required storage-attachment effect."))
		}
	}
	return boundedIssues(issues)
}

func validateManifests(manifests api.Value, field string, partial bool) []api.Issue {
	issues := []api.Issue{}
	for i, manifest := range manifests.Items() {
		if partial && !manifest.Has("path") {
			continue
		}
		if len(issues) >= maxIssues {
			return issues[:maxIssues]
		}
		value := manifest.Get("path").Text()
		ext := strings.ToLower(path.Ext(value))
		if !api.ValidLexical("relative-path", value) || !slices.Contains(strings.Split(value, "/"), "manifests") || ext != ".yaml" && ext != ".yml" {
			issues = append(issues, addonIssue("api.value", indexed(field, i)+".path", "Manifest paths must be contained YAML paths with a manifests segment."))
		}
	}
	return issues
}

func sortedStrings(value api.Value) api.Value {
	items := value.Strings()
	slices.Sort(items)
	return api.StringList(items...)
}

func sortedNamed(value api.Value, key string) api.Value {
	items := value.Items()
	slices.SortStableFunc(items, func(a, b api.Value) int { return strings.Compare(a.Get(key).Text(), b.Get(key).Text()) })
	return api.ListValue(items...)
}

func uniqueNamedValues(values api.Value, key string) map[string]api.Value {
	unique := map[string]api.Value{}
	duplicates := map[string]bool{}
	for _, value := range values.Items() {
		name := value.Get(key).Text()
		if _, exists := unique[name]; exists {
			duplicates[name] = true
		}
		unique[name] = value
	}
	for name := range duplicates {
		delete(unique, name)
	}
	return unique
}

func hasEffect(input api.Value, effect string) bool {
	for _, value := range input.Get("effects").Items() {
		if value.Len() == 1 && value.Get(effect).Type() == api.Mapping {
			return true
		}
	}
	return false
}

var labelValue = regexp.MustCompile(`^[A-Za-z0-9](?:[-_.A-Za-z0-9]{0,61}[A-Za-z0-9])?$`)

func validLabelValue(value string) bool { return value == "" || labelValue.MatchString(value) }

func validLabelKey(value string) bool {
	prefix, name, qualified := strings.Cut(value, "/")
	if qualified {
		return api.ValidLexical("dns", prefix) && labelValue.MatchString(name)
	}
	return labelValue.MatchString(value)
}

func indexed(field string, index int) string { return field + "[" + strconv.Itoa(index) + "]" }

func addonIssue(code, field, message string) api.Issue {
	if !strings.HasPrefix(field, "$") {
		field = "$." + field
	}
	return api.Issue{Code: code, Field: field, Message: message}
}

const maxIssues = 999

func boundedIssues(issues []api.Issue) []api.Issue {
	if len(issues) > maxIssues {
		return issues[:maxIssues]
	}
	return issues
}
