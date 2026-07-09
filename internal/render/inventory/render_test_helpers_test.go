package inventory

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const fixtureRoot = "../../state/desired/testdata/good"

func firstMachineComponent(t *testing.T, cluster map[string]any) map[string]any {
	t.Helper()
	return componentByKind(t, cluster, v1alpha1.ServiceKindMachines)
}

func machineByName(t *testing.T, state v1alpha1.State, name string) v1alpha1.Machine {
	t.Helper()
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine
		}
	}
	t.Fatalf("Machine/%s not found", name)
	return v1alpha1.Machine{}
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

func setArtifactTLS(state *v1alpha1.State, tls *v1alpha1.ArtifactServerTLS) {
	for i := range state.InfraComponents {
		if server := state.InfraComponents[i].Spec.ArtifactServer; server != nil {
			server.TLS = tls
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

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
