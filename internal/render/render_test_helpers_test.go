package render_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func firstMachineComponent(t *testing.T, cluster map[string]any) map[string]any {
	t.Helper()
	return componentByKind(t, cluster, v1alpha1.ComponentSlotMachines)
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

func defaultEndpointRefs() v1alpha1.ContainerEndpointRefs {
	return v1alpha1.ContainerEndpointRefs{
		API:     v1alpha1.EndpointRef{Name: v1alpha1.EndpointAPI},
		APIInt:  v1alpha1.EndpointRef{Name: "api-int"},
		Ingress: v1alpha1.EndpointRef{Name: "apps"},
	}
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

func firstProviderServiceByKind(t *testing.T, services []any, kind string) map[string]any {
	t.Helper()
	for _, raw := range services {
		service := raw.(map[string]any)
		if service["kind"] == kind {
			return service
		}
	}
	t.Fatalf("no %s provider service in %v", kind, services)
	return nil
}

func setArtifactHTTPPort(state *v1alpha1.State, port int) {
	for i := range state.InfraComponents {
		server := state.InfraComponents[i].Spec.ArtifactServer
		if server == nil {
			continue
		}
		for j := range server.Listeners {
			if server.Listeners[j].Name == v1alpha1.ArtifactServerProtocolHTTPS {
				server.Listeners[j].Port = port
			}
		}
	}
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ab, err := os.ReadFile(a)
	if err != nil {
		t.Fatalf("read %s: %v", a, err)
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		t.Fatalf("read %s: %v", b, err)
	}
	// Compare structurally (decoded YAML) so unrelated whitespace or
	// key-order shifts inside the yaml library don't flake the test.
	var ad, bd any
	if err := yaml.Unmarshal(ab, &ad); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := yaml.Unmarshal(bb, &bd); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return deepEqualYAML(ad, bd)
}

// deepEqualYAML compares two yaml-decoded values without depending on
// reflect.DeepEqual semantics for map[string]any (which is order-
// insensitive on its own).
func deepEqualYAML(a, b any) bool {
	ay, _ := yaml.Marshal(a)
	by, _ := yaml.Marshal(b)
	return bytes.Equal(ay, by)
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// compile-time check that render.Result hasn't lost the fields the
// CLI prints and the workflow passes downstream — quietly removing one
// would mask the breakage; surface it here at compile time.
var _ = fmt.Sprintf("%s %s %s %s %s",
	render.Result{}.EffectiveStatePath,
	render.Result{}.LockPath,
	render.Result{}.InventoryPath,
	render.Result{}.VarsPath,
	render.Result{}.ArtifactsDir,
)
