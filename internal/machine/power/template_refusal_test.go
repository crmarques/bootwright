package power

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// templatedState renders the golden catalog with the named Machine's
// controller credential reference replaced by the given value. The address
// cannot hold a delimiter, because a controller URL admits no brace; a
// reference is read as text, so it is the field that carries one here.
type templatedState struct{ machine, reference string }

func (s templatedState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	objects := goldenCatalog(nil).Objects()
	for index, candidate := range objects {
		if candidate.Kind() == api.Machine && candidate.Name() == s.machine {
			objects[index] = candidate.WithSpec(candidate.Spec().WithPath(api.StringValue(s.reference), "hardware", "management", "bmc", "credentialsRef"))
		}
	}
	return &compilation.EffectiveResult{Effective: api.NewCatalog(objects)}, nil
}

// onlyRefusal is the one diagnostic a refusal carries.
func onlyRefusal(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 {
		t.Fatalf("err = %v, want exactly one diagnostic", err)
	}
	return reported[0]
}

// A power verb is a bounded run no plan ever saw, so it refuses a value that
// would be a template in Ansible before it consults a pin, asks for
// confirmation or has a runtime lent, and its remedy repeats the verb itself.
func TestAPowerVerbRefusesATemplateDelimiterBeforeAnyBinding(t *testing.T) {
	proved, runtime, runner, confirmer := &pins{}, &boundary{}, &adapter{machine: "metal", power: "off"}, &answer{}
	state := templatedState{machine: "metal", reference: "{{ lookup('env', 'HOME') }}"}
	_, err := New(state, evidenceSource{}, proved, runtime, runner, confirmer, nil, nil).
		Stop(context.Background(), PowerRequest{ContextName: "lab", Name: "metal"})
	refused := onlyRefusal(t, err)
	if refused.Code != "api.value" || refused.Object == nil || refused.Object.Kind != string(api.Machine) || refused.Object.Name != "metal" {
		t.Fatalf("refusal = %+v", refused)
	}
	if !strings.Contains(refused.Message, `holds the template delimiter "{{" in controller.credentialsRef`) {
		t.Fatalf("message = %q", refused.Message)
	}
	if !strings.HasSuffix(refused.Remediation, "then run bootwright machine stop --context lab --name metal") {
		t.Fatalf("remediation = %q", refused.Remediation)
	}
	if runtime.entered != 0 || len(proved.asked) != 0 || len(confirmer.asked) != 0 || runner.seen.Implementation != "" {
		t.Fatalf("a refused run still reached the runtime %d times, pins %v, confirmation %v", runtime.entered, proved.asked, confirmer.asked)
	}
}

// A reading refuses the same way, naming the Machine whose controller holds
// the delimiter, with a remedy that repeats the reading.
func TestAPowerReadRefusesATemplateDelimiterBeforeAnyBinding(t *testing.T) {
	runtime, runner := &boundary{}, &surveyor{}
	state := templatedState{machine: "rack", reference: "rack-{% raw %}"}
	_, err := New(state, evidenceSource{}, &pins{}, runtime, runner, nil, nil, nil).
		Read(context.Background(), "lab", []string{"metal", "rack"})
	refused := onlyRefusal(t, err)
	if refused.Code != "api.value" || refused.Object == nil || refused.Object.Name != "rack" {
		t.Fatalf("refusal = %+v", refused)
	}
	if !strings.Contains(refused.Message, `holds the template delimiter "{%" in controller.credentialsRef`) {
		t.Fatalf("message = %q", refused.Message)
	}
	if !strings.HasSuffix(refused.Remediation, "then run bootwright machine list --context lab --power-status") {
		t.Fatalf("remediation = %q", refused.Remediation)
	}
	if runtime.entered != 0 || len(runner.runs) != 0 {
		t.Fatalf("a refused reading still reached the runtime %d times and the adapter %d times", runtime.entered, len(runner.runs))
	}
}
