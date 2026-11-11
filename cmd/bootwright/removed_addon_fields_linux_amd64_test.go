//go:build linux && amd64

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestRemovedAddOnInputFieldsRefuseAsUnknownFields(t *testing.T) {
	for name, test := range map[string]struct {
		suffix, old, replacement, field string
	}{
		"add-on inputs": {"add-ons/widgets.yaml", "spec:\n  olm:\n",
			"spec:\n  inputs:\n    - name: credential\n      secretType: token\n      effects:\n        - globalPullSecretMerge:\n            registry: registry.example.test\n            username: reader\n\n  olm:\n", "$.spec.inputs"},
		"binding add-on configurations": {"add-ons/binding.yaml", "  addonRefs:\n    - widgets\n",
			"  addonRefs:\n    - widgets\n\n  addonConfigs:\n    - addonRef: widgets\n      inputs:\n        - name: credential\n          value: pull-credential\n", "$.spec.addonConfigs"},
		"step input target": {"add-ons/widgets.yaml", "spec:\n  olm:\n",
			"spec:\n  steps:\n    - name: configure\n      gates: apply\n      playbook: playbooks/configure.yaml\n      target:\n        fromInput:\n          input: credential\n\n  olm:\n", "$.spec.steps[0].target.fromInput"},
	} {
		t.Run(name, func(t *testing.T) {
			sources := patchedSources(t, exampleDirectory(t, "lab-sno-authored"), test.suffix, test.old, test.replacement, nil)
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
