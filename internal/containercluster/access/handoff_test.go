package access

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type metadataStub struct {
	calls  []string
	err    error
	client Client
}

func (m *metadataStub) Client(_ context.Context, contextName, cluster, executable string) (Client, error) {
	m.calls = append(m.calls, contextName+"/"+cluster+"/"+executable)
	return m.client, m.err
}
func readyMetadata() *metadataStub {
	return &metadataStub{client: Client{Executable: "/approved/oc", Version: "4.19.3", Distribution: "openshift"}}
}

func TestClientHandoffsPreservePayloadWithoutReadingCredentials(t *testing.T) {
	for _, name := range []string{"sno", "okd"} {
		for _, explicit := range []string{"", "lab"} {
			for _, executable := range []string{"oc", "kubectl"} {
				reader, metadata := custody(), readyMetadata()
				metadata.client.Executable = "/approved/" + executable
				state := &effectiveState{objects: graph()}
				service := New(state, reader, locate, current("lab")).WithMetadata(metadata)
				args := []string{"get", "pods", "--help", "", "quotes ' \" $(echo never)"}
				result, err := service.handoff(t.Context(), executable, explicit, name, args)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(result.Arguments, args) || result.Context != "lab" || result.Cluster != name || result.Client.Executable != "/approved/"+executable || len(reader.reads) != 0 || !slices.Equal(metadata.calls, []string{"lab/" + name + "/" + executable}) {
					t.Fatalf("result %+v; credential reads %v; metadata %v", result, reader.reads, metadata.calls)
				}
				args[0] = "changed"
				if result.Arguments[0] != "get" {
					t.Fatal("argument alias escaped")
				}
				if !slices.Equal(result.KubeconfigCommand, []string{"bootwright", "cluster", "kubeconfig", "--context", "lab", "--name", name}) {
					t.Fatal(result.KubeconfigCommand)
				}
			}
		}
	}
}

func TestClientHandoffsRefuseBeforeMetadataOrCredentials(t *testing.T) {
	for name, want := range map[string]string{"missing": "access.target", "": "access.target", "ceph": "cluster.not-applicable", "remote": "cluster.not-applicable"} {
		for _, executable := range []string{"oc", "kubectl"} {
			reader, metadata := custody(), readyMetadata()
			result, err := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).WithMetadata(metadata).handoff(t.Context(), executable, "", name, []string{"get", "nodes"})
			if result != nil || code(t, err) != want || len(reader.reads)+len(metadata.calls) != 0 {
				t.Fatalf("%s: result %+v, error %v", name, result, err)
			}
		}
	}
	for _, args := range [][]string{nil, {"get", "\x00"}, {"get", "\xff"}, {"get", "--token=private"}, {"get", "--server", "https://other.test"}, {"get", "--kubeconfig=other"}, {"get", "-shttps://other.test"}, {"login", "secret"}, {"--namespace=default", "config", "set-credentials", "example", "--exec-command=/tmp/helper"}, {"create", "secret", "generic", "example", "--from-literal=password=secret"}, {"whoami", "--show-token"}, {"whoami", "--show-token=true"}, {"whoami", "-t"}, {"whoami", "-t=true"}, {"whoami", "-ct"}, {"whoami", "-tc"}, {"whoami", "--show-context", "-t"}} {
		reader, metadata := custody(), readyMetadata()
		result, err := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).WithMetadata(metadata).OC(t.Context(), OCRequest{Name: "sno", Command: args})
		if result != nil || code(t, err) != "access.handoff" || len(reader.reads)+len(metadata.calls) != 0 {
			t.Fatalf("unsafe arguments: result %+v, error %v", result, err)
		}
	}
	spec, readErr := os.ReadFile(filepath.Join("..", "..", "..", "specs", "cli", "cluster-access.md"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	_, refused, _ := strings.Cut(string(spec), "These refuse `access.handoff` without")
	refused, _, _ = strings.Cut(refused, "(`TestClientHandoffsRefuseBeforeMetadataOrCredentials`)")
	flags := regexp.MustCompile("`(--[a-z-]+)`").FindAllStringSubmatch(refused, -1)
	if len(flags) != 18 {
		t.Fatalf("the spec lists %d refused flags, want 18", len(flags))
	}
	for _, flag := range flags {
		for _, args := range [][]string{{"get", flag[1]}, {"get", flag[1] + "=x"}} {
			result, err := New(&effectiveState{objects: graph()}, custody(), locate, current("lab")).WithMetadata(readyMetadata()).OC(t.Context(), OCRequest{Name: "sno", Command: args})
			if result != nil || code(t, err) != "access.handoff" {
				t.Errorf("%v: result %+v, error %v, want access.handoff", args, result, err)
			}
		}
	}
}

func TestClientHandoffPreservesPrerequisiteAndContextRefusals(t *testing.T) {
	refused := diagnostics.NewFailure("access.unavailable", "no completed installation", "")
	reader, metadata := custody(), readyMetadata()
	metadata.err = refused
	service := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).WithMetadata(metadata)
	result, err := service.Kubectl(t.Context(), KubectlRequest{Name: "okd", Command: []string{"get", "nodes"}})
	if result != nil || !errors.Is(err, refused) || len(reader.reads) != 0 {
		t.Fatalf("result %+v, error %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.OC(ctx, OCRequest{Name: "sno", Command: []string{"get", "nodes"}})
	if !errors.Is(err, context.Canceled) || len(metadata.calls) != 1 {
		t.Fatal(err, metadata.calls)
	}
	metadata.err = nil
	metadata.client.Executable = "oc"
	_, err = service.OC(t.Context(), OCRequest{Name: "sno", Command: []string{"get", "nodes"}})
	if code(t, err) != "access.handoff" {
		t.Fatal(err)
	}
}
