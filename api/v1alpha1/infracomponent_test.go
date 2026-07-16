package v1alpha1

import "testing"

func TestArtifactServerRetentionMode(t *testing.T) {
	var nilServer *ArtifactServerComponent
	if got := nilServer.RetentionMode(); got != ArtifactServerRetentionPersistent {
		t.Fatalf("nil server = %q, want %q", got, ArtifactServerRetentionPersistent)
	}
	if got := (&ArtifactServerComponent{}).RetentionMode(); got != ArtifactServerRetentionPersistent {
		t.Fatalf("empty retention = %q, want %q", got, ArtifactServerRetentionPersistent)
	}
	if got := (&ArtifactServerComponent{Retention: ArtifactServerRetentionInstallOnly}).RetentionMode(); got != ArtifactServerRetentionInstallOnly {
		t.Fatalf("install-only = %q, want %q", got, ArtifactServerRetentionInstallOnly)
	}
}
