package instance

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func placingPackage(name, placed string, declared ...string) addons.Package {
	return addons.Package{Name: name, Summary: name + " package", Budget: time.Minute, PlacementNamespaces: declared, OLM: &addons.OLM{Namespace: name + "-operator", OperatorGroup: addons.OwnNamespace, GroupName: name, Subscription: name, Package: name, DefaultSource: "catalog", Approval: addons.Exact}, Render: func(addons.Instance) (addons.Rendered, error) {
		secret := co.SecretPlacement{Secret: "credentials", Namespace: placed, Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "user", Part: "username"}, {Key: "password", Part: "password"}}}
		return addons.Rendered{Before: []co.Phase{{Secrets: []co.SecretPlacement{secret}}}}, nil
	}}
}
func placingRequest(t *testing.T, pkg addons.Package) (Request, error) {
	t.Helper()
	request, _, err := Render(addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: "cluster", Binding: "binding", Config: fixtureConfig()}}, addons.ClusterFacts{Name: "cluster", Release: "4.21.1"}, "controller")
	return request, err
}

func TestEachPackageFreezesItsOwnPlacementNamespaces(t *testing.T) {
	for _, test := range []struct {
		pkg  addons.Package
		want []string
	}{
		{placingPackage("alpha", "alpha-secrets", "alpha-secrets"), []string{"alpha-secrets"}},
		{placingPackage("beta", "beta-a", "beta-b", "beta-a", "beta-b"), []string{"beta-a", "beta-b"}},
	} {
		t.Run(test.pkg.Name, func(t *testing.T) {
			request, err := placingRequest(t, test.pkg)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(request.PlacementNamespaces, test.want) {
				t.Fatalf("frozen namespaces %v, want %v", request.PlacementNamespaces, test.want)
			}
			data, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			thawed, err := DecodeRequest(data)
			if err != nil {
				t.Fatal(err)
			}
			program, err := thawed.Objects()
			if err != nil {
				t.Fatal(err)
			}
			placements := 0
			for _, step := range program.Steps {
				if step.Place != nil {
					placements++
				}
			}
			if placements != 1 {
				t.Fatalf("thawed %d placements", placements)
			}
		})
	}
}

func TestPlacementIntoAnUndeclaredNamespaceRefuses(t *testing.T) {
	if _, err := placingRequest(t, placingPackage("alpha", "elsewhere", "alpha-secrets")); err == nil || !strings.Contains(err.Error(), "invalid source or namespace") {
		t.Fatalf("a placement outside the declared namespaces rendered: %v", err)
	}
	if _, err := placingRequest(t, placingPackage("alpha", "alpha-secrets")); err == nil {
		t.Fatal("a placement rendered with no declared namespace")
	}
	request, err := placingRequest(t, placingPackage("alpha", "alpha-secrets", "alpha-secrets"))
	if err != nil {
		t.Fatal(err)
	}
	request.PlacementNamespaces = []string{}
	if _, err := request.Canonical(); err == nil {
		t.Fatal("a request without its placement namespace froze")
	}
}

func TestEditedPlacementNamespacesRefuseAtDecode(t *testing.T) {
	request, err := placingRequest(t, placingPackage("beta", "beta-a", "beta-a", "beta-b"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	frozen := `"placementNamespaces":["beta-a","beta-b"]`
	if !strings.Contains(string(data), frozen) {
		t.Fatalf("the frozen request does not carry %s: %s", frozen, data)
	}
	for name, edit := range map[string]string{
		"unsorted":  `"placementNamespaces":["beta-b","beta-a"]`,
		"duplicate": `"placementNamespaces":["beta-a","beta-a","beta-b"]`,
		"invalid":   `"placementNamespaces":["Beta A","beta-a","beta-b"]`,
		"null":      `"placementNamespaces":null`,
		"missing":   `"placementNamespaces":["beta-b"]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRequest([]byte(strings.Replace(string(data), frozen, edit, 1))); err == nil {
				t.Fatalf("decoded the edit %s", edit)
			}
		})
	}
	widened := strings.Replace(string(data), frozen, `"placementNamespaces":["beta-a","beta-b","beta-c"]`, 1)
	if _, err := DecodeRequest([]byte(widened)); err != nil {
		t.Fatalf("a valid widening is for planning to refuse: %v", err)
	}
}

func TestValidateNamespacesRefusesWhatDiffersFromTheRegistry(t *testing.T) {
	registry, err := addons.NewRegistry([]addons.Package{placingPackage("beta", "beta-a", "beta-a", "beta-b")})
	if err != nil {
		t.Fatal(err)
	}
	request, err := placingRequest(t, placingPackage("beta", "beta-a", "beta-a", "beta-b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNamespaces(request, registry); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Request){
		"widened":  func(r *Request) { r.PlacementNamespaces = []string{"beta-a", "beta-b", "beta-c"} },
		"narrowed": func(r *Request) { r.PlacementNamespaces = []string{"beta-a"} },
		"other":    func(r *Request) { r.Package = "gamma" },
	} {
		t.Run(name, func(t *testing.T) {
			edited := request
			change(&edited)
			err := validateNamespaces(edited, registry)
			got := diagnostics.Of(err)
			if len(got) != 1 || got[0].Code != "lifecycle.state" || !strings.Contains(got[0].Message, edited.Package) || got[0].Remediation == "" {
				t.Fatalf("namespaces that differ from the registry were accepted: %v", err)
			}
		})
	}
}

func TestApplyAndObserveRefuseAFrozenRequestWhoseNamespacesWereWidened(t *testing.T) {
	declared := placingPackage("beta", "beta-a", "beta-a")
	registry, err := addons.NewRegistry([]addons.Package{declared})
	if err != nil {
		t.Fatal(err)
	}
	request, err := placingRequest(t, declared)
	if err != nil {
		t.Fatal(err)
	}
	request.PlacementNamespaces = []string{"beta-a", "kube-system"}
	execution := fixtureExecution(t, request)
	if _, err := DecodeRequest(execution.Block.Request); err != nil {
		t.Fatalf("the widened request must decode so that the registry comparison is what refuses it: %v", err)
	}
	runner := &scriptedRunner{}
	capability := New(runner, registry, nil)
	_, applyErr := capability.Apply(context.Background(), execution)
	_, observeErr := capability.Observe(context.Background(), execution)
	for name, err := range map[string]error{"apply": applyErr, "observe": observeErr} {
		got := diagnostics.Of(err)
		if len(got) != 1 || got[0].Code != "lifecycle.state" || !strings.Contains(got[0].Message, "beta") || got[0].Remediation == "" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatalf("a refused request reached the adapter %d times", len(runner.calls))
	}
	if _, err := capability.Destroy(context.Background(), execution); err != nil {
		t.Fatalf("retiring a request frozen by another build must still work: %v", err)
	}
	if _, err := capability.Removal(context.Background(), execution.Block); err != nil {
		t.Fatal(err)
	}
}

func TestARequestOfTheEarlierVersionRefusesAtRetirement(t *testing.T) {
	data, err := fixtureRequest(t).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	earlier := strings.Replace(string(data), `"version":"`+requestVersion+`"`, `"version":"addon-instance-request-v2"`, 1)
	if earlier == string(data) {
		t.Fatal("the frozen request does not carry its version member")
	}
	block := fixtureBlock(t, fixtureRequest(t))
	block.Request = []byte(earlier)
	capability := New(&scriptedRunner{}, fixtureRegistry(t), nil)
	_, destroyErr := capability.Destroy(context.Background(), lifecycle.Execution{Block: block})
	_, removalErr := capability.Removal(context.Background(), block)
	for name, err := range map[string]error{"destroy": destroyErr, "removal": removalErr} {
		got := diagnostics.Of(err)
		if len(got) != 1 || got[0].Code != "lifecycle.state" || !strings.Contains(got[0].Message, "unsupported version: addon-instance-request-v2") || got[0].Remediation == "" {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
