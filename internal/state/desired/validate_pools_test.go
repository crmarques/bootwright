package desiredstate

import (
	"strings"
	"testing"
)

func TestRemovedMachinePoolFieldsRejectUnknown(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "compute",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: demo }
spec:
  compute:
    - replicas: 3
`,
			want: `field "compute" is not authored here; control-plane and compute replica counts are derived from spec.nodes[].role`,
		},
		{
			name: "control-plane",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: demo }
spec:
  controlPlane:
    replicas: 3
`,
			want: `field "controlPlane" is not authored here; control-plane and compute replica counts are derived from spec.nodes[].role`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"cluster.yaml": tc.body})
			_, err := LoadNormalizeValidate([]string{dir})
			if err == nil {
				t.Fatal("expected unknown field error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}
