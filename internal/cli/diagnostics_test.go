package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestDiagnosticsMapRemovedInstallField(t *testing.T) {
	err := errors.New("decode /tmp/input/cluster.yaml document 1: yaml: unmarshal errors:\n  line 10: field baseDomain not found in type v1alpha1.OCPInstallSpec")
	diagnostics := diagnosticsFromError(err)
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
	if got.Remediation != "set Environment.spec.domains.base instead" {
		t.Fatalf("remediation = %q", got.Remediation)
	}
}

func TestDiagnosticsMapRenamedCephManagementDNSName(t *testing.T) {
	err := errors.New("decode /tmp/input/cluster.yaml document 1: yaml: unmarshal errors:\n  line 24: field dnsName not found in type v1alpha1.StorageCephManagement")
	diagnostics := diagnosticsFromError(err)
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics returned %d entries, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Object != "StorageCluster" || got.Field != "spec.ceph.management.dnsName" {
		t.Fatalf("diagnostic owner = (%q, %q), want StorageCluster spec.ceph.management.dnsName", got.Object, got.Field)
	}
	if !strings.Contains(got.Remediation, "spec.ceph.management.dnsLabel") {
		t.Fatalf("remediation = %q, want the dnsLabel successor named", got.Remediation)
	}
}

func TestDiagnosticsMapRenamedGatewayPublicDNSName(t *testing.T) {
	err := errors.New("decode /tmp/input/rgw.yaml document 1: yaml: unmarshal errors:\n  line 8: field dnsName not found in type v1alpha1.StorageObjectGatewayPublic")
	diagnostics := diagnosticsFromError(err)
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics returned %d entries, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Object != "StorageObjectGateway" || got.Field != "spec.public.dnsName" {
		t.Fatalf("diagnostic owner = (%q, %q), want StorageObjectGateway spec.public.dnsName", got.Object, got.Field)
	}
	if !strings.Contains(got.Remediation, "spec.public.dnsLabel") {
		t.Fatalf("remediation = %q, want the dnsLabel successor named", got.Remediation)
	}
}

func TestDiagnosticsMapUnknownInstallField(t *testing.T) {
	err := errors.New("decode /tmp/input/cluster.yaml document 1: yaml: unmarshal errors:\n  line 10: field customOverride not found in type v1alpha1.OCPInstallSpec")
	diagnostics := diagnosticsFromError(err)
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

func TestDiagnosticsOnlySupportedFieldSuggestsRemoval(t *testing.T) {
	diagnostics := diagnosticsFromError(errors.New("ContainerCluster/sno-baremetal spec.distribution.release.channel is only supported for openshift"))
	if len(diagnostics) != 1 {
		t.Fatalf("diagnosticsFromError returned %d entries, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Object != "ContainerCluster/sno-baremetal" || got.Field != "spec.distribution.release.channel" {
		t.Fatalf("extraction = (%q, %q), want ContainerCluster/sno-baremetal spec.distribution.release.channel", got.Object, got.Field)
	}
	if got.Remediation != "remove spec.distribution.release.channel from ContainerCluster/sno-baremetal" {
		t.Fatalf("remediation = %q, want a removal hint, not 'set ... to a valid value'", got.Remediation)
	}
}

func TestDiagnosticsBadValueFieldSuggestsCorrection(t *testing.T) {
	diagnostics := diagnosticsFromError(errors.New(`ContainerCluster/sno spec.networking.clusterNetwork[0].cidr "not-a-cidr" is not a valid CIDR`))
	if len(diagnostics) != 1 {
		t.Fatalf("diagnosticsFromError returned %d entries, want 1", len(diagnostics))
	}
	if got := diagnostics[0].Remediation; got != "set spec.networking.clusterNetwork[0].cidr on ContainerCluster/sno to a valid value" {
		t.Fatalf("remediation = %q, want a set-to-valid-value hint", got)
	}
}

func TestDiagnosticsExtractObjectFieldValueFromMessage(t *testing.T) {
	diagnostics := diagnosticsFromError(errors.New(`ContainerCluster/sno spec.networking.clusterNetwork[0].cidr "not-a-cidr" is not a valid CIDR`))
	if len(diagnostics) != 1 {
		t.Fatalf("diagnosticsFromError returned %d entries, want 1", len(diagnostics))
	}
	got := diagnostics[0]
	if got.Object != "ContainerCluster/sno" || got.Field != "spec.networking.clusterNetwork[0].cidr" || got.Value != "not-a-cidr" {
		t.Fatalf("extraction = (%q, %q, %q), want ContainerCluster/sno spec.networking.clusterNetwork[0].cidr not-a-cidr", got.Object, got.Field, got.Value)
	}
}
