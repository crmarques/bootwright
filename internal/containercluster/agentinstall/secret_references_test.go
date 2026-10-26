package agentinstall

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// The media and install blocks report the declarations their requests name,
// and a request this build cannot read is refused rather than reported empty.
func TestAgentBlocksReportTheSecretsTheirRequestsReference(t *testing.T) {
	media, install, _ := onlyRequests(t, singleNodeCatalog())
	mediaData, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	installData, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := func(data []byte) reconciliation.Block {
		return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: data}}
	}
	for name, tc := range map[string]struct {
		references func(reconciliation.Block) ([]string, error)
		block      reconciliation.Block
		want       []string
	}{
		"media":   {NewMedia(nil, nil).SecretReferences, block(mediaData), []string{"artifact-server-tls", "openshift-pull-secret", "sno-cluster-admin-ssh-key"}},
		"install": {NewInstall(nil).SecretReferences, block(installData), []string{"artifact-server-tls", "lab-bmc-credentials", "lab-bmc-tls"}},
		"unread":  {NewInstall(nil).SecretReferences, block([]byte(`{"version":"other"}`)), nil},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := tc.references(tc.block)
			if (err != nil) != (name == "unread") || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
