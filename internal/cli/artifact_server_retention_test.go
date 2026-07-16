package cli

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInstallOnlyArtifactServerTargets(t *testing.T) {
	state := v1alpha1.State{
		InfraComponents: []v1alpha1.InfraComponent{
			artifactServerInfraComponent("keep", v1alpha1.ArtifactServerRetentionPersistent),
			artifactServerInfraComponent("gone", v1alpha1.ArtifactServerRetentionInstallOnly),
			artifactServerInfraComponent("default", ""),
		},
	}
	targets := installOnlyArtifactServerTargets(state)
	if len(targets) != 1 {
		t.Fatalf("want 1 install-only target, got %d", len(targets))
	}
	if targets[0].RecordName != "InfraComponent-gone" {
		t.Fatalf("record name = %q, want InfraComponent-gone", targets[0].RecordName)
	}
}

func artifactServerInfraComponent(name, retention string) v1alpha1.InfraComponent {
	return v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.InfraComponentSpec{
			Type:           v1alpha1.ComponentSlotArtifactServer,
			ArtifactServer: &v1alpha1.ArtifactServerComponent{Retention: retention},
		},
	}
}
