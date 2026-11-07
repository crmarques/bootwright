package ceph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestCephRequestsOfAnotherVersionRefuse(t *testing.T) {
	core, err := coreRequest().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	consumer := managedExportPlan(t, false).Request
	for _, test := range []struct {
		name, other string
		current     []byte
		decode      func([]byte) error
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
			members["version"], err = json.Marshal(test.other)
			if err != nil {
				t.Fatal(err)
			}
			other, err := json.Marshal(members)
			if err != nil {
				t.Fatal(err)
			}
			issues := diagnostics.Of(test.decode(other))
			if len(issues) != 1 || !strings.Contains(issues[0].Message, test.other) || issues[0].Remediation == "" {
				t.Fatalf("request of another version did not refuse by version with a remedy: %+v", issues)
			}
		})
	}
}
