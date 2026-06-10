package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/infra/proxy"
)

func writeShellExport(stdout io.Writer, key, value string) {
	fmt.Fprintf(stdout, "export %s=%s\n", key, shellQuoteValue(value))
}

func writeProxyShellExports(stdout io.Writer, env map[string]string) {
	for _, key := range proxy.EnvKeys {
		if value := env[key]; value != "" {
			writeShellExport(stdout, key, value)
		}
	}
}

func shellQuoteValue(value string) string {
	if value == "" {
		return "''"
	}
	if isShellSafeWord(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func isShellSafeWord(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("_@%+=:,./-", r):
		default:
			return false
		}
	}
	return true
}
