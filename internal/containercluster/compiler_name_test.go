package containercluster_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestClusterNameAdmissionDiagnosticNamesItsObject(t *testing.T) {
	base := strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    nodeSSH:\n      keyPairRef: cluster-cluster-admin-ssh-key\n    endpoints:", 1)
	for _, size := range []int{47, 48} {
		name := strings.Repeat("a", size)
		content := strings.Replace(base, "kind: ContainerCluster\nmetadata:\n  name: cluster", "kind: ContainerCluster\nmetadata:\n  name: "+name, 1)
		_, _, err := compiler().Compile(context.Background(), sources(content))
		if size == 47 {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		found := false
		for _, issue := range diagnostics.Of(err) {
			if issue.Field == "$.metadata.name" && issue.Code == "api.invariant" && issue.Object != nil && issue.Object.Kind == "ContainerCluster" && issue.Object.Name == name && strings.Contains(issue.Remediation, "47") {
				found = true
			}
		}
		if !found {
			t.Fatalf("long name diagnostic did not name its object and field: %v", diagnostics.Of(err))
		}
	}
}
