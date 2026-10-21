package instance

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type Evidence = driver.Evidence

func refusal(reason, remedy string) error {
	return diagnostics.NewFailureWithRemediation("lifecycle.state", reason, "", remedy)
}
func decodeEvidence(data []byte, digest string) (Evidence, error) {
	return driver.DecodeEvidence(data, digest, evidenceVersion)
}
func effectOf(data []byte, request Request, digest string) (reconciliation.EffectState, error) {
	program, err := request.Objects()
	if err != nil {
		return reconciliation.EffectUnknown, err
	}
	return driver.Effect(data, program, digest, evidenceVersion)
}
func retirement(digest string) (json.RawMessage, error) {
	return driver.Retirement(digest, evidenceVersion)
}
