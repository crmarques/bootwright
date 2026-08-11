package converge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestHostSharedServiceManifestIsExactDeterministicAndHostScoped(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	manifest, err := BuildHostSharedServiceManifest("matrix", "apply", []InfraComponentServiceRef{
		{Kind: "infra-component", Name: "squid-proxy", Host: "services", SelectionDigests: []string{digestB}, ClaimDigests: []string{digestB}},
		{Kind: "bmc-emulator", Name: "libvirt", Host: "services", SelectionDigests: []string{digestA}},
		{Kind: "infra-component", Name: "squid-proxy", Host: "services", SelectionDigests: []string{digestA}, ClaimDigests: []string{digestA}},
		{Kind: "infra-component", Name: "dns", Host: "dns-host", SelectionDigests: []string{digestA}},
	})
	if err != nil {
		t.Fatalf("BuildHostSharedServiceManifest: %v", err)
	}
	if want := []string{"dns-host", "services"}; !reflect.DeepEqual(manifest.Hosts(), want) {
		t.Fatalf("Hosts = %#v, want %#v", manifest.Hosts(), want)
	}
	selection := manifest["services"]
	if selection.APIVersion != "bootwright.io/host-shared-service-selection/v1alpha1" || selection.Kind != "host-shared-service-selection" || selection.Context != "matrix" || selection.Command != "apply" || selection.Host != "services" {
		t.Fatalf("services selection identity = %+v", selection)
	}
	wantConsequences := []HostSharedServiceConsequence{
		{Kind: "bmc-emulator", Name: "libvirt", SelectionDigests: []string{digestA}},
		{Kind: "infra-component", Name: "squid-proxy", SelectionDigests: []string{digestA, digestB}, ClaimDigests: []string{digestA, digestB}},
	}
	if !reflect.DeepEqual(selection.Consequences, wantConsequences) {
		t.Fatalf("services consequences = %#v, want %#v", selection.Consequences, wantConsequences)
	}
	pair, err := manifest.ExtraVarPair()
	if err != nil {
		t.Fatalf("ExtraVarPair: %v", err)
	}
	var decoded map[string]HostSharedServiceManifest
	if err := json.Unmarshal([]byte(pair), &decoded); err != nil {
		t.Fatalf("decode ExtraVarPair %q: %v", pair, err)
	}
	if !reflect.DeepEqual(decoded[HostSharedServiceManifestExtraVar], manifest) {
		t.Fatalf("decoded manifest = %#v, want %#v", decoded[HostSharedServiceManifestExtraVar], manifest)
	}
}

func TestHostSharedServiceManifestFailsClosedOnAmbiguousIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		context string
		command string
		ref     InfraComponentServiceRef
		want    string
	}{
		{name: "context", command: "apply", ref: InfraComponentServiceRef{Kind: "infra", Name: "name", Host: "host"}, want: "context name"},
		{name: "command", context: "matrix", command: "plan", ref: InfraComponentServiceRef{Kind: "infra", Name: "name", Host: "host"}, want: "does not support"},
		{name: "kind", context: "matrix", command: "destroy", ref: InfraComponentServiceRef{Name: "name", Host: "host"}, want: "exact kind/name/host"},
		{name: "name", context: "matrix", command: "destroy", ref: InfraComponentServiceRef{Kind: "infra", Name: " name", Host: "host"}, want: "exact kind/name/host"},
		{name: "host", context: "matrix", command: "destroy", ref: InfraComponentServiceRef{Kind: "infra", Name: "name", Host: "host "}, want: "exact kind/name/host"},
		{name: "digest", context: "matrix", command: "destroy", ref: InfraComponentServiceRef{Kind: "infra", Name: "name", Host: "host"}, want: "selected-input digest"},
		{name: "malformed selection digest", context: "matrix", command: "destroy", ref: InfraComponentServiceRef{Kind: "infra", Name: "name", Host: "host", SelectionDigests: []string{"sha256:ABC"}}, want: "invalid selected-input digest"},
		{name: "malformed claim digest", context: "matrix", command: "destroy", ref: InfraComponentServiceRef{Kind: "infra", Name: "name", Host: "host", SelectionDigests: []string{"sha256:" + strings.Repeat("a", 64)}, ClaimDigests: []string{"sha256:ABC"}}, want: "invalid physical claim digest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildHostSharedServiceManifest(tc.context, tc.command, []InfraComponentServiceRef{tc.ref})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildHostSharedServiceManifest error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestEmptyHostSharedServiceManifestProjectsNoExecutionAuthority(t *testing.T) {
	manifest, err := BuildHostSharedServiceManifest("matrix", "apply", nil)
	if err != nil {
		t.Fatalf("BuildHostSharedServiceManifest: %v", err)
	}
	pair, err := manifest.ExtraVarPair()
	if err != nil {
		t.Fatalf("ExtraVarPair: %v", err)
	}
	if pair != "" {
		t.Fatalf("empty manifest pair = %q", pair)
	}
}
