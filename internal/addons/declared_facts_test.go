package addons

import (
	"reflect"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func mustRegistry(t *testing.T, entries ...Package) Registry {
	t.Helper()
	r, err := NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryAcceptsAProviderOfADefinedTokenWithNoConsumer(t *testing.T) {
	for _, token := range api.DefinedCapabilityTokens() {
		provider := fixture("provider")
		provider.Provides = []string{token}
		if _, err := NewRegistry([]Package{provider}); err != nil {
			t.Fatalf("defined token %s: %v", token, err)
		}
	}
	provider := fixture("provider")
	provider.Provides = []string{"certManager"}
	if _, err := NewRegistry([]Package{provider}); err != nil {
		t.Fatal(err)
	}
	provider.Provides = []string{"certmanager"}
	if _, err := NewRegistry([]Package{provider}); err == nil {
		t.Fatal("a token the list does not define was admitted")
	}
}

func olmFixture(name string, approval Approval, reason string) Package {
	p := fixture(name)
	p.OLM = &OLM{Namespace: name + "-ns", OperatorGroup: OwnNamespace, GroupName: name, Subscription: name, Package: name, Approval: approval, OperatorManagedReason: reason}
	return p
}

func TestOperatorManagedApprovalNeedsAStatedReasonAndNoName(t *testing.T) {
	for name, test := range map[string]struct {
		approval Approval
		reason   string
		admitted bool
	}{
		"managed with a reason":    {OperatorManaged, "the operator approves its own later plans", true},
		"managed without a reason": {OperatorManaged, "", false},
		"managed with a control":   {OperatorManaged, "two\nlines", false},
		"managed with blank":       {OperatorManaged, " \t ", false},
		"exact without a reason":   {Exact, "", true},
		"exact carrying a reason":  {Exact, "the operator approves its own later plans", false},
		"unknown policy":           {Approval("other"), "reason", false},
		"empty policy":             {Approval(""), "reason", false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewRegistry([]Package{olmFixture("synthetic", test.approval, test.reason)})
			if (err == nil) != test.admitted {
				t.Fatalf("admitted = %t, want %t (%v)", err == nil, test.admitted, err)
			}
		})
	}
}

func TestLimitsHaveOneSourceEach(t *testing.T) {
	if MaxBudget != 5*time.Hour+50*time.Minute || budgetCeiling != "5h50m" {
		t.Fatal(MaxBudget, budgetCeiling)
	}
	p := fixture(strings.Repeat("a", MaxPackageNameBytes))
	if _, err := NewRegistry([]Package{p}); err != nil {
		t.Fatal(err)
	}
	p.Name += "a"
	if _, err := NewRegistry([]Package{p}); err == nil {
		t.Fatal("name over the limit admitted")
	}
	p = fixture("over")
	p.Budget = MaxBudget + time.Second
	if _, err := NewRegistry([]Package{p}); err == nil {
		t.Fatal("budget over the ceiling admitted")
	}
	b := packageBinding("binding", m("fixture", m("timeout", s("5h50m1s"))))
	issues := ValidateWithRegistry(b, api.NewCatalog([]api.Object{b}), mustRegistry(t, fixture("fixture")), ClusterFacts{Name: "cluster"})
	if len(issues) == 0 || !strings.Contains(issues[0].Message, "at most 5h50m") {
		t.Fatal(issues)
	}
	names := DefaultCatalogs()
	names[0] = "changed"
	if DefaultCatalogs()[0] == "changed" {
		t.Fatal("default catalogs are mutable")
	}
}

func consumerFixture(name, field string) Package {
	p := fixture(name)
	p.Consumes = func(v api.Value) []CephConsumerUse {
		if ref := v.Get(field); ref.Type() == api.String && ref.Text() != "" {
			return []CephConsumerUse{{Field: field, Name: ref.Text()}}
		}
		return nil
	}
	return p
}

func TestASecondConsumerPackageJoinsTheOneConsumerRuleAndAttachments(t *testing.T) {
	registry := mustRegistry(t, consumerFixture("first", "storageRef"), consumerFixture("second", "backendRef"))
	earlier := obj(api.ClusterAddonBinding, "a", m("clusterRef", s("one"), "packages", m("first", m("storageRef", s("export"), "timeout", s("1m")))))
	later := obj(api.ClusterAddonBinding, "b", m("clusterRef", s("two"), "packages", m("second", m("backendRef", s("export"), "timeout", s("1m")))))
	catalog := api.NewCatalog([]api.Object{later, earlier})
	if issues := ValidatePackages(earlier, catalog, registry, ClusterFacts{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	issues := ValidatePackages(later, catalog, registry, ClusterFacts{})
	if len(issues) != 1 || issues[0].Code != "api.duplicate" || issues[0].Field != "$.spec.packages.second.backendRef" || issues[0].Message != "CephConsumer/export is already consumed by package first of ClusterAddonBinding/a" {
		t.Fatal(issues)
	}
	want := []Attachment{{ClusterRef: "one", ConsumerRef: "export"}, {ClusterRef: "two", ConsumerRef: "export"}}
	if got := StorageAttachments(catalog, registry); !reflect.DeepEqual(got, want) {
		t.Fatalf("attachments = %v, want %v", got, want)
	}
	if got := StorageAttachments(catalog, Registry{}); len(got) != 0 {
		t.Fatalf("a registry without consumers attached %v", got)
	}
}

func TestTwoConsumerPackagesOfOneBindingMayNotNameOneConsumer(t *testing.T) {
	registry := mustRegistry(t, consumerFixture("first", "storageRef"), consumerFixture("second", "backendRef"))
	binding := obj(api.ClusterAddonBinding, "a", m("clusterRef", s("one"), "packages", m("first", m("storageRef", s("export"), "timeout", s("1m")), "second", m("backendRef", s("export"), "timeout", s("1m")))))
	issues := ValidatePackages(binding, api.NewCatalog([]api.Object{binding}), registry, ClusterFacts{})
	duplicates := 0
	for _, issue := range issues {
		if issue.Code == "api.duplicate" && issue.Field == "$.spec.packages.second.backendRef" && issue.Message == "CephConsumer/export is already consumed by package first of the same ClusterAddonBinding" && issue.Remediation != "" {
			duplicates++
		}
	}
	if duplicates != 1 || len(issues) != 1 {
		t.Fatal(issues)
	}
	distinct := obj(api.ClusterAddonBinding, "b", m("clusterRef", s("one"), "packages", m("first", m("storageRef", s("export"), "timeout", s("1m")), "second", m("backendRef", s("other"), "timeout", s("1m")))))
	if issues := ValidatePackages(distinct, api.NewCatalog([]api.Object{distinct}), registry, ClusterFacts{}); len(issues) != 0 {
		t.Fatal(issues)
	}
}
