package prerequisites

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

func TestBootstrapAnsibleMinimum(t *testing.T) {
	for _, version := range []string{"2.19.0", "2.21.4", "3.0.0"} {
		if err := ValidateBootstrapAnsibleVersion(version); err != nil {
			t.Fatalf("compatible stable release %s rejected: %v", version, err)
		}
	}
	for _, version := range []string{"1.99.99", "2.18.99", "2.19.0rc1", "02.19.0", "latest"} {
		diagnostics := desiredstate.DiagnosticsOf(ValidateBootstrapAnsibleVersion(version))
		if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, MinimumBootstrapAnsibleVersion) {
			t.Fatalf("incompatible release %s lacks minimum-version diagnostic: %+v", version, diagnostics)
		}
	}
	_, fixture := dynamicFixture(t)
	value := fixture.bootstrap
	value.AnsibleVersion = "2.18.99"
	value.AnsibleIntent = "latest"
	_, err := CanonicalBootstrap(value)
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, MinimumBootstrapAnsibleVersion) {
		t.Fatalf("frozen bootstrap accepted unsupported Ansible: %+v", diagnostics)
	}
}

func TestCanonicalBootstrapDoesNotShareCallerSlices(t *testing.T) {
	_, fixture := dynamicFixture(t)
	input := fixture.bootstrap
	canonical, err := CanonicalBootstrap(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Sources[0].SHA256 = strings.Repeat("f", 64)
	input.Wheels[0].Version = "99.0.0"
	input.Metadata[0].URL = "https://example.test/substituted"
	if err := ValidateBootstrap(canonical); err != nil {
		t.Fatalf("caller mutation changed frozen bootstrap: %v", err)
	}
}

func TestBootstrapRejectsSelfConsistentSourceAndVersionSubstitution(t *testing.T) {
	for _, test := range []string{"source-traversal", "python-version", "wheel-version", "foreign-project", "metadata-authority", "null-collection"} {
		t.Run(test, func(t *testing.T) {
			_, fixture := dynamicFixture(t)
			value := fixture.bootstrap
			switch test {
			case "source-traversal":
				value.Sources[0].ID = ".."
			case "python-version":
				value.PythonVersion = "3.14.8"
			case "wheel-version":
				value.Wheels[0].Version = "99.0.0"
			case "foreign-project":
				value.Sources[1].URL = "https://files.pythonhosted.org/packages/foreign-2.21.4-py3-none-any.whl"
			case "metadata-authority":
				value.Metadata[1].URL = "https://pypi.org/pypi/foreign/json"
			case "null-collection":
				value.Execution.Links = nil
				if err := ValidateBootstrap(value); err == nil {
					t.Fatal("noncanonical null collection accepted")
				}
				return
			}
			if _, err := CanonicalBootstrap(value); err == nil {
				t.Fatal("substituted bootstrap could recompute an accepted identity")
			}
		})
	}
}
