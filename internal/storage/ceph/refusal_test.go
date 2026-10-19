package ceph

import (
	"context"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// refusingRunner answers as the lifecycle runner does when the adapter
// publishes a refused record: with the failure its caller gave that reason,
// and a reason its caller did not name is an invalid protocol.
type refusingRunner struct {
	reason  string
	request lifecycle.RunRequest
}

func (r *refusingRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.request = request
	if named := request.Refusals[r.reason]; named != nil {
		return lifecycle.RunResult{}, named
	}
	return lifecycle.RunResult{}, failure("lifecycle.unknown", "the adapter capability protocol was invalid", "")
}

func TestAHostRefusalReachesTheOperatorWithItsRemedy(t *testing.T) {
	e, _, proofs := hostExecution(t, "node-2")
	runner := &refusingRunner{reason: "image-pull"}
	result, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e)
	d := diagnostics.Of(err)
	if result.Outcome == reconciliation.OutcomeChanged || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the frozen Ceph image digest could not be pulled within its three bounded attempts" ||
		d[0].Remediation != "inspect the attempt's adapter output, then apply again; host preparation resumes from its journal" {
		t.Fatalf("an image-pull refusal reached the operator as %+v %+v", result, d)
	}
}

func TestAPackageRefusalReachesTheOperatorWithWhatToCheck(t *testing.T) {
	for reason, want := range map[string]string{"package-license": "Entitlement.spec.license.accept", "package-signature": "publisher keys", "package-resolution": "IBM Ceph repository", "package-transaction": "dnf log"} {
		t.Run(reason, func(t *testing.T) {
			e, _, proofs := hostExecution(t, "node-1")
			_, err := NewHost(&refusingRunner{reason: reason}).WithProofs(proofs).Apply(context.Background(), e)
			if d := diagnostics.Of(err); len(d) != 1 || d[0].Message == "the adapter capability protocol was invalid" || !strings.Contains(d[0].Remediation, want) {
				t.Fatalf("a %s refusal reached the operator as %+v", reason, d)
			}
		})
	}
}

// A named refusal ends the attempt with its entry's code: an unknown-coded one
// leaves the block's effect unknown, for the next apply to observe first, and
// an unsupported-coded one fails it.
func TestANamedHostRefusalsCodeDecidesTheAttemptOutcome(t *testing.T) {
	for reason, want := range map[string]reconciliation.Outcome{"image-pull": reconciliation.OutcomeUnknown, "package-license": reconciliation.OutcomeFailed} {
		t.Run(reason, func(t *testing.T) {
			e, _, proofs := hostExecution(t, "node-1")
			result, err := NewHost(&refusingRunner{reason: reason}).WithProofs(proofs).Apply(context.Background(), e)
			if err == nil || result.Outcome != want || lifecycle.AttemptOutcome(err) != want {
				t.Fatalf("a %s refusal ended the attempt %q (%v), want %q", reason, result.Outcome, diagnostics.Of(err), want)
			}
		})
	}
}

func TestAClusterRefusalReachesTheOperatorWithItsRemedy(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "dashboard-drift"}
	observation, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if observation.Effect != reconciliation.EffectUnknown || len(d) != 1 ||
		d[0].Message != "the Ceph dashboard user admin no longer matches the password this context's custody holds" ||
		!strings.Contains(d[0].Remediation, "bootwright cluster info --secrets --name <cluster>") {
		t.Fatalf("a dashboard-drift refusal reached the operator as %+v %+v", observation, d)
	}
}

func TestACallHomeUnqualifiedRefusalReachesTheOperatorAsUnsupportedWithItsRemedy(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "call-home-unqualified"}
	_, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != "lifecycle.unsupported" || d[0].Message != "IBM Call Home intent has no qualified path on the selected Ceph build" ||
		d[0].Remediation != "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, "+
			"correct ceph.ibm.callHome for the selected build with bootwright context update, then apply; see .agents/knowledge/version-specializations.md" {
		t.Fatalf("a call-home-unqualified refusal reached the operator as %+v", d)
	}
}

func TestANativeOptionUndeclaredRefusalNamesTheDestroyItsInputChangeNeeds(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "native-option-undeclared"}
	_, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != "lifecycle.unsupported" ||
		!strings.HasPrefix(d[0].Remediation, "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, ") ||
		!strings.Contains(d[0].Remediation, "with bootwright context update, then apply") {
		t.Fatalf("a native-option-undeclared refusal reached the operator as %+v", d)
	}
}

// The module publishes only a reason its block names, and a token the block
// does not name as unknown with the token in its detail, so each table names
// exactly what ceph_core.py may publish for its block, unknown among them.
func TestEachCephBlockNamesExactlyTheRefusalsItsModuleMayPublish(t *testing.T) {
	const path = "collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py"
	source := string(ansible.Assets()[path])
	token := regexp.MustCompile(`"([a-z0-9-]+)"`)
	for name, refusals := range map[string]map[string]error{"HOST_REASONS": hostRefusals(), "CLUSTER_REASONS": adapterRefusals()} {
		_, tuple, found := strings.Cut(source, "\n"+name+" = (\n")
		tuple, _, closed := strings.Cut(tuple, "\n)\n")
		if !found || !closed {
			t.Fatalf("%s declares no %s tuple", path, name)
		}
		published := []string{}
		for _, match := range token.FindAllStringSubmatch(tuple, -1) {
			published = append(published, match[1])
		}
		if slices.Sort(published); !slices.Equal(published, slices.Sorted(maps.Keys(refusals))) || !slices.Contains(published, "unknown") {
			t.Fatalf("%s publishes %q, its block names %q", name, published, slices.Sorted(maps.Keys(refusals)))
		}
		for reason, named := range refusals {
			if d := diagnostics.Of(named); len(d) != 1 || d[0].Remediation == "" {
				t.Fatalf("%s names %q with no remedy: %+v", name, reason, d)
			}
		}
	}
}
