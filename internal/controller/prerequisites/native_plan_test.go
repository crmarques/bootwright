package prerequisites

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
)

func nativePlanFixture() NativeResolvedPlan {
	plan := NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: Platform{OS: "fedora", Release: "43", Architecture: "amd64"},
		Solver: "dnf5", SolverVersion: "5.2.18", Requests: controller.DefaultDependencyVersions(),
		Repositories: []NativeRepository{{ID: "qualified", BaseURL: "https://publisher.example.test/os", MetadataSHA256: strings.Repeat("a", 64)}},
		BeforeSHA256: strings.Repeat("b", 64), AfterSHA256: strings.Repeat("b", 64), Actions: []NativeAction{},
	}
	for _, entry := range []struct{ key, name string }{{"nmstate", "nmstate"}, {"openssh", "openssh-clients"}} {
		identity := NativeIdentity{Name: entry.name, Version: "1.2.3", Release: "4.fc43", Architecture: "x86_64"}
		plan.Roots = append(plan.Roots, NativeRoot{Key: entry.key, Requested: "latest", Package: identity})
		plan.Packages = append(plan.Packages, NativePackage{Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture, Signer: strings.Repeat("c", 40), Source: DependencySource{ID: entry.name, URL: "https://publisher.example.test/os/" + entry.name + ".rpm", SHA256: strings.Repeat("d", 64), Bytes: 64}})
	}
	return plan
}

func TestNativePlanCanonicalizationFreezesIndependentExactEvidence(t *testing.T) {
	original := nativePlanFixture()
	canonical, err := CanonicalNativePlan(original)
	if err != nil || ValidateNativePlan(canonical) != nil {
		t.Fatal(err)
	}
	slices.Reverse(original.Roots)
	slices.Reverse(original.Packages)
	again, err := CanonicalNativePlan(original)
	if err != nil || !reflect.DeepEqual(canonical, again) {
		t.Fatal("input order changed frozen plan identity", err)
	}
	original.Packages[0].Source.SHA256 = strings.Repeat("e", 64)
	if ValidateNativePlan(canonical) != nil {
		t.Fatal("caller mutation reached canonical evidence")
	}
	canonical.Packages[0].Source.SHA256 = strings.Repeat("f", 64)
	if ValidateNativePlan(canonical) == nil {
		t.Fatal("modified source accepted under old plan digest")
	}
	digest, err := NativeTransitionsDigest([]NativeAction{})
	if err != nil || digest != "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatal("native transition encoding differs from compact Python JSON", digest, err)
	}
}

func TestNativePlanEnforcesVersionOverridesAndSelectedSources(t *testing.T) {
	cases := []struct {
		name   string
		change func(*NativeResolvedPlan)
		valid  bool
	}{
		{"version only chooses matching build", func(p *NativeResolvedPlan) { p.Requests.NMState = "1.2.3"; p.Roots[0].Requested = "1.2.3" }, true},
		{"exact build", func(p *NativeResolvedPlan) {
			p.Requests.NMState = "0:1.2.3-4.fc43"
			p.Roots[0].Requested = "0:1.2.3-4.fc43"
		}, true},
		{"ignored version", func(p *NativeResolvedPlan) { p.Requests.NMState = "1.2.4"; p.Roots[0].Requested = "1.2.4" }, false},
		{"ignored release", func(p *NativeResolvedPlan) {
			p.Requests.NMState = "1.2.3-5.fc43"
			p.Roots[0].Requested = "1.2.3-5.fc43"
		}, false},
		{"ignored epoch", func(p *NativeResolvedPlan) { p.Requests.NMState = "1:1.2.3"; p.Roots[0].Requested = "1:1.2.3" }, false},
		{"missing root artifact", func(p *NativeResolvedPlan) { p.Packages = p.Packages[1:] }, false},
		{"unselected artifact", func(p *NativeResolvedPlan) {
			extra := p.Packages[0]
			extra.Name = "unselected"
			extra.Source.ID = "unselected"
			p.Packages = append(p.Packages, extra)
		}, false},
		{"outside repository", func(p *NativeResolvedPlan) { p.Packages[0].Source.URL = "https://other.example.test/package.rpm" }, false},
		{"ambient URL credential", func(p *NativeResolvedPlan) { p.Repositories[0].BaseURL = "https://user@publisher.example.test/os" }, false},
		{"invalid unused version", func(p *NativeResolvedPlan) { p.Requests.Virtctl = "nightly" }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := nativePlanFixture()
			test.change(&plan)
			_, err := CanonicalNativePlan(plan)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t: %v", test.valid, err)
			}
		})
	}
}

func TestNativePlanPermitsOnlyExplicitRootDowngrade(t *testing.T) {
	plan := nativePlanFixture()
	previous := plan.Roots[0].Package
	previous.Version = "2.0.0"
	plan.Actions = []NativeAction{{Kind: "downgrade", Before: &previous, After: plan.Roots[0].Package, SourceID: plan.Packages[0].Source.ID, Reason: "root"}}
	plan.AfterSHA256 = strings.Repeat("e", 64)
	if _, err := CanonicalNativePlan(plan); err == nil {
		t.Fatal("latest request permitted downgrade")
	}
	plan.Requests.NMState, plan.Roots[0].Requested = "1.2.3", "1.2.3"
	if _, err := CanonicalNativePlan(plan); err != nil {
		t.Fatal(err)
	}
	plan.Actions[0].Reason = "dependency"
	if _, err := CanonicalNativePlan(plan); err == nil {
		t.Fatal("false transition reason accepted")
	}
	plan.Actions[0].Kind = "erase"
	if _, err := CanonicalNativePlan(plan); err == nil {
		t.Fatal("erase accepted")
	}
}

// A requirement may install more than one root package. The plan is proved
// against exactly the packages its selected requirements name, so a missing
// one, a surplus one and a package from an unselected requirement all refuse
// before any transaction exists.
func TestANativePlanIsProvedAgainstEveryPackageItsRequirementsName(t *testing.T) {
	closure := func() NativeResolvedPlan {
		plan := nativePlanFixture()
		plan.Requirements.InstallerMedia = true
		for _, name := range []string{"lorax", "xorriso"} {
			identity := NativeIdentity{Name: name, Version: "1.2.3", Release: "4.fc43", Architecture: "x86_64"}
			plan.Roots = append(plan.Roots, NativeRoot{Key: "installer-media", Requested: "latest", Package: identity})
			plan.Packages = append(plan.Packages, NativePackage{
				Name: identity.Name, Version: identity.Version, Release: identity.Release,
				Architecture: identity.Architecture, Signer: strings.Repeat("c", 40),
				Source: DependencySource{ID: name, URL: "https://publisher.example.test/os/" + name + ".rpm", SHA256: strings.Repeat("d", 64), Bytes: 64},
			})
		}
		return plan
	}
	canonical, err := CanonicalNativePlan(closure())
	if err != nil || ValidateNativePlan(canonical) != nil {
		t.Fatalf("a complete two-package requirement was refused: %v", err)
	}
	for name, change := range map[string]func(*NativeResolvedPlan){
		"one package missing": func(p *NativeResolvedPlan) {
			p.Roots = p.Roots[:len(p.Roots)-1]
		},
		"a package the requirement does not name": func(p *NativeResolvedPlan) {
			p.Roots[len(p.Roots)-1].Package.Name = "genisoimage"
		},
		"a requirement that was never selected": func(p *NativeResolvedPlan) {
			p.Requirements.InstallerMedia = false
		},
		"a package claimed under another requirement": func(p *NativeResolvedPlan) {
			p.Roots[len(p.Roots)-1].Key = "nmstate"
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan := closure()
			change(&plan)
			value, err := CanonicalNativePlan(plan)
			if err == nil && ValidateNativePlan(value) == nil {
				t.Fatal("an inconsistent root set was accepted")
			}
		})
	}
}

// The hypervisor runs the release its client speaks, so both are proved against
// the one libvirt intent rather than drifting apart.
func TestTheHypervisorClosureSharesTheLibvirtVersionIntent(t *testing.T) {
	plan := nativePlanFixture()
	plan.Requirements.Hypervisor = true
	plan.Requests.Libvirt = "11.5.0"
	for _, name := range []string{
		"libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
		"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm", "swtpm", "swtpm-tools",
	} {
		identity := NativeIdentity{Name: name, Version: "11.5.0", Release: "4.fc43", Architecture: "x86_64"}
		plan.Roots = append(plan.Roots, NativeRoot{Key: "hypervisor", Requested: "11.5.0", Package: identity})
		plan.Packages = append(plan.Packages, NativePackage{
			Name: identity.Name, Version: identity.Version, Release: identity.Release,
			Architecture: identity.Architecture, Signer: strings.Repeat("c", 40),
			Source: DependencySource{ID: name, URL: "https://publisher.example.test/os/" + name + ".rpm", SHA256: strings.Repeat("d", 64), Bytes: 64},
		})
	}
	canonical, err := CanonicalNativePlan(plan)
	if err != nil || ValidateNativePlan(canonical) != nil {
		t.Fatalf("the hypervisor closure was refused under its client's intent: %v", err)
	}
	// A closure resolved against a release the client never asked for is not
	// the closure this plan selected.
	drifted := canonical
	drifted.Requests.Libvirt = "11.6.0"
	if ValidateNativePlan(drifted) == nil {
		t.Fatal("a hypervisor resolved against another release was accepted")
	}
}
