package libvirt

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Admission's bridge grammar refuses a template delimiter, so this graph
// bypasses it: the planned host and Machine requests still carry the bridge
// the templates write, and the plan refuses both before any block registers.
func TestAPlannedBridgeHoldingATemplateDelimiterRefusesBeforeRegistration(t *testing.T) {
	attachment := provider().Spec().Get("networkAttachments").Items()[0]
	attachment = attachment.With("libvirt", attachment.Get("libvirt").With("bridge", api.StringValue("{%raw%}")))
	p := provider(field("networkAttachments", api.ListValue(attachment)))
	catalog := catalogOf(controller(), p, networkConfig(), guest("rhel-01"))
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}}
	var definitions []reconciliation.BlockDefinition
	for _, capability := range []interface {
		Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
	}{NewHost(nil), NewMachine(nil)} {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, plan.Definitions...)
	}
	got := diagnostics.Of(lifecycle.RefuseTemplateDelimiters(testContext, definitions))
	want := map[string]string{
		"InfraProvider/lab-libvirt": `holds the template delimiter "{%" in networks[0].bridge`,
		"Machine/rhel-01":           `holds the template delimiter "{%" in interfaces[0].bridge`,
	}
	if len(got) != len(want) {
		t.Fatalf("refused %+v, want one refusal for each of %v", got, want)
	}
	for _, refused := range got {
		if refused.Object == nil || refused.Code != "api.value" {
			t.Fatalf("refusal %+v", refused)
		}
		suffix, found := want[refused.Object.Kind+"/"+refused.Object.Name]
		if !found || !strings.HasSuffix(refused.Message, suffix) {
			t.Fatalf("refusal %+v, want one ending %q", refused, suffix)
		}
		delete(want, refused.Object.Kind+"/"+refused.Object.Name)
	}
}
