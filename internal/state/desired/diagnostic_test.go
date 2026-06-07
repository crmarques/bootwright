package desiredstate

import (
	"errors"
	"testing"
)

func TestDiagnosticsMapRemovedInstallField(t *testing.T) {
	err := errors.New("decode /tmp/input/cluster.yaml document 1: yaml: unmarshal errors:\n  line 10: field baseDomain not found in type v1alpha1.OCPInstallSpec")
	diagnostics := Diagnostics(err)
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics returned %d entries, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Object != "ContainerCluster" || got.Field != "spec.install.baseDomain" || got.Value != "" {
		t.Fatalf("diagnostic owner = (%q, %q, %q), want ContainerCluster spec.install.baseDomain with empty value", got.Object, got.Field, got.Value)
	}
	if got.Rule != "spec.install.baseDomain is not accepted on ContainerCluster install intent" {
		t.Fatalf("rule = %q", got.Rule)
	}
	if got.Remediation != "set Environment.spec.baseDomain instead" {
		t.Fatalf("remediation = %q", got.Remediation)
	}
}

func TestDiagnosticsMapUnknownInstallField(t *testing.T) {
	err := errors.New("decode /tmp/input/cluster.yaml document 1: yaml: unmarshal errors:\n  line 10: field customOverride not found in type v1alpha1.OCPInstallSpec")
	diagnostics := Diagnostics(err)
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics returned %d entries, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Object != "ContainerCluster" || got.Field != "spec.install.customOverride" {
		t.Fatalf("diagnostic owner = (%q, %q), want ContainerCluster spec.install.customOverride", got.Object, got.Field)
	}
	if got.Remediation != "remove spec.install.customOverride or move the fact to the desired-state object that owns it" {
		t.Fatalf("remediation = %q", got.Remediation)
	}
}
