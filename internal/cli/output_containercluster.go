package cli

import (
	"io"
	"slices"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/secrets"
)

// writeKubeconfig writes the custodied bytes alone to out. A copy whose access
// was never proved is still exported, after one warning on errOut that says so.
func writeKubeconfig(out, errOut io.Writer, result *containeraccess.KubeconfigResult) error {
	value, present := result.Material.Part(secrets.ValuePart)
	if !present || len(value) == 0 || len(value) > secrets.MaxPartBytes || !slices.Equal(result.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		clear(value)
		return &resultFailure{"runtime.internal", "application service returned an unsupported result", 1}
	}
	defer clear(value)
	if err := writeHumanDiagnostics(errOut, displayDiagnostics(kubeconfigWarnings(result))); err != nil {
		return err
	}

	n, err := out.Write(value)
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	return err
}

func kubeconfigWarnings(result *containeraccess.KubeconfigResult) []diagnostic {
	if !result.Unproved {
		return nil
	}
	return []diagnostic{{Severity: "warning", Code: "access.unproved", Message: "the administrator kubeconfig of ContainerCluster " + result.Cluster +
		" was kept from an installation whose completion was never proved, so the access it grants was not proved"}}
}
