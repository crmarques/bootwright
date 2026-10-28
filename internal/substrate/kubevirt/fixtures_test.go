package kubevirt

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const testContext = "lab"

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func number(name, value string) api.FieldValue { return field(name, api.IntegerValue(value)) }

func controller() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
}

func provider(profile ...api.FieldValue) api.Object {
	selected := api.MapValue(text("name", "node"), number("cpu", "8"), number("memoryMiB", "16384"), number("diskGiB", "120"))
	for _, extra := range profile {
		selected = selected.With(extra.Name, extra.Value)
	}
	return api.NewObject(api.InfraProvider, "lab-kubevirt", api.Value{}, api.MapValue(
		field("kubevirt", api.MapValue(text("kubeconfigRef", "host-kubeconfig"), text("namespace", "guests"),
			text("storageClassRef", "guest-block"), field("machineProfiles", api.ListValue(selected)))),
		field("networkAttachments", api.ListValue(
			api.MapValue(text("name", "lab-guests"), field("kubevirt", api.MapValue(field("networkRef", api.MapValue(
				text("apiGroup", "k8s.cni.cncf.io"), text("kind", "NetworkAttachmentDefinition"), text("name", "guest-l2"), text("namespace", "guests")))))),
			api.MapValue(text("name", "storage"), field("kubevirt", api.MapValue(field("networkRef", api.MapValue(
				text("apiGroup", "k8s.ovn.org"), text("kind", "ClusterUserDefinedNetwork"), text("name", "storage-net")))))),
		)),
	))
}

func networkConfig() api.Object {
	return api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
		field("machineNetwork", api.ListValue(api.MapValue(text("cidr", "198.51.100.0/24")))),
		field("nmstate", api.MapValue(field("interfaces", api.ListValue(
			api.MapValue(text("name", "enp1s0"), text("type", "ethernet"), text("state", "up")),
			api.MapValue(text("name", "enp2s0"), text("type", "ethernet"), text("state", "up")),
		)))),
	))
}

func node(name string) api.Object {
	return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "lab-kubevirt"), field("kubevirt", api.MapValue(text("profileRef", "node"),
			field("interfaceAttachments", api.ListValue(
				api.MapValue(text("interface", "enp1s0"), text("attachmentRef", "lab-guests")),
				api.MapValue(text("interface", "enp2s0"), text("attachmentRef", "storage")),
			)))))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
		field("network", api.MapValue(text("configRef", "lab-guests"))),
	))
}

func labCatalog(objects ...api.Object) api.Catalog {
	if len(objects) == 0 {
		objects = []api.Object{provider()}
	}
	return api.NewCatalog(append([]api.Object{controller(), networkConfig(), node("master-0")}, objects...))
}

func planInput(catalog api.Catalog, verb reconciliation.Verb) lifecycle.PlanInput {
	return lifecycle.PlanInput{
		State: compilation.NewState(catalog, catalog, nil), Verb: verb,
		Context: lifecycle.ContextIdentity{Name: testContext}, Controller: "controller",
	}
}

func machineRequest(t *testing.T) MachineRequest {
	t.Helper()
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %v, %v", requests, err)
	}
	return requests[0]
}

func hostRequest(t *testing.T) HostRequest {
	t.Helper()
	requests, err := HostRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %v, %v", requests, err)
	}
	return requests[0]
}

func encoded(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type runner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *runner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}
