//go:build !linux || !amd64

package venvlocal

import (
	"context"
	"crypto/x509"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var errUnsupported = errors.New("the controller runtime is built only on linux/amd64")

func (a *Adapter) Select(ctx context.Context, _ string) (prerequisites.PythonInterpreter, error) {
	return prerequisites.PythonInterpreter{}, errors.Join(ctx.Err(), errUnsupported)
}

func (a *Adapter) Inspect(ctx context.Context, _ prerequisites.PythonInterpreter) (prerequisites.Runtime, bool, error) {
	return prerequisites.Runtime{}, false, errors.Join(ctx.Err(), errUnsupported)
}

func (a *Adapter) Build(ctx context.Context, _ prerequisites.PythonInterpreter) (prerequisites.Runtime, error) {
	return prerequisites.Runtime{}, errors.Join(ctx.Err(), errUnsupported)
}

func (a *Adapter) WithPython(ctx context.Context, _ prerequisites.Runtime, _ func(prerequisites.PythonLaunch) error) error {
	return errors.Join(ctx.Err(), errUnsupported)
}

func systemRoots(ctx context.Context) (*x509.CertPool, error) {
	return nil, errors.Join(ctx.Err(), errUnsupported)
}

func runCommand(ctx context.Context, _ command) ([]byte, error) {
	return nil, errors.Join(ctx.Err(), errUnsupported)
}
