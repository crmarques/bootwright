package cli

import (
	"io"
	"slices"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/secrets"
)

func writeKubeconfig(out io.Writer, result *containeraccess.KubeconfigResult) error {
	value, present := result.Material.Part(secrets.ValuePart)
	if !present || len(value) == 0 || len(value) > secrets.MaxPartBytes || !slices.Equal(result.Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		clear(value)
		return &resultFailure{"runtime.internal", "application service returned an unsupported result", 1}
	}
	defer clear(value)
	n, err := out.Write(value)
	if err == nil && n != len(value) {
		err = io.ErrShortWrite
	}
	return err
}
