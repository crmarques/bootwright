//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

const snoKubeconfig = "apiVersion: v1\nclusters:\n- cluster:\n    server: https://api.sno.lab.example.test:6443\n  name: sno\nkind: Config\n"

// retiredRemedy is how every access to a keyring this build no longer opens
// tells the operator out.
const retiredRemedy = "destroy this context's effects with the Bootwright build that created it, then run bootwright context delete --name <context> --purge and create the context again"

// producedCustody is what the lifecycle and the reveal reach of the composed
// custody service.
type producedCustody interface {
	Produce(context.Context, secretstore.Context, secretstore.Area, custody.ProduceRequest) ([]secretstore.Produced, error)
	Withdraw(context.Context, secretstore.Context, secretstore.Area) (bool, error)
	ReadProduced(context.Context, custody.ReadProducedRequest) (secretstore.ProducedMaterial, bool, error)
}

func composedCustody(t *testing.T, services cli.Services) producedCustody {
	t.Helper()
	composed, ok := services.Secrets.(producedCustody)
	if !ok {
		t.Fatal("the composed custody service offers no produced material")
	}
	return composed
}

// produceThroughLentArea publishes one produced entry as the engine does: in
// a lifecycle transaction, through the secret area it lends.
func produceThroughLentArea(t *testing.T, services cli.Services, repository *contextfs.Store, contextName, block, name, value string) {
	t.Helper()
	ctx := context.Background()
	material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
	defer material.Clear()
	err := repository.MutateLifecycle(ctx, contextName, func(tx lifecycle.Transaction) error {
		return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			_, err := composedCustody(t, services).Produce(ctx, selected, area, custody.ProduceRequest{Block: block, Outputs: []secretstore.ProducedInput{{Name: name, Material: material}}})
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func withdrawThroughLentArea(t *testing.T, services cli.Services, repository *contextfs.Store, contextName string) bool {
	t.Helper()
	ctx := context.Background()
	withdrawn := false
	err := repository.MutateLifecycle(ctx, contextName, func(tx lifecycle.Transaction) error {
		return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			var err error
			withdrawn, err = composedCustody(t, services).Withdraw(ctx, selected, area)
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return withdrawn
}

func readProduced(t *testing.T, services cli.Services, contextName, block, name string) (string, bool) {
	t.Helper()
	read, found, err := composedCustody(t, services).ReadProduced(context.Background(), custody.ReadProducedRequest{ContextName: contextName, Block: block, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	material := read.Material
	defer material.Clear()
	value, _ := material.Part(secrets.ValuePart)
	defer clear(value)
	return string(value), found
}

// snoContext initializes a context over the lab-sno example beside an external
// Ceph StorageCluster, the shape the cluster access commands resolve names in.
func snoContext(t *testing.T) (cli.Services, *contextfs.Store, string) {
	t.Helper()
	services, repository, _, root := contextFixture(t)
	input := filepath.Join(t.TempDir(), "lab-sno")
	base, err := filepath.Abs(filepath.Join("..", "..", "examples", "lab-sno"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range snoExampleSources(t).Files {
		relative, err := filepath.Rel(base, file.Path())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(input, filepath.Dir(relative)), 0700); err != nil {
			t.Fatal(err)
		}
		addSecretInput(t, filepath.Join(input, filepath.Dir(relative)), filepath.Base(relative), string(file.Bytes()))
	}
	if err := os.MkdirAll(filepath.Join(input, "storage"), 0700); err != nil {
		t.Fatal(err)
	}
	addSecretInput(t, filepath.Join(input, "storage"), "ceph.yaml", "apiVersion: bootwright.io/v1alpha1\nkind: StorageCluster\nmetadata:\n  name: ceph\nspec:\n  type: ceph\n  management: external\n")
	contextRun(t, services, 0, "context", "init", "--name", "lab-sno", "--input-dir", input)
	return services, repository, root
}

func singleDiagnostic(t *testing.T, stderr, code string) {
	t.Helper()
	if strings.Count(stderr, "\n") != 1 || !strings.HasPrefix(stderr, "[FAIL] "+code+": ") {
		t.Fatalf("standard error = %q, want exactly one %s diagnostic", stderr, code)
	}
}

// The complete journey: an apply's proved completion leaves the kubeconfig in
// custody, cluster kubeconfig reveals exactly those bytes for an explicit or
// the current context, refuses a StorageCluster and an unknown name before any
// custody read, and once a completed removal withdraws the entry it reports
// the access unavailable, naming the apply that restores it.
func TestClusterKubeconfigJourney(t *testing.T) {
	services, repository, _ := snoContext(t)
	block := agentinstall.InstallBlockID("sno")
	out, stderr := contextRun(t, services, 1, "cluster", "kubeconfig", "--context", "lab-sno", "--name", "sno")
	if out != "" {
		t.Fatalf("an unavailable kubeconfig wrote %q", out)
	}
	singleDiagnostic(t, stderr, "access.unavailable")
	produceThroughLentArea(t, services, repository, "lab-sno", block, agentinstall.KubeconfigOutput, snoKubeconfig)
	for _, args := range [][]string{{"--context", "lab-sno", "--name", "sno"}, {"--name", "sno"}} {
		out, stderr := contextRun(t, services, 0, append([]string{"cluster", "kubeconfig"}, args...)...)
		if out != snoKubeconfig || stderr != "" {
			t.Fatalf("%v revealed %q with standard error %q", args, out, stderr)
		}
	}
	for name, code := range map[string]string{"ceph": "cluster.not-applicable", "missing": "access.target", "sno-01": "access.target"} {
		out, stderr := contextRun(t, services, 1, "cluster", "kubeconfig", "--context", "lab-sno", "--name", name)
		if out != "" {
			t.Fatalf("%s wrote %q", name, out)
		}
		singleDiagnostic(t, stderr, code)
		if !strings.Contains(stderr, "bootwright render effective --context lab-sno") {
			t.Fatalf("%s names no discovery action: %q", name, stderr)
		}
	}
	if !withdrawThroughLentArea(t, services, repository, "lab-sno") {
		t.Fatal("the removal withdrew nothing")
	}
	out, stderr = contextRun(t, services, 1, "cluster", "kubeconfig", "--name", "sno")
	if out != "" || !strings.Contains(stderr, "; next: bootwright apply --context lab-sno") {
		t.Fatalf("a withdrawn kubeconfig wrote %q with %q", out, stderr)
	}
	singleDiagnostic(t, stderr, "access.unavailable")
}

// A keyring whose persisted identity this build's catalog lacks, the format
// three one included, refuses every access with that identity and the way
// out, through the resolver composition binds by default.
func TestTheComposedStoreRefusesAV3Keyring(t *testing.T) {
	services, _, root := snoContext(t)
	record := filepath.Join(root, "contexts", "lab-sno", "secrets", secretstore.RecordPath)
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	retired := bytes.Replace(data, []byte(`"local-keyring-v4"`), []byte(`"local-keyring-v3"`), 1)
	if bytes.Equal(retired, data) {
		t.Fatalf("the store record names no local-keyring-v4 backend: %s", data)
	}
	if err := os.WriteFile(record, retired, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"secret", "list"}, {"secret", "encryption", "init"}, {"cluster", "kubeconfig", "--name", "sno"},
	} {
		out, stderr := contextRun(t, services, 1, args...)
		if out != "" || !strings.Contains(stderr, "local-keyring-v3") || !strings.HasSuffix(stderr, "; next: "+retiredRemedy+"\n") {
			t.Fatalf("%v wrote %q with %q", args, out, stderr)
		}
		singleDiagnostic(t, stderr, "secret.store.implementation")
	}
}
