//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller/evidence"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

type accessMetadataFixture struct {
	records []lifecycle.BlockEvidence
	present bool
	located []evidence.InstalledTool
	refusal error
}

func (f *accessMetadataFixture) Evidence(context.Context, string, string, string) ([]lifecycle.BlockEvidence, error) {
	return f.records, nil
}
func (f *accessMetadataFixture) AccessTool(_ context.Context, _ string, tool evidence.InstalledTool) (string, error) {
	f.located = append(f.located, tool)
	return "/installed/" + tool.Executable, f.refusal
}
func (f *accessMetadataFixture) ProducedAvailable(context.Context, custody.ReadProducedRequest) (bool, error) {
	return f.present, nil
}

func TestClusterClientMetadataRequiresCustodyAndTheMatchingInstalledClient(t *testing.T) {
	t.Parallel()
	var request agentinstall.InstallRequest
	for _, block := range compactExamplePlan(t).Blocks {
		if block.Implementation == agentinstall.InstallImplementation {
			var err error
			request, err = agentinstall.DecodeInstallRequest(block.Request)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, distribution := range []string{"openshift", "okd"} {
		request.Identity = agentinstall.Identity{Cluster: "example", Context: "example"}
		request.Release = agentinstall.Release{Distribution: distribution, Version: "4.19.3"}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		fixture := &accessMetadataFixture{present: true, records: []lifecycle.BlockEvidence{{Implementation: agentinstall.InstallImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: raw}}}}}
		metadata := clusterClientMetadata{reconciler: fixture, custody: fixture}
		for _, executable := range []string{"oc", "kubectl"} {
			result, err := metadata.Client(t.Context(), "example", "example", executable)
			if err != nil || result.Executable != "/installed/"+executable || result.Version != "4.19.3" || fixture.located[len(fixture.located)-1].Compatibility != distribution {
				t.Fatalf("result %+v, error %v", result, err)
			}
		}
		fixture.present = false
		if _, err := metadata.Client(t.Context(), "example", "example", "oc"); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "access.unavailable" || len(fixture.located) != 2 {
			t.Fatalf("missing custody: %v; lookups %v", err, fixture.located)
		}
		fixture.present = true
		fixture.refusal = diagnostics.NewFailureWithRemediation("controller.state", "client is absent", "", "apply the controller stage")
		reason, err := metadata.ClientReason(t.Context(), "example", "example", "oc")
		if err != nil || !strings.Contains(reason, "client is absent") || !strings.Contains(reason, "apply the controller stage") {
			t.Fatalf("reason %q, error %v", reason, err)
		}
	}
}

type descriptorMetadata struct{}

func (descriptorMetadata) Client(_ context.Context, _, _, executable string) (containeraccess.Client, error) {
	return containeraccess.Client{Executable: "/installed/" + executable, Version: "4.19.3", Distribution: "openshift"}, nil
}

func TestClusterHandoffCommandWritesOneDescriptorAndKeepsCredentialsPrivate(t *testing.T) {
	t.Parallel()
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.OpenShiftCluster, "example", api.Value{}, api.MapValue())})
	service := containeraccess.New(labSessionState{effective: catalog}, nil, nil, func(context.Context) (string, error) { return "example", nil }).WithMetadata(descriptorMetadata{})
	services := cli.Services{ClusterAccess: service}
	for _, executable := range []string{"oc", "kubectl"} {
		for _, format := range []string{"text", "json", "yaml"} {
			out, stderr := contextRun(t, services, 0, "cluster", executable, "--name", "example", "--output", format, "--", "get", "nodes", "--help", "")
			if stderr != "" || !strings.Contains(out, "/installed/"+executable) || !strings.Contains(out, "kubeconfigCommand") {
				t.Fatalf("%s %s: %q; %q", executable, format, out, stderr)
			}
			if format != "yaml" {
				var document map[string]any
				if err := json.Unmarshal([]byte(out), &document); err != nil {
					t.Fatal(err)
				}
			}
		}
		out, stderr := contextRun(t, services, 1, "cluster", executable, "--name", "example", "--", "get", "nodes", "--token=DO-NOT-PRINT")
		if out != "" || strings.Contains(stderr, "DO-NOT-PRINT") {
			t.Fatalf("unsafe output: %q; %q", out, stderr)
		}
		singleDiagnostic(t, stderr, "access.handoff")
	}
}
