package compilation_test

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func smbDiagnosticsAt(t *testing.T, probe, kind, name, field string) []diagnostics.Diagnostic {
	t.Helper()
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+"---\n"+probe))
	found := []diagnostics.Diagnostic{}
	for _, d := range requireCompilationFailure(t, state, report, err) {
		if d.Field == field && (d.Object == nil || d.Object.Kind == kind && d.Object.Name == name) {
			found = append(found, d)
		}
	}
	return found
}

func TestDeclaredPoolCannotClaimDerivedSMBPool(t *testing.T) {
	probe := "apiVersion: bootwright.io/v1alpha1\nkind: CephPool\nmetadata:\n  name: .smb\nspec:\n  clusterRef: missing\n  role: rbd\n"
	found := smbDiagnosticsAt(t, probe, "CephPool", ".smb", "$.metadata.name")
	if len(found) != 1 || found[0].Code != "api.value" || found[0].Message != "CephPool metadata.name must be a lowercase DNS label" {
		t.Fatalf("a declared .smb pool was not refused by name: %+v", found)
	}
}

func TestSMBPasswordBindingRefusesANonValueSecret(t *testing.T) {
	probe := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata:\n  name: smb-cert\nspec:\n  type: tlsCertificate\n---\n" +
		"apiVersion: bootwright.io/v1alpha1\nkind: CephSMBResource\nmetadata:\n  name: smb-users\nspec:\n  clusterRef: missing\n  secrets:\n    - path: /values/users/0/password\n      secretRef: smb-cert\n" +
		"  native:\n    resource_type: ceph.smb.usersgroups\n    users_groups_id: users\n    values:\n      users:\n        - name: alice\n      groups: []\n"
	found := smbDiagnosticsAt(t, probe, "CephSMBResource", "smb-users", "$.spec.secrets[0].secretRef")
	if len(found) != 1 || found[0].Code != "api.reference" || found[0].Message != "Secret/smb-cert is declared as tlsCertificate; this field accepts opaque, token" {
		t.Fatalf("a non-value Secret bound an SMB password: %+v", found)
	}
}
