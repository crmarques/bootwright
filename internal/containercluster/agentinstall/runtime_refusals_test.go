package agentinstall

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestRuntimeRefusalsNameTheExactNodeAndRecovery(t *testing.T) {
	_, request, _ := onlyRequests(t, compactCatalog())
	refused := runtimeRefusals("lab", "bootwright apply --context lab --authorize data-loss", request)
	for _, reason := range []string{"media-missing", "private-delivery", "controller-read", "media-insert", "media-eject", "node-unreachable"} {
		reported := diagnostics.Of(refused[nodeRefusal(reason, 2)])
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Object == nil || reported[0].Object.Name != request.Nodes[2].Machine || !strings.Contains(reported[0].Message, request.Nodes[2].Name) || !strings.Contains(reported[0].Message, request.Nodes[2].Address) || reported[0].Remediation == "" {
			t.Fatalf("%s: %+v", reason, reported)
		}
	}
}
