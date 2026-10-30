package instance

import (
	"fmt"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func Render(selected addons.SelectedInstance, facts addons.ClusterFacts, controller string) (Request, []reconciliation.ObjectRef, error) {
	p, instance := selected.Package, selected.Instance
	instance.Facts = facts
	rendered, err := p.Render(instance)
	if err != nil {
		return Request{}, nil, err
	}
	budget := p.Budget
	if timeout := instance.Config.Get("timeout").Text(); timeout != "" {
		budget, err = time.ParseDuration(timeout)
		if err != nil {
			return Request{}, nil, err
		}
	}
	if budget%time.Second != 0 {
		return Request{}, nil, fmt.Errorf("package %s timeout must use whole seconds", p.Name)
	}
	program := co.Program{Version: co.Version, Steps: []co.Step{}, Checks: rendered.Checks}
	if program.Checks == nil {
		program.Checks = []co.Check{}
	}
	if p.OLM != nil {
		if err := operatorPrelude(&program, *p.OLM, instance.Config); err != nil {
			return Request{}, nil, err
		}
	}
	if err := appendPhases(&program, rendered.Before); err != nil {
		return Request{}, nil, err
	}
	if p.OLM != nil {
		if err := subscribe(&program, *p.OLM, instance.Config, rendered.SubscriptionConfig); err != nil {
			return Request{}, nil, err
		}
	}
	if err := appendPhases(&program, rendered.After); err != nil {
		return Request{}, nil, err
	}
	placementNamespaces := declaredNamespaces(p)
	options := co.Options{PlacementNamespaces: slices.Clone(placementNamespaces)}
	if p.OLM != nil {
		options.Namespace = p.OLM.Namespace
	}
	data, err := co.Freeze(program, options)
	if err != nil {
		return Request{}, nil, fmt.Errorf("program of package %s for %s: %w", p.Name, instance.Cluster, err)
	}
	secrets := []string{}
	produced := []lifecycle.ProducedInput{{Kind: "OpenShiftCluster", Object: instance.Cluster, Output: "kubeconfig"}}
	for _, step := range program.Steps {
		if step.Place != nil {
			if p := step.Place.Produced; p != nil {
				ref := lifecycle.ProducedInput{Kind: p.Kind, Object: p.Object, Output: p.Output}
				if !slices.Contains(produced, ref) {
					produced = append(produced, ref)
				}
			} else {
				secrets = append(secrets, step.Place.Secret)
			}
		}
	}
	slices.Sort(secrets)
	secrets = slices.Compact(secrets)
	request := Request{Binding: instance.Binding, BudgetSeconds: int64(budget / time.Second), Cluster: instance.Cluster, Package: p.Name, Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: controller}, PlacementNamespaces: placementNamespaces, ProducedInputs: produced, Program: data, Secrets: secrets, Tool: Tool{Compatibility: "openshift", Kind: "openshift-clients", Version: facts.Release}, Version: requestVersion}
	requires := []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: instance.Cluster}}
	for _, provider := range selected.Providers {
		if provider.Package != "" {
			requires = append(requires, reconciliation.ObjectRef{Kind: Kind, Object: Object(instance.Cluster, provider.Package)})
		}
	}
	requires = append(requires, rendered.Requires...)
	_, err = request.Canonical()
	return request, requires, err
}

func declaredNamespaces(p addons.Package) []string {
	namespaces := slices.Clone(p.PlacementNamespaces)
	slices.Sort(namespaces)
	return append([]string{}, slices.Compact(namespaces)...)
}
func appendPhases(program *co.Program, phases []co.Phase) error {
	for _, phase := range phases {
		steps, err := co.CompilePhase(phase)
		if err != nil {
			return err
		}
		program.Steps = append(program.Steps, steps...)
		for _, approval := range phase.Approvals {
			program.Checks = append(program.Checks, co.Check{CSVSucceeded: &co.CSVCheck{Namespace: approval.Namespace, Subscription: approval.Subscription, CSV: approval.CSV}})
		}
	}
	return nil
}
func nativeObject(apiVersion, kind, namespace, name string, spec api.Value) co.Object {
	metadata := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(name)})
	if namespace != "" {
		metadata = metadata.With("namespace", api.StringValue(namespace))
	}
	body := api.MapValue(api.FieldValue{Name: "apiVersion", Value: api.StringValue(apiVersion)}, api.FieldValue{Name: "kind", Value: api.StringValue(kind)}, api.FieldValue{Name: "metadata", Value: metadata})
	if spec.Present() {
		body = body.With("spec", spec)
	}
	return co.Object{Ref: co.Ref{APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name, Namespaced: namespace != ""}, Body: body}
}
func stringMap(values map[string]string) api.Value {
	fields := []api.FieldValue{}
	for k, v := range values {
		fields = append(fields, api.FieldValue{Name: k, Value: api.StringValue(v)})
	}
	slices.SortFunc(fields, func(a, b api.FieldValue) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return api.MapValue(fields...)
}
func addObject(program *co.Program, object co.Object) error {
	entry, err := co.NewObjectEntry(object)
	if err != nil {
		return err
	}
	program.Steps = append(program.Steps, co.Step{Kind: "create", Create: &entry})
	return nil
}
func operatorPrelude(program *co.Program, olm addons.OLM, config api.Value) error {
	namespace := nativeObject("v1", "Namespace", "", olm.Namespace, api.Value{})
	if len(olm.NamespaceLabels) > 0 {
		namespace.Body = namespace.Body.WithPath(stringMap(olm.NamespaceLabels), "metadata", "labels")
	}
	if err := addObject(program, namespace); err != nil {
		return err
	}
	if olm.OperatorGroup != addons.NoGroup {
		spec := api.Value{}
		if olm.OperatorGroup == addons.OwnNamespace {
			spec = api.MapValue(api.FieldValue{Name: "targetNamespaces", Value: api.StringList(olm.Namespace)})
		} else if olm.OperatorGroup != addons.AllNamespaces {
			return fmt.Errorf("invalid OperatorGroup shape")
		}
		if err := addObject(program, nativeObject("operators.coreos.com/v1", "OperatorGroup", olm.Namespace, olm.GroupName, spec)); err != nil {
			return err
		}
	}
	catalog := config.Get("subscription", "catalogSource")
	if !catalog.Present() {
		return nil
	}
	spec := api.MapValue(api.FieldValue{Name: "sourceType", Value: api.StringValue("grpc")}, api.FieldValue{Name: "image", Value: catalog.Get("image")}, api.FieldValue{Name: "priority", Value: catalog.Get("priority")}, api.FieldValue{Name: "grpcPodConfig", Value: api.MapValue(api.FieldValue{Name: "securityContextConfig", Value: catalog.Get("securityContextConfig")})})
	if poll := catalog.Get("pollInterval"); poll.Present() {
		spec = spec.With("updateStrategy", api.MapValue(api.FieldValue{Name: "registryPoll", Value: api.MapValue(api.FieldValue{Name: "interval", Value: poll})}))
	}
	object := nativeObject("operators.coreos.com/v1alpha1", "CatalogSource", "openshift-marketplace", catalog.Get("name").Text(), spec)
	if err := addObject(program, object); err != nil {
		return err
	}
	program.Steps = append(program.Steps, co.Step{Kind: "catalogReady", CatalogReady: &object.Ref})
	program.Checks = append(program.Checks, co.Check{StatusPhase: &co.PhaseCheck{Ref: object.Ref, Path: "status.connectionState.lastObservedState", Values: []string{"READY"}}})
	return nil
}
func subscribe(program *co.Program, olm addons.OLM, config api.Value, settings *co.SubscriptionConfig) error {
	sub := config.Get("subscription")
	spec := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(olm.Package)}, api.FieldValue{Name: "channel", Value: sub.Get("channel")}, api.FieldValue{Name: "source", Value: sub.Get("source")}, api.FieldValue{Name: "sourceNamespace", Value: sub.Get("sourceNamespace")}, api.FieldValue{Name: "startingCSV", Value: sub.Get("startingCSV")}, api.FieldValue{Name: "installPlanApproval", Value: api.StringValue("Manual")})
	if settings != nil {
		value := api.MapValue()
		if len(settings.Env) > 0 {
			env := []api.Value{}
			for _, v := range settings.Env {
				env = append(env, api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(v.Name)}, api.FieldValue{Name: "value", Value: api.StringValue(v.Value)}))
			}
			value = value.With("env", api.ListValue(env...))
		}
		if len(settings.NodeSelector) > 0 {
			value = value.With("nodeSelector", stringMap(settings.NodeSelector))
		}
		if settings.Tolerations != nil {
			value = value.With("tolerations", api.ListValue(settings.Tolerations...))
		}
		if settings.Resources.Present() {
			value = value.With("resources", settings.Resources)
		}
		spec = spec.With("config", value)
	}
	object, err := co.NewObjectEntry(nativeObject("operators.coreos.com/v1alpha1", "Subscription", olm.Namespace, olm.Subscription, spec))
	if err != nil {
		return err
	}
	program.Steps = append(program.Steps, co.Step{Kind: "subscribe", Subscribe: &co.Subscription{Object: object, Approval: co.Approval(olm.Approval), CSV: sub.Get("startingCSV").Text()}})
	program.Checks = append([]co.Check{{CSVSucceeded: &co.CSVCheck{Namespace: olm.Namespace, Subscription: olm.Subscription, CSV: sub.Get("startingCSV").Text()}}}, program.Checks...)
	return nil
}
