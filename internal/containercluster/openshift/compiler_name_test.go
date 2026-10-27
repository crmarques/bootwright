package openshift_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestClusterNameAdmissionDiagnosticNamesItsObject(t *testing.T) {
	base := strings.Replace(clusterYAML, "  install:\n    endpoints:", "  install:\n    nodeSSH:\n      keyPairRef: cluster-cluster-admin-ssh-key\n    endpoints:", 1)
	content := strings.Replace(base, "kind: OpenShiftCluster\nmetadata:\n  name: cluster", "kind: OpenShiftCluster\nmetadata:\n  name: "+strings.Repeat("a", 63), 1)
	if _, _, err := compiler().Compile(context.Background(), sources(content)); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
}
