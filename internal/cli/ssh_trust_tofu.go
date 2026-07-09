package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/sshtrust"
)

func offerTrustOnFirstUse(ctx context.Context, stdin io.Reader, stdout io.Writer, contextDir string, state v1alpha1.State, deps sshtrust.Deps, hostTrustScope map[string]bool) error {
	if deps.Scan == nil {
		deps.Scan = sshtrust.ScanHostKeys
	}
	if deps.LookPath == nil {
		deps.LookPath = preflight.DefaultLookPath
	}
	p := output.NewContinuation(stdout)
	return sshtrust.OfferTrustOnFirstUse(ctx, contextDir, state, controllerLocalityPolicy, deps, sshtrust.FirstUseInteraction{
		Begin:    func() { p.Section("SSH host trust (first use)") },
		Warn:     func(name, detail string) { p.Status(output.StatusWarn, name, detail) },
		Skip:     func(name, detail string) { p.Status(output.StatusSkip, name, detail) },
		Recorded: func(name, detail string) { p.Status(output.StatusOK, name, detail) },
		Confirm: func(record sshtrust.HostRecord) bool {
			return confirm(stdin, stdout, fmt.Sprintf("Trust %s %s for Machine/%s at %s? [y/N] (default: no): ", record.KeyType, record.FingerprintSHA256, record.Name, record.Address))
		},
	}, hostTrustScope)
}
