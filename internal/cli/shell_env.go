package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/infra/proxy"
)

func writeShellExport(stdout io.Writer, key, value string) {
	fmt.Fprintf(stdout, "export %s=%s\n", key, shellquote.QuoteWord(value))
}

func writeProxyShellExports(stdout io.Writer, env map[string]string) {
	for _, key := range proxy.EnvKeys {
		if value := env[key]; value != "" {
			writeShellExport(stdout, key, value)
		}
	}
}
