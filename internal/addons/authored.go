package addons

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/canonicaljson"
)

const authoredSummary = "Authored OLM add-on"

type ShapeRefusal string

const (
	ShapeManifestSet ShapeRefusal = "it declares a manifest set"
	ShapeSteps       ShapeRefusal = "it declares steps"
	ShapeInputs      ShapeRefusal = "it declares inputs"
	ShapeExternal    ShapeRefusal = "its namespace is external"
	ShapeNoGroup     ShapeRefusal = "it declares no OperatorGroup"
	ShapeTargets     ShapeRefusal = "its OperatorGroup targets other namespaces"
	shapeNotAddOn    ShapeRefusal = "it is not a ClusterAddon"
)

func AuthoredShapeRefusal(definition api.Object) ShapeRefusal {
	spec := definition.Spec()
	olm := spec.Get("olm")
	switch {
	case definition.Kind() != api.ClusterAddon:
		return shapeNotAddOn
	case spec.Has("manifestSet") || !olm.Present():
		return ShapeManifestSet
	case spec.Get("steps").Len() > 0:
		return ShapeSteps
	case spec.Get("inputs").Len() > 0:
		return ShapeInputs
	case olm.Get("namespace", "management").Text() == "external":
		return ShapeExternal
	case !olm.Get("operatorGroup").Present():
		return ShapeNoGroup
	}
	if _, ok := authoredGroupShape(olm); !ok {
		return ShapeTargets
	}
	return ""
}

func authoredGroupShape(olm api.Value) (GroupShape, bool) {
	targets := olm.Get("operatorGroup", "targetNamespaces").Strings()
	switch {
	case len(targets) == 0 && olm.Has("operatorGroup", "targetNamespaces"):
		return AllNamespaces, true
	case len(targets) == 1 && targets[0] == olm.Get("namespace", "name").Text():
		return OwnNamespace, true
	}
	return NoGroup, false
}

func ExecutableAuthored(definition api.Object) bool {
	_, err := Authored(definition)
	return err == nil
}

func DescriptorDigest(spec api.Value) (string, error) {
	data, err := canonicaljson.Encode(nativeValue(spec), canonicaljson.Bare)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func Authored(definition api.Object) (Package, error) {
	identity := definition.Identity()
	if reason := AuthoredShapeRefusal(definition); reason != "" {
		return Package{}, fmt.Errorf("%s is not an executable add-on: %s", identity, reason)
	}
	name := definition.Name()
	if !validPackageName(name) {
		return Package{}, fmt.Errorf("%s has a name that is not a package name of at most %d bytes", identity, MaxPackageNameBytes)
	}
	spec, olm := definition.Spec(), definition.Spec().Get("olm")
	budget, err := time.ParseDuration(spec.Get("readiness", "timeout").Text())
	if err != nil || budget <= 0 || budget > MaxBudget || budget%time.Second != 0 {
		return Package{}, fmt.Errorf("%s has a readiness timeout outside the package budget", identity)
	}
	shape, _ := authoredGroupShape(olm)
	subscription := olm.Get("subscription")
	for _, required := range []string{"package", "channel", "startingCSV", "source", "sourceNamespace"} {
		if subscription.Get(required).Text() == "" {
			return Package{}, fmt.Errorf("%s has no olm.subscription.%s", identity, required)
		}
	}
	digest, err := DescriptorDigest(spec)
	if err != nil {
		return Package{}, fmt.Errorf("%s cannot be encoded: %w", identity, err)
	}
	labels := map[string]string{}
	for _, label := range olm.Get("namespace", "labels").Fields() {
		labels[label.Name] = label.Value.Text()
	}
	if _, err := renderAuthored(definition); err != nil {
		return Package{}, err
	}
	return Package{
		Name:     name,
		Summary:  authoredSummary,
		Provides: slices.Clone(spec.Get("provides").Strings()),
		Requires: slices.Clone(spec.Get("requires").Strings()),
		Budget:   budget,
		OLM: &OLM{
			Namespace: olm.Get("namespace", "name").Text(), NamespaceLabels: labels, OperatorGroup: shape,
			GroupName: olm.Get("operatorGroup", "name").Text(), Subscription: effectiveSubscription(olm),
			Package: subscription.Get("package").Text(), DefaultSource: subscription.Get("source").Text(), Approval: Exact,
		},
		Descriptor: digest,
		Render:     func(Instance) (Rendered, error) { return renderAuthored(definition) },
	}, nil
}

func authoredConfig(definition api.Object, budget time.Duration) api.Value {
	olm := definition.Spec().Get("olm")
	subscription := olm.Get("subscription")
	config := api.MapValue(
		api.FieldValue{Name: "channel", Value: subscription.Get("channel")},
		api.FieldValue{Name: "startingCSV", Value: subscription.Get("startingCSV")},
		api.FieldValue{Name: "source", Value: subscription.Get("source")},
		api.FieldValue{Name: "sourceNamespace", Value: subscription.Get("sourceNamespace")},
	)
	if catalog := olm.Get("catalogSource"); catalog.Present() {
		owned := api.MapValue(
			api.FieldValue{Name: "name", Value: catalog.Get("name")},
			api.FieldValue{Name: "image", Value: catalog.Get("image")},
			api.FieldValue{Name: "priority", Value: catalog.Get("priority")},
			api.FieldValue{Name: "securityContextConfig", Value: catalog.Get("securityContextConfig")},
		)
		if poll := catalog.Get("pollInterval"); poll.Present() {
			owned = owned.With("pollInterval", poll)
		}
		config = config.With("catalogSource", owned)
	}
	return api.MapValue(api.FieldValue{Name: "subscription", Value: config}, api.FieldValue{Name: "timeout", Value: api.StringValue(budget.String())})
}

func renderAuthored(definition api.Object) (Rendered, error) {
	spec := definition.Spec()
	rendered := Rendered{}
	for i, entry := range spec.Get("olm", "customResources").Items() {
		gate, err := authoredChecks(entry.Get("gate"), false)
		if err != nil {
			return Rendered{}, fmt.Errorf("%s custom resource %d gate: %w", definition.Identity(), i, err)
		}
		resource, metadata := entry.Get("resource"), entry.Get("resource", "metadata")
		bound, err := kindBoundChecks(gate, resource.Get("apiVersion").Text(), resource.Get("kind").Text())
		if err != nil {
			return Rendered{}, fmt.Errorf("%s custom resource %d gate: %w", definition.Identity(), i, err)
		}
		gate = append(gate, bound...)
		ref := clusterobjects.Ref{APIVersion: resource.Get("apiVersion").Text(), Kind: resource.Get("kind").Text(), Namespace: metadata.Get("namespace").Text(), Name: metadata.Get("name").Text(), Namespaced: true}
		rendered.After = append(rendered.After, clusterobjects.Phase{Gate: gate, Objects: []clusterobjects.Object{{Ref: ref, Body: resource}}})
	}
	checks, err := authoredChecks(spec.Get("readiness", "checks"), true)
	if err != nil {
		return Rendered{}, fmt.Errorf("%s readiness: %w", definition.Identity(), err)
	}
	rendered.Checks = checks
	return rendered, nil
}

func authoredChecks(checks api.Value, dropCSV bool) ([]clusterobjects.Check, error) {
	result := []clusterobjects.Check{}
	for i, check := range checks.Items() {
		if dropCSV && check.Has("csvSucceeded") {
			continue
		}
		program := programCheck(check)
		if err := clusterobjects.ValidateCheck(program); err != nil {
			return nil, fmt.Errorf("check %d: %w", i, err)
		}
		result = append(result, program)
	}
	return result, nil
}

func kindBoundChecks(gate []clusterobjects.Check, apiVersion, kind string) ([]clusterobjects.Check, error) {
	group, _, _ := strings.Cut(apiVersion, "/")
	var crds []clusterobjects.Ref
	for _, check := range gate {
		crd := check.ResourceExists
		plural, found := "", false
		if crd != nil && crd.APIVersion == crdAPIVersion && crd.Kind == crdKind {
			plural, found = strings.CutSuffix(crd.Name, "."+group)
		}
		if found && plural != "" && !strings.Contains(plural, ".") {
			crds = append(crds, *crd)
		}
	}
	if len(crds) != 1 {
		return nil, fmt.Errorf("the gate must name exactly one CustomResourceDefinition of group %s, not %d", group, len(crds))
	}
	phase := func(path, value string) clusterobjects.Check {
		return clusterobjects.Check{StatusPhase: &clusterobjects.PhaseCheck{Ref: crds[0], Path: path, Values: []string{value}}}
	}
	return []clusterobjects.Check{phase(clusterobjects.ScopePath, "Namespaced"), phase("status.acceptedNames.kind", kind)}, nil
}
