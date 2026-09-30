package access

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const kubeconfig = "apiVersion: v1\nclusters:\n- cluster:\n    server: https://api.sno.lab.example:6443\n  name: sno\nkind: Config\n"

type effectiveState struct {
	objects  []api.Object
	contexts []string
	failure  error
}

func (s *effectiveState) RenderEffective(_ context.Context, request compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	s.contexts = append(s.contexts, request.ContextName)
	if s.failure != nil {
		return nil, s.failure
	}
	return &compilation.EffectiveResult{Effective: api.NewCatalog(s.objects)}, nil
}

type custodyReader struct {
	entries map[string]string
	reads   []string
	failure error
}

func (r *custodyReader) ReadProduced(_ context.Context, contextName, block, name string) (secrets.Material, bool, error) {
	r.reads = append(r.reads, contextName+"/"+block+"/"+name)
	if r.failure != nil {
		return secrets.Material{}, false, r.failure
	}
	value, found := r.entries[contextName+"/"+block+"/"+name]
	if !found {
		return secrets.Material{}, false, nil
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)}), true, nil
}

func locate(cluster string) (string, string) { return "cluster-install-" + cluster, "kubeconfig" }

func object(kind api.Kind, name string, fields ...api.FieldValue) api.Object {
	return api.NewObject(kind, name, api.MapValue(), api.MapValue(fields...))
}

func distribution(kind string) api.FieldValue {
	return api.FieldValue{Name: "distribution", Value: api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue(kind)})}
}

func management(kind string) api.FieldValue {
	return api.FieldValue{Name: "management", Value: api.StringValue(kind)}
}

// A lab-sno-shaped graph beside an OKD cluster and both kinds of Ceph
// StorageCluster, in one declaration order.
func graph() []api.Object {
	return []api.Object{
		object(api.ContainerCluster, "sno", distribution("openshift")),
		object(api.ContainerCluster, "okd", distribution("okd")),
		object(api.StorageCluster, "ceph", api.FieldValue{Name: "type", Value: api.StringValue("ceph")}, management("managed")),
		object(api.StorageCluster, "remote", api.FieldValue{Name: "type", Value: api.StringValue("ceph")}, management("external")),
		object(api.Machine, "sno-node"),
	}
}

func current(name string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return name, nil }
}

func custody() *custodyReader {
	return &custodyReader{entries: map[string]string{"lab/cluster-install-sno/kubeconfig": kubeconfig}}
}

func code(t *testing.T, err error) string {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 {
		t.Fatalf("error %v carries %d diagnostics, want one", err, len(found))
	}
	return found[0].Code
}

func TestKubeconfigRevealsTheCustodyBytesExactly(t *testing.T) {
	reader := custody()
	result, err := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: "sno"})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Material.Clear()
	value, _ := result.Material.Part(secrets.ValuePart)
	if string(value) != kubeconfig || result.Context != "lab" || result.Cluster != "sno" || !slices.Equal(result.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		t.Fatalf("result = %+v with %q", result, value)
	}
	if !slices.Equal(reader.reads, []string{"lab/cluster-install-sno/kubeconfig"}) {
		t.Fatalf("custody reads = %v", reader.reads)
	}
}

// The context is resolved once: an explicit name wins without reading the
// selection, and the current selection is used otherwise; the graph and the
// custody are both read in that one context.
func TestKubeconfigResolvesExplicitAndCurrentContexts(t *testing.T) {
	for _, test := range []struct {
		explicit, selected, want string
	}{
		{"edge", "lab", "edge"},
		{"", "edge", "edge"},
	} {
		state, reader := &effectiveState{objects: graph()}, &custodyReader{entries: map[string]string{"edge/cluster-install-sno/kubeconfig": kubeconfig}}
		selections := 0
		selection := func(context.Context) (string, error) { selections++; return test.selected, nil }
		result, err := New(state, reader, locate, selection).Kubeconfig(context.Background(), KubeconfigRequest{ContextName: test.explicit, Name: "sno"})
		if err != nil {
			t.Fatal(err)
		}
		result.Material.Clear()
		wantSelections := 1
		if test.explicit != "" {
			wantSelections = 0
		}
		if result.Context != test.want || !slices.Equal(state.contexts, []string{test.want}) || selections != wantSelections {
			t.Fatalf("explicit %q selected %q: context %q, rendered %v, selection read %d times", test.explicit, test.selected, result.Context, state.contexts, selections)
		}
	}
	_, err := New(&effectiveState{objects: graph()}, custody(), locate, current("")).Kubeconfig(context.Background(), KubeconfigRequest{Name: "sno"})
	if code(t, err) != "context.state" {
		t.Fatalf("no current context: %v", err)
	}
}

// A name the selected graph does not hold, including one only an unselected
// file declares, never selects another cluster and reads no custody.
func TestAnUnknownOrExcludedClusterIsAnAccessTarget(t *testing.T) {
	for _, name := range []string{"missing", "sno-node", ""} {
		reader := custody()
		_, err := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: name})
		if code(t, err) != "access.target" || len(reader.reads) != 0 {
			t.Fatalf("%q: %v after %d custody reads", name, err, len(reader.reads))
		}
		if remedy := diagnostics.Of(err)[0].Remediation; remedy != "name a ContainerCluster this context selects; bootwright render effective --context lab lists them" {
			t.Fatalf("%q: remediation %q", name, remedy)
		}
	}
}

func TestAnOKDClusterIsApplicable(t *testing.T) {
	reader := custody()
	_, err := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: "okd"})
	if code(t, err) != "access.unavailable" || !slices.Equal(reader.reads, []string{"lab/cluster-install-okd/kubeconfig"}) {
		t.Fatalf("an OKD cluster without custody: %v after reads %v", err, reader.reads)
	}
}

func TestManagedAndExternalStorageClustersAreNotApplicableAndReadNoCustody(t *testing.T) {
	for name, kind := range map[string]string{"ceph": "managed", "remote": "external"} {
		reader := custody()
		_, err := New(&effectiveState{objects: graph()}, reader, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: name})
		if code(t, err) != "cluster.not-applicable" || len(reader.reads) != 0 {
			t.Fatalf("%s: %v after %d custody reads", name, err, len(reader.reads))
		}
		found := diagnostics.Of(err)[0]
		want := "bootwright cluster kubeconfig does not apply to " + name + ", a " + kind + " Ceph StorageCluster; it applies to OpenShift and OKD ContainerClusters only"
		if found.Message != want || found.Remediation != "bootwright render effective --context lab lists the clusters this context selects and their kinds" {
			t.Fatalf("%s: %+v", name, found)
		}
	}
}

func TestAClusterWithoutCustodyIsUnavailable(t *testing.T) {
	_, err := New(&effectiveState{objects: graph()}, &custodyReader{}, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: "sno"})
	if code(t, err) != "access.unavailable" || diagnostics.Of(err)[0].Remediation != "bootwright apply --context lab" {
		t.Fatalf("no custody entry: %v", err)
	}
}

// Whatever refuses the context or its custody refuses the reveal, unchanged:
// an incomplete context before any custody read, and a keyring this build
// cannot open with its own identity and remedy.
func TestAnIncompleteContextRefuses(t *testing.T) {
	incomplete := diagnostics.NewFailure("context.state", "context is incomplete; repeat its init or delete command", "")
	reader := custody()
	_, err := New(&effectiveState{failure: incomplete}, reader, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: "sno"})
	if !errors.Is(err, incomplete) || len(reader.reads) != 0 {
		t.Fatalf("an incomplete context: %v after %d custody reads", err, len(reader.reads))
	}
	refused := secretstore.Failure("store.implementation", "this context's secret store is local-keyring-v3, which this Bootwright build cannot open")
	_, err = New(&effectiveState{objects: graph()}, &custodyReader{failure: refused}, locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: "sno"})
	if !errors.Is(err, refused) {
		t.Fatalf("an identity refusal became %v", err)
	}
}

func TestAnUnconfiguredServiceIsUnavailable(t *testing.T) {
	for _, service := range []Service{{}, New(nil, custody(), locate, current("lab")), New(&effectiveState{}, nil, locate, current("lab")), New(&effectiveState{}, custody(), nil, current("lab"))} {
		if _, err := service.Kubeconfig(context.Background(), KubeconfigRequest{Name: "sno"}); !errors.Is(err, availability.ErrNotImplemented) {
			t.Fatalf("an unconfigured service returned %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(&effectiveState{objects: graph()}, custody(), locate, current("lab")).Kubeconfig(ctx, KubeconfigRequest{Name: "sno"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled reveal returned %v", err)
	}
}

// Where a cluster is declared among the others changes nothing it resolves to.
func TestTheTargetDoesNotDependOnDeclarationOrder(t *testing.T) {
	objects := graph()
	for rotation := range objects {
		order := append(slices.Clone(objects[rotation:]), objects[:rotation]...)
		slices.Reverse(order)
		for _, name := range []string{"sno", "okd", "ceph", "missing"} {
			forward, forwardErr := New(&effectiveState{objects: objects}, custody(), locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: name})
			reordered, reorderedErr := New(&effectiveState{objects: order}, custody(), locate, current("lab")).Kubeconfig(context.Background(), KubeconfigRequest{Name: name})
			if (forward == nil) != (reordered == nil) || !slices.Equal(diagnostics.Of(forwardErr), diagnostics.Of(reorderedErr)) {
				t.Fatalf("%s resolved differently after reordering: %v and %v", name, forwardErr, reorderedErr)
			}
			if forward != nil {
				forward.Material.Clear()
				reordered.Material.Clear()
			}
		}
	}
}
