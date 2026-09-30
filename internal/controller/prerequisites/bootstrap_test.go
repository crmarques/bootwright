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

// Setup records the Index API page it selected from, for latest and an exact
// intent alike; a record an earlier build wrote from the project JSON stays
// readable on every path a controller-record read takes, and that JSON names
// an exact intent's own release.
func TestAnsibleMetadataNamesTheEndpointItsIntentReads(t *testing.T) {
	_, fixture := dynamicFixture(t)
	for _, test := range []struct {
		intent, url string
		pass        bool
	}{
		{"latest", "https://pypi.org/simple/ansible-core/", true},
		{"latest", "https://pypi.org/pypi/ansible-core/json", true},
		{"latest", "https://pypi.org/simple/ansible/", false},
		{"latest", "https://pypi.org/simple/ansible-core", false},
		{"latest", "https://pypi.org/pypi/ansible-core/2.21.4/json", false},
		{"2.21.4", "https://pypi.org/simple/ansible-core/", true},
		{"2.21.4", "https://pypi.org/pypi/ansible-core/2.21.4/json", true},
		{"2.21.4", "https://pypi.org/pypi/ansible-core/2.21.3/json", false},
		{"2.21.4", "https://pypi.org/simple/ansible/", false},
		{"2.21.4", "https://pypi.org/pypi/ansible-core/json", false},
	} {
		value := fixture.bootstrap
		value.Metadata = slices.Clone(value.Metadata)
		value.AnsibleIntent, value.Metadata[1].URL = test.intent, test.url
		bootstrap, err := CanonicalBootstrap(value)
		if (err == nil) != test.pass {
			t.Fatalf("%s intent naming %s: accepted %t, want %t: %+v", test.intent, test.url, err == nil, test.pass, diagnostics.Of(err))
		}
		if !test.pass || test.intent != "latest" {
			continue
		}
		definition, err := NewResolvedDefinition(bootstrap, fixture.native)
		if err == nil {
			err = ValidateResolvedDefinition(definition)
		}
		if err != nil {
			t.Fatalf("a retained definition naming %s is unreadable: %+v", test.url, diagnostics.Of(err))
		}
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

// The closure identity follows what a resolution executes and nothing else.
// Carrying it onto other automation, which moves the projection it is
// published in, reading it from other index documents or reaching it from an
// exact intent keeps it, although each moves the resolution's own digest. A
// release, a source's bytes, the execution foundation or the platform moves
// it.
func TestClosureDigestFollowsOnlyTheExecutedClosure(t *testing.T) {
	_, fixture := dynamicFixture(t)
	closure := func(value BootstrapDefinition) string {
		t.Helper()
		digest, err := ClosureDigest(value)
		if err != nil || !bootstrapHash(digest) {
			t.Fatalf("closure digest = %q (%v)", digest, err)
		}
		return digest
	}
	base := closure(fixture.bootstrap)
	canonical := func(t *testing.T, value BootstrapDefinition) BootstrapDefinition {
		t.Helper()
		value, err := CanonicalBootstrap(value)
		if err != nil {
			t.Fatalf("the changed resolution is not canonical: %+v", diagnostics.Of(err))
		}
		return value
	}
	for name, change := range map[string]func(*BootstrapDefinition){
		"other automation": func(value *BootstrapDefinition) {
			value.AutomationDigest, value.ProjectionSHA256 = strings.Repeat("9", 64), strings.Repeat("8", 64)
			value.FileCount, value.ExpandedBytes = value.FileCount+1, value.ExpandedBytes+1
		},
		"other index documents": func(value *BootstrapDefinition) {
			value.Metadata = slices.Clone(value.Metadata)
			value.Metadata[0].SHA256, value.Metadata[1].SHA256 = strings.Repeat("7", 64), strings.Repeat("6", 64)
		},
		"exact intents": func(value *BootstrapDefinition) {
			value.PythonIntent, value.AnsibleIntent = value.PythonVersion, value.AnsibleVersion
		},
	} {
		t.Run("keeps under "+name, func(t *testing.T) {
			value := fixture.bootstrap
			change(&value)
			value = canonical(t, value)
			if value.Digest == fixture.bootstrap.Digest || closure(value) != base {
				t.Fatalf("the resolution digest moved %t and the closure moved %t", value.Digest != fixture.bootstrap.Digest, closure(value) != base)
			}
		})
	}
	for name, change := range map[string]func(*testing.T, BootstrapDefinition) BootstrapDefinition{
		"another ansible-core release": func(t *testing.T, value BootstrapDefinition) BootstrapDefinition {
			moved, err := ansibleRecordedAt(value, "2.21.5")
			if err != nil {
				t.Fatal(err)
			}
			return moved
		},
		"other source bytes": func(t *testing.T, value BootstrapDefinition) BootstrapDefinition {
			value.Sources = slices.Clone(value.Sources)
			value.Sources[2].SHA256 = strings.Repeat("f", 64)
			return canonical(t, value)
		},
		"another execution foundation": func(t *testing.T, value BootstrapDefinition) BootstrapDefinition {
			value.Execution.Files = []InstalledFile{{Path: "/usr/lib64/libc.so.6", SHA256: strings.Repeat("5", 64)}}
			return canonical(t, value)
		},
		"another platform release": func(t *testing.T, value BootstrapDefinition) BootstrapDefinition {
			value.Platform.Release += "1"
			return canonical(t, value)
		},
	} {
		t.Run("moves under "+name, func(t *testing.T) {
			if closure(change(t, fixture.bootstrap)) == base {
				t.Fatal("the closure identity did not move")
			}
		})
	}
}
