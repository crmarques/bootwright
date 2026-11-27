package ceph

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestCephProducedCredentialsAreSingleValueAndBoundToThePrincipal(t *testing.T) {
	for _, tc := range []struct {
		name, document, selected, other string
		decode                          func(secrets.Material, string) (secrets.Material, bool)
		want                            map[secrets.Part]string
	}{
		{"object user", `{"accessKey":"sample-access","secretKey":"sample-secret","uid":"reader"}`, "reader", "writer", ObjectUserCredentialsOf,
			map[secrets.Part]string{secrets.UsernamePart: "sample-access", secrets.PasswordPart: "sample-secret"}},
		{"client keyring", `[{"caps":{},"entity":"client.reader","key":"dGVzdA=="}]`, "reader", "writer", ClientKeyringOf,
			map[secrets.Part]string{secrets.ValuePart: "[client.reader]\n\tkey = dGVzdA==\n"}},
		{"monitoring login", monitoringLogins, "prometheus", "grafana", MonitoringCredentialsOf,
			map[secrets.Part]string{secrets.UsernamePart: "pr-admin", secrets.PasswordPart: monitoringPassword}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(tc.document)})
			defer value.Clear()
			material, ok := tc.decode(value, tc.selected)
			defer material.Clear()
			if !ok || len(material.Parts()) != len(tc.want) {
				t.Fatalf("selected principal produced %v (%t)", material.Parts(), ok)
			}
			for part, want := range tc.want {
				got, found := material.Part(part)
				matches := found && bytes.Equal(got, []byte(want))
				clear(got)
				if !matches {
					t.Errorf("selected principal did not yield the expected %s", part)
				}
			}
			mixed := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(tc.document), secrets.PasswordPart: []byte("extraneous")})
			defer mixed.Clear()
			for _, attempt := range []struct {
				value     secrets.Material
				principal string
			}{{mixed, tc.selected}, {value, tc.other}} {
				got, ok := tc.decode(attempt.value, attempt.principal)
				parts := got.Parts()
				got.Clear()
				if ok || len(parts) != 0 {
					t.Errorf("invalid envelope or wrong principal produced parts %v (%t)", parts, ok)
				}
			}
		})
	}
}

func TestCephAdminCredentialIdentityKeepsClusterIncarnationAndLoginScope(t *testing.T) {
	ctx := context.Background()
	capability := New(api.CephCluster, nil)
	execution := credentialExecution(t)
	cluster := coreRequest()
	for _, tc := range []struct {
		login     lifecycle.AdminLogin
		principal string
	}{
		{dashboardLogin, "dashboard user " + DashboardUser},
		{lifecycle.AdminLogin{Custody: lifecycle.AdminProducedLogin}, "monitoring basic auth"},
		{lifecycle.AdminLogin{Custody: lifecycle.AdminBoundSecret}, "grafana admin"},
	} {
		want := "cluster fsid " + cluster.FSID + " created " + monmapCreatedAt + " as " + tc.principal
		if got, err := capability.AdminCredentialIdentity(ctx, execution, tc.login); err != nil || got != want {
			t.Fatalf("credential identity = %q (%v), want %q", got, err, want)
		}
	}
	execution.PreviousEvidence = nil
	if got, err := capability.AdminCredentialIdentity(ctx, execution, dashboardLogin); err == nil || got != "" {
		t.Fatalf("missing evidence returned identity %q (%v)", got, err)
	}
}

func TestCephGeneratedMonitoringCredentialRejectsBiasedBytesAndShortEntropy(t *testing.T) {
	ctx := context.Background()
	capability := New(api.CephCluster, nil)
	login := lifecycle.AdminLogin{Custody: lifecycle.AdminProducedLogin}
	var reads int
	material, err := capability.GenerateAdminCredential(ctx, login, func(buffer []byte) (int, error) {
		reads++
		// 255 is outside the largest whole multiple of the alphabet length.
		// A full rejected buffer must cause a fresh read.
		sample := byte(255)
		if reads > 1 {
			sample = 0
		}
		copy(buffer, bytes.Repeat([]byte{sample}, len(buffer)))
		return len(buffer), nil
	})
	defer material.Clear()
	password, ok := material.Part(secrets.ValuePart)
	matches := bytes.Equal(password, []byte(strings.Repeat(dashboardAlphabet[:1], dashboardLength)))
	clear(password)
	if err != nil || !ok || !matches || reads != 2 || !slices.Equal(material.Parts(), []secrets.Part{secrets.ValuePart}) {
		t.Fatalf("unexpected generated material shape or entropy consumption: %v, %d reads, %v", material.Parts(), reads, err)
	}
	bad, err := capability.GenerateAdminCredential(ctx, login, func(buffer []byte) (int, error) {
		return len(buffer) - 1, nil
	})
	defer bad.Clear()
	if err == nil || len(bad.Parts()) != 0 {
		t.Fatalf("short entropy returned %v (%v)", bad.Parts(), err)
	}
}
