//go:build linux && amd64

package material

import (
	"context"
	"os"
)

// Test helpers over the production readers: each fixes the arguments every
// caller in this package's tests passes.

func resolveSecretPath(ctx context.Context, base, authored, code string) (string, error) {
	return resolveSecretPathFor(ctx, base, authored, code, "")
}

// newSecureFiles opens with this process's own credentials, as a Service bound
// to no Files does.
func newSecureFiles(ctx context.Context, code string) (*secureFiles, error) {
	return secureFilesThrough(ctx, code, processFiles{})
}

func readSecretFile(ctx context.Context, file *os.File) ([]byte, error) {
	return readSecretFileEncoded(ctx, file, transportExact)
}
