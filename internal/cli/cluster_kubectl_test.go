package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

const clusterKubeconfigFixtureContent = "apiVersion: v1\nkind: Config\nclusters: []\n"

func seedClusterKubeconfig(t *testing.T, contextName, clustersDir, cluster string) {
	t.Helper()
	store := secret.NewContextStore(contextName, workflow.ClusterSecretsDir(clustersDir, cluster))
	if err := store.Write(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary}, []byte(clusterKubeconfigFixtureContent)); err != nil {
		t.Fatalf("seed encrypted kubeconfig: %v", err)
	}
}

func TestClusterKubeClientRunsWithDecryptedKubeconfig(t *testing.T) {
	for _, binary := range []string{"oc", "kubectl"} {
		t.Run(binary, func(t *testing.T) {
			ctx := initTestContext(t, "001-sno-libvirt")
			seedClusterKubeconfig(t, ctx.Name, ctx.ClustersDir, "sno-libvirt")

			var gotBinary string
			var gotArgs []string
			var gotKubeconfig string
			previous := defaultClusterKubectlDeps
			defaultClusterKubectlDeps = clusterKubectlDeps{
				run: func(_ context.Context, inv clusterKubectlInvocation) (int, error) {
					gotBinary = inv.Binary
					gotArgs = append([]string(nil), inv.Args...)
					data, err := os.ReadFile(inv.KubeconfigPath)
					if err != nil {
						t.Fatalf("read materialized kubeconfig: %v", err)
					}
					gotKubeconfig = string(data)
					return 0, nil
				},
			}
			t.Cleanup(func() { defaultClusterKubectlDeps = previous })

			_, stderr, code := runCLI(t, "cluster", binary, "--name", "sno-libvirt", "get", "pods", "-n", "openshift-ingress", "-o", "json")
			if code != 0 {
				t.Fatalf("cluster %s exited %d, stderr=%q", binary, code, stderr)
			}
			if gotBinary != binary {
				t.Fatalf("binary = %q, want %q", gotBinary, binary)
			}
			if strings.Join(gotArgs, " ") != "get pods -n openshift-ingress -o json" {
				t.Fatalf("passthrough args = %v, want the command verbatim with its own flags", gotArgs)
			}
			if gotKubeconfig != clusterKubeconfigFixtureContent {
				t.Fatalf("materialized kubeconfig = %q, want the decrypted admin kubeconfig", gotKubeconfig)
			}
		})
	}
}

func TestClusterKubeClientRemovesMaterializedKubeconfig(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	seedClusterKubeconfig(t, ctx.Name, ctx.ClustersDir, "sno-libvirt")

	var kubeconfigPath string
	previous := defaultClusterKubectlDeps
	defaultClusterKubectlDeps = clusterKubectlDeps{
		run: func(_ context.Context, inv clusterKubectlInvocation) (int, error) {
			kubeconfigPath = inv.KubeconfigPath
			return 0, nil
		},
	}
	t.Cleanup(func() { defaultClusterKubectlDeps = previous })

	if _, stderr, code := runCLI(t, "cluster", "oc", "--name", "sno-libvirt", "get", "nodes"); code != 0 {
		t.Fatalf("cluster oc exited %d, stderr=%q", code, stderr)
	}
	if kubeconfigPath == "" {
		t.Fatal("cluster oc did not materialize a kubeconfig")
	}
	if _, err := os.Stat(kubeconfigPath); !os.IsNotExist(err) {
		t.Fatalf("materialized kubeconfig %s was not removed after the command, stat err=%v", kubeconfigPath, err)
	}
}

func TestClusterKubeClientPreservesExitCode(t *testing.T) {
	ctx := initTestContext(t, "001-sno-libvirt")
	seedClusterKubeconfig(t, ctx.Name, ctx.ClustersDir, "sno-libvirt")

	previous := defaultClusterKubectlDeps
	defaultClusterKubectlDeps = clusterKubectlDeps{
		run: func(context.Context, clusterKubectlInvocation) (int, error) { return 23, nil },
	}
	t.Cleanup(func() { defaultClusterKubectlDeps = previous })

	if _, _, code := runCLI(t, "cluster", "kubectl", "--name", "sno-libvirt", "get", "nonexistent"); code != 23 {
		t.Fatalf("cluster kubectl exit code = %d, want 23", code)
	}
}

func TestClusterKubeClientRequiresCommand(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	_, stderr, code := runCLI(t, "cluster", "oc", "--name", "sno-libvirt")
	if code != 2 {
		t.Fatalf("cluster oc without a command exited %d, want 2\nstderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "requires a command") {
		t.Fatalf("stderr missing missing-command guidance: %q", stderr)
	}
}

func TestClusterKubeClientRequiresName(t *testing.T) {
	initTestContext(t, "001-sno-libvirt")
	if _, _, code := runCLI(t, "cluster", "oc", "get", "nodes"); code == 0 {
		t.Fatal("cluster oc without --name unexpectedly succeeded")
	}
}

func TestClusterKubeClientRejectsNonContainerCluster(t *testing.T) {
	called := false
	previous := defaultClusterKubectlDeps
	defaultClusterKubectlDeps = clusterKubectlDeps{
		run: func(context.Context, clusterKubectlInvocation) (int, error) { called = true; return 0, nil },
	}
	t.Cleanup(func() { defaultClusterKubectlDeps = previous })

	initTestContext(t, cephFixture)
	_, stderr, code := runCLI(t, "cluster", "oc", "--name", "ceph-libvirt", "get", "nodes")
	if code != 2 {
		t.Fatalf("cluster oc against a storage cluster exited %d, want 2\nstderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "unknown cluster(s): ceph-libvirt") {
		t.Fatalf("stderr missing container-only rejection: %q", stderr)
	}
	if called {
		t.Fatal("cluster oc ran the client for a non-container cluster")
	}
}
