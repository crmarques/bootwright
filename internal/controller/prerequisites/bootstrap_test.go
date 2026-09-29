package prerequisites

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestQualifiedAnsibleVersion(t *testing.T) {
	for _, version := range []string{"2.21.0", "2.21.4", "2.21.10"} {
		if err := ValidateQualifiedAnsibleVersion(version); err != nil {
			t.Fatalf("qualified release %s rejected: %v", version, err)
		}
	}
	for _, version := range []string{"2.20.9", "2.22.0", "3.0.0", "2.21.0rc1", "02.21.0", "2.21", "latest"} {
		found := diagnostics.Of(ValidateQualifiedAnsibleVersion(version))
		if len(found) != 1 || found[0].Code != "controller.unsupported" || !strings.Contains(found[0].Message, "ansible-core "+QualifiedAnsibleMinor+" ") ||
			strings.Contains(found[0].Message+found[0].Remediation, "Environment") {
			t.Fatalf("unqualified release %s lacks the qualified-minor diagnostic: %+v", version, found)
		}
	}
}

func TestQualifiedControllerPython(t *testing.T) {
	for _, version := range []string{"3.12.0", "3.13.15", "3.14.7"} {
		if !QualifiedControllerPython(version) {
			t.Fatalf("qualified controller Python %s rejected", version)
		}
	}
	for _, version := range []string{"3.11.9", "3.15.0", "3.14", "03.14.7"} {
		if QualifiedControllerPython(version) {
			t.Fatalf("unqualified controller Python %s accepted", version)
		}
	}
}

// ansibleRecordedAt re-canonicalizes a bootstrap at another ansible-core
// release, moving its wheel and that wheel's source with it, so a refusal can
// only come from a version rule.
func ansibleRecordedAt(value BootstrapDefinition, version string) (BootstrapDefinition, error) {
	value.Sources, value.Wheels = slices.Clone(value.Sources), slices.Clone(value.Wheels)
	value.AnsibleVersion = version
	for index, wheel := range value.Wheels {
		if wheel.Name == "ansible-core" {
			id := "ansible-" + version
			value.Wheels[index].Version, value.Wheels[index].SourceID = version, id
			value.Sources[index+1].ID = id
			value.Sources[index+1].URL = "https://files.pythonhosted.org/packages/ansible_core-" + version + "-py3-none-any.whl"
		}
	}
	return CanonicalBootstrap(value)
}

// A record an earlier build wrote under the recorded floor stays readable on
// every path a controller-record read takes, although setup would no longer
// select its release; only a release below the floor refuses.
func TestRecordedAnsibleFloorKeepsEarlierRecordsReadable(t *testing.T) {
	_, fixture := dynamicFixture(t)
	for _, version := range []string{"2.19.0", "2.20.9"} {
		bootstrap, err := ansibleRecordedAt(fixture.bootstrap, version)
		if err != nil || bootstrap.AnsibleIntent != "latest" {
			t.Fatalf("a latest record at %s is unreadable: %+v", version, diagnostics.Of(err))
		}
		definition, err := NewResolvedDefinition(bootstrap, fixture.native)
		if err != nil {
			t.Fatalf("a latest record at %s cannot be bound: %+v", version, diagnostics.Of(err))
		}
		if err := ValidateResolvedDefinition(definition); err != nil {
			t.Fatalf("a retained definition at %s is unreadable: %+v", version, diagnostics.Of(err))
		}
	}
	_, err := ansibleRecordedAt(fixture.bootstrap, "2.18.99")
	found := diagnostics.Of(err)
	if len(found) != 1 || !strings.Contains(found[0].Message, MinimumRecordedAnsibleVersion) || strings.Contains(found[0].Message+found[0].Remediation, "Environment") {
		t.Fatalf("a record below the floor lacks the recorded-minimum diagnostic: %+v", found)
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
