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
			name: "compute-architecture",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: demo }
spec:
  compute:
    - replicas: 3
      architecture: arm64
`,
			want: "field architecture not found",
		},
		{
			name: "compute-hyperthreading",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: demo }
spec:
  compute:
    - replicas: 3
      hyperthreading: Disabled
`,
			want: "field hyperthreading not found",
		},
		{
			name: "control-plane-platform",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: demo }
spec:
  controlPlane:
    replicas: 3
    platform: {}
`,
			want: "field platform not found",
		},
		{
			name: "compute-name",
			body: `apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata: { name: demo }
spec:
  compute:
    - replicas: 3
      name: custom
`,
			want: "field name not found",
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
