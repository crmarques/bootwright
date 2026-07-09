package render_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
)

func firstMachineComponent(t *testing.T, cluster map[string]any) map[string]any {
	t.Helper()
	return componentByKind(t, cluster, v1alpha1.ServiceKindMachines)
}

func writeEncryptedContextSecret(t *testing.T, dir, name string, role secretstore.MaterialRole, data []byte) {
	t.Helper()
	store := secretstore.NewContextStore("test", dir)
	if err := store.Write(secretstore.MaterialKey{Name: name, Role: role}, data); err != nil {
		t.Fatalf("write encrypted %s/%s: %v", name, role, err)
	}
}

func containerClusterByName(t *testing.T, state v1alpha1.State, name string) v1alpha1.ContainerCluster {
	t.Helper()
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return cluster
		}
	}
	t.Fatalf("ContainerCluster/%s not found", name)
	return v1alpha1.ContainerCluster{}
}

func componentByKind(t *testing.T, cluster map[string]any, kind string) map[string]any {
	t.Helper()
	comps, ok := cluster["components"].([]any)
	if !ok {
		t.Fatalf("cluster has no components: %v", cluster)
	}
	for _, c := range comps {
		entry := c.(map[string]any)
		if entry["kind"] == kind {
			return entry
		}
	}
	t.Fatalf("no %s component in cluster: %v", kind, cluster)
	return nil
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode got %#o, want %#o", path, got, want)
	}
}

func assertDirMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode got %#o, want %#o", path, got, want)
	}
}

var _ = fmt.Sprintf("%s %s %s %s %s",
	render.Result{}.EffectiveStatePath,
	render.Result{}.LockPath,
	render.Result{}.InventoryPath,
	render.Result{}.VarsPath,
	render.Result{}.ArtifactsDir,
)

func clustersByName(t *testing.T, vars map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, raw := range vars["bootwright_clusters"].([]any) {
		cluster := raw.(map[string]any)
		out[cluster["name"].(string)] = cluster
	}
	return out
}
