package agentinstall

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type unresolvedCase struct {
	verb     reconciliation.Verb
	evidence []byte
	want     lifecycle.Unresolved
	explains bool
}

func requireUnresolved(t *testing.T, reporter lifecycle.UnresolvedReporter, block reconciliation.Block, cases map[string]unresolvedCase) {
	t.Helper()
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			got, explains := reporter.Unresolved(test.verb, block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
		})
	}
}

// Evidence this request's own that none of the verb's checks accept is named
// by the check that decided. Media its build claims complete, built by another
// release's installer, is refused by the apply's presence check and by the
// removal's partial check alike; an installation claiming completion at
// another release is refused by the apply's presence check, and media on a
// Machine that is not one of the cluster's nodes by the removal's partial
// release. Evidence for another request and no evidence at all are left to
// the general reason.
func TestUnresolvedNamesTheRefusingCheck(t *testing.T) {
	t.Run("media", func(t *testing.T) {
		execution, _ := mediaExecution(t, testDigest)
		block := execution.Block
		block.Object = "sno"
		subject := "the agent media of ContainerCluster sno on Machine controller"
		another := mediaEvidence(t, testDigest, func(e *MediaEvidence) { e.Installer = "4.21.14" })
		requireUnresolved(t, NewMedia(nil), block, map[string]unresolvedCase{
			"an apply over another release's build": {reconciliation.Apply, another, lifecycle.Unresolved{
				Reason: subject + " is not what its apply froze: the published image was built by another release's installer",
				Remedy: "restore " + subject + " to what its frozen request names",
			}, true},
			"a removal over another release's build": {reconciliation.Destroy, another, lifecycle.Unresolved{
				Reason: subject + " is not what its destroy froze: the boot-media evidence proves a settled state, not a partial one",
				Remedy: "restore " + subject + " so its observation reads it as this context's own",
			}, true},
			"evidence for another request": {reconciliation.Apply, mediaEvidence(t, testDigest[:63]+"0", func(e *MediaEvidence) { e.Installer = "4.21.14" }), lifecycle.Unresolved{}, false},
			"no evidence":                  {reconciliation.Apply, nil, lifecycle.Unresolved{}, false},
		})
	})
	t.Run("install", func(t *testing.T) {
		execution, _ := installExecution(t, singleNodeCatalog(), testDigest)
		block := execution.Block
		block.Object = "sno"
		subject := "the installation of ContainerCluster sno on Machine controller"
		another := installEvidence(t, testDigest, func(e *InstallEvidence) { e.Release = "4.21.14" })
		stray := installEvidence(t, testDigest, func(e *InstallEvidence) {
			e.Postcondition, e.Media, e.OwnMedia = false, []string{"sno-01", "sno-02"}, []string{"sno-01"}
		})
		requireUnresolved(t, NewInstall(nil), block, map[string]unresolvedCase{
			"an apply over another release": {reconciliation.Apply, another, lifecycle.Unresolved{
				Reason: subject + " is not what its apply froze: the cluster reports another release than the one it was installed for",
				Remedy: "restore " + subject + " to what its frozen request names",
			}, true},
			"a removal over media on another Machine": {reconciliation.Destroy, stray, lifecycle.Unresolved{
				Reason: subject + " is not what its destroy froze: a Machine presenting boot media is not one of this cluster's nodes",
				Remedy: "restore " + subject + " so its observation reads it as this context's own",
			}, true},
			"a removal over another release": {reconciliation.Destroy, another, lifecycle.Unresolved{}, false},
			"evidence for another request":   {reconciliation.Apply, installEvidence(t, testDigest[:63]+"0", func(e *InstallEvidence) { e.Release = "4.21.14" }), lifecycle.Unresolved{}, false},
			"no evidence":                    {reconciliation.Apply, json.RawMessage(nil), lifecycle.Unresolved{}, false},
		})
	})
}
