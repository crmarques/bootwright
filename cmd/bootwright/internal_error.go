package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"

	"github.com/crmarques/bootwright/internal/cli"
)

func safeInvocation(args []string, out, errOut io.Writer, invoke func() int) (code int) {
	defer func() {
		if recover() != nil {
			var identifier [16]byte
			reference := "unavailable"
			if _, err := rand.Read(identifier[:]); err == nil {
				reference = hex.EncodeToString(identifier[:])
			}
			reported := cli.InternalErrorDiagnostic(reference)
			code = cli.ClassifyInvocation(args).Diagnostic(out, errOut, reported, 1)
		}
	}()
	return invoke()
}
