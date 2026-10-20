package cli

import (
	"io"
	"slices"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
)

func storageAccessCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "cluster keyring", short: "Export a StorageClient's raw sensitive Ceph keyring bytes to standard output",
			flags: []flagSpec{nameFlag(), requiredFlag("client", "Select the exact StorageClient name")},
			long:  "Export the raw sensitive Ceph keyring bytes of one StorageClient to standard output. Applicable to managed Ceph StorageClusters only."}),
	}
}

func keyringValue(result *inspection.KeyringResult) ([]byte, bool) {
	value, present := result.Material.Part(secrets.ValuePart)
	if !present || len(value) == 0 || len(value) > secrets.MaxPartBytes || !slices.Equal(result.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		clear(value)
		return nil, false
	}
	return value, true
}

func writeKeyring(out, errOut io.Writer, result *inspection.KeyringResult) error {
	value, ok := keyringValue(result)
	if !ok {
		return &resultFailure{"runtime.internal", "application service returned an unsupported result", 1}
	}
	defer clear(value)
	if err := writeHumanDiagnostics(errOut, displayDiagnostics(keyringWarnings(result))); err != nil {
		return err
	}
	n, err := out.Write(value)
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	return err
}

func keyringWarnings(result *inspection.KeyringResult) []diagnostic {
	if !result.Unproved {
		return nil
	}
	return []diagnostic{{Severity: "warning", Code: "access.unproved", Message: "the keyring of StorageClient " + result.Client + " of StorageCluster " + result.Cluster +
		" was kept from an apply that never proved it, so the access it grants was not proved"}}
}
