package ceph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestEarlierAuditedCephRequestsRefuse(t *testing.T) {
	core, err := coreRequest().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	consumer := managedExportPlan(t, false).Request
	for _, test := range []struct {
		name, previous string
		current        []byte
		decode         func([]byte) error
	}{
		{"core", "ceph-core-v41", core, func(data []byte) error { _, err := DecodeRequest(data); return err }},
		{"consumer", "ceph-export-request-v3", consumer, func(data []byte) error { _, err := DecodeConsumerRequest(data); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode(test.current); err != nil {
				t.Fatalf("current request refused: %v", err)
			}
			var members map[string]json.RawMessage
			if err := json.Unmarshal(test.current, &members); err != nil {
				t.Fatal(err)
			}
			members["version"], err = json.Marshal(test.previous)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := json.Marshal(members)
			if err != nil {
				t.Fatal(err)
			}
			issues := diagnostics.Of(test.decode(previous))
			if len(issues) != 1 || !strings.Contains(issues[0].Message, test.previous) || issues[0].Remediation == "" {
				t.Fatalf("earlier request did not refuse by version with a remedy: %+v", issues)
			}
		})
	}
}
