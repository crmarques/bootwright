//go:build linux && amd64

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestRemovedSingleValueFieldsRefuseAsUnknownFields(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		example, suffix, old, replacement, field string
	}{
		"password authentication": {"lab-rhel", "infra/os/rhel-9-8.yaml", "      timezone: Etc/UTC\n",
			"      timezone: Etc/UTC\n\n    ssh:\n      passwordAuthentication: false\n", "$.spec.customizations.ssh"},
		"Insights enrollment": {hostVariants, "storage/rhel-entitlement.yaml", "    activationKeyRef: rhsm-key\n",
			"    activationKeyRef: rhsm-key\n    connectToInsights: false\n", "$.spec.rhsm.connectToInsights"},
		"sudo password": {"lab-rhel-mirror-host", "infra/machines/mirror.yaml", "      knownHostsRef: mirror-known-hosts\n",
			"      knownHostsRef: mirror-known-hosts\n      sudoPasswordRef: mirror-sudo\n", "$.spec.access.ssh.sudoPasswordRef"},
	} {
		t.Run(name, func(t *testing.T) {
			sources := patchedSources(t, exampleDirectory(t, test.example), test.suffix, test.old, test.replacement, nil)
			_, _, err := wireCompiler().Compile(context.Background(), sources)
			found := diagnostics.Of(err)
			if len(found) != 1 {
				t.Fatalf("diagnostics = %+v, want one unknown-field refusal", found)
			}
			got := found[0]
			if got.Code != "api.field" || got.Field != test.field || !strings.HasPrefix(got.Message, "unknown field; ") ||
				got.Remediation != "remove it or use a permitted field" || got.Source == nil || !strings.HasSuffix(got.Source.Path, test.suffix) {
				t.Fatalf("refusal = %+v", got)
			}
		})
	}
}

func TestRootLoginAdmitsOnlyKeep(t *testing.T) {
	t.Parallel()
	const suffix, old = "infra/machines/mirror.yaml", "        privateKeyRef: mirror-access-key\n"
	compile := func(value string) []diagnostics.Diagnostic {
		sources := patchedSources(t, exampleDirectory(t, "lab-rhel-mirror-host"), suffix, old, old+"\n    rootLogin: "+value+"\n", nil)
		_, _, err := wireCompiler().Compile(context.Background(), sources)
		return diagnostics.Of(err)
	}
	if found := compile("keep"); len(found) != 0 {
		t.Fatalf("rootLogin: keep = %+v", found)
	}
	found := compile("revoke")
	if len(found) != 1 || found[0].Code != "api.value" || found[0].Field != "$.spec.access.rootLogin" || found[0].Message != "value is not one of the permitted choices: keep" ||
		found[0].Source == nil || !strings.HasSuffix(found[0].Source.Path, suffix) {
		t.Fatalf("rootLogin: revoke = %+v", found)
	}
}
