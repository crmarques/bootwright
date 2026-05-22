package v1alpha1

import "testing"

func TestReleaseImageSource(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{"quay.io/okd/scos-release:4.20.0-okd-scos.13", "quay.io/okd/scos-release"},
		{"registry.example.test:5000/ns/release@sha256:abc", "registry.example.test:5000/ns/release"},
		{"registry.example.test:5000/ns/release:tag", "registry.example.test:5000/ns/release"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ImageReferenceSource(tc.image); got != tc.want {
			t.Fatalf("ImageReferenceSource(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

func TestDefaultReleaseImageDigestSourcesUsesExplicitOKDImage(t *testing.T) {
	cluster := ContainerCluster{
		Spec: ContainerClusterSpec{
			Distribution: DistributionSpec{
				Type: DistributionOKD,
				Release: ReleaseSpec{
					Image: "quay.io/okd/scos-release:4.20.0-okd-scos.13",
				},
			},
		},
	}
	got := DefaultReleaseImageDigestSources(cluster, "registry.example.test:5000")
	if len(got) != 1 {
		t.Fatalf("DefaultReleaseImageDigestSources len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Source != "quay.io/okd/scos-release" {
		t.Fatalf("source = %q, want OKD release image repository", got[0].Source)
	}
}

func TestDefaultReleaseImageDigestSourcesOpenShiftDefaults(t *testing.T) {
	cluster := ContainerCluster{
		Spec: ContainerClusterSpec{
			Distribution: DistributionSpec{
				Type:    DistributionOpenShift,
				Release: ReleaseSpec{Version: "4.20.15"},
			},
		},
	}
	got := DefaultReleaseImageDigestSources(cluster, "registry.example.test:5000")
	if len(got) != 2 {
		t.Fatalf("DefaultReleaseImageDigestSources len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Source != OCPReleaseSourceQuayOCPRelease || got[1].Source != OCPReleaseSourceQuayARTDev {
		t.Fatalf("sources = %#v, want OCP defaults", got)
	}
}
