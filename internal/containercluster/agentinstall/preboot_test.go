package agentinstall

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

// An apply names to its runner every refusal each node's pre-boot proof may
// make, under that node's position in the frozen order, the one its boot names
// it by, so a refused proof of the node booted third reports that node's
// Machine rather than an adapter failure or another node's. A removal and an
// observation boot nothing, so they name none.
func TestAnApplyRemediesEachNodesPreBootRefusalsUnderItsPosition(t *testing.T) {
	identities := &pinsByName{pins: map[string]machine.HardwareIdentity{}}
	for _, operation := range []string{"apply", "destroy", "observe"} {
		t.Run(operation, func(t *testing.T) {
			_, runner, err := mixedRun(t, operation, NewInstall(nil).WithIdentities(identities))
			if len(runner.requests) != 1 {
				t.Fatalf("run: %v", diagnostics.Of(err))
			}
			request, err := DecodeInstallRequest(runner.requests[0].Canonical)
			if err != nil {
				t.Fatalf("the run crossed with a request it cannot decode: %v", diagnostics.Of(err))
			}
			got := map[string][]diagnostics.Diagnostic{}
			for reason, err := range runner.requests[0].Refusals {
				got[reason] = diagnostics.Of(err)
			}
			want := map[string][]diagnostics.Diagnostic{}
			if operation == "apply" {
				for index, node := range request.Nodes {
					for reason, err := range substrate.PreBootRefusals(node.Substrate, mixedRunContext, node.Machine, node.Controller.Endpoint) {
						want[reason+"-node-"+strconv.Itoa(index)] = diagnostics.Of(err)
					}
				}
				arms := []string{request.Nodes[0].Substrate, request.Nodes[1].Substrate, request.Nodes[2].Substrate}
				if len(want) != 7 || !reflect.DeepEqual(arms, []string{substrate.ArmBaremetal, substrate.ArmLibvirt, substrate.ArmBaremetal}) {
					t.Fatalf("nodes on %v name %d refusals, want 7", arms, len(want))
				}
				if refused := got["machine-running-node-2"]; len(refused) != 1 || refused[0].Object.Name != "metal-03" ||
					!strings.Contains(refused[0].Remediation, "bootwright machine stop --context "+mixedRunContext+" --name metal-03") {
					t.Fatalf("the third node's running refusal reports %#v", refused)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("the run names %#v, want %#v", got, want)
			}
		})
	}
}
