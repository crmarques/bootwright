package cli

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
)

// offerTrustOnFirstUse is the interactive trust-on-first-use flow for preflight
// and apply: for every selected machine that requires managed SSH trust and has
// no recorded host key yet, scan the host, show the operator the key
// fingerprint, and record the key only after an explicit per-host yes. Machines
// with an existing record are never touched here — a changed key keeps failing
// closed with the `bootwright host trust --replace` ceremony, because a changed
// key is the man-in-the-middle signal that deserves a deliberate step. Callers
// gate this on interactive text runs; non-interactive runs keep failing closed
// without recording anything. A declined or unreachable host is left for the
// host check that follows, which fails with the `bootwright host trust`
// remediation.
func offerTrustOnFirstUse(ctx context.Context, stdin io.Reader, stdout io.Writer, contextDir string, state v1alpha1.State, deps hostTrustDeps) error {
	if deps.scan == nil {
		deps.scan = scanSSHHostKeys
	}
	if deps.lookPath == nil {
		deps.lookPath = defaultLookPath
	}
	machines := managedTrustMachines(state, controllerLocalityPolicy)
	if len(machines) == 0 {
		return nil
	}
	store, err := sshtrust.Load(sshtrust.StorePathForContext(contextDir))
	if err != nil {
		return err
	}
	var candidates []v1alpha1.Machine
	for _, machine := range machines {
		if _, ok := store.Find(machine.Metadata.Name); ok {
			continue
		}
		candidates = append(candidates, machine)
	}
	if len(candidates) == 0 {
		return nil
	}
	if _, err := deps.lookPath("ssh-keyscan", nil); err != nil {
		// The host check that follows reports the missing tool with its
		// remediation; first-use recording is simply not offered.
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Metadata.Name < candidates[j].Metadata.Name })
	p := output.NewContinuation(stdout)
	p.Section("SSH host trust (first use)")
	accepted := 0
	for _, machine := range candidates {
		report, record, write, err := evaluateHostTrust(ctx, machine, store, false, deps.scan)
		if err != nil {
			// Unreachable host or scan failure: leave it to the host check.
			p.Status(output.StatusWarn, "Machine/"+machine.Metadata.Name, err.Error())
			continue
		}
		if !write || report.Action != "add" {
			continue
		}
		if !confirm(stdin, stdout, fmt.Sprintf("Trust %s %s for Machine/%s at %s? [y/N] (default: no): ", record.KeyType, record.FingerprintSHA256, machine.Metadata.Name, record.Address)) {
			p.Status(output.StatusSkip, "Machine/"+machine.Metadata.Name, "not trusted; run bootwright host trust to record it later")
			continue
		}
		store.Upsert(record)
		accepted++
		p.Status(output.StatusOK, "Machine/"+machine.Metadata.Name, "recorded "+record.KeyType+" "+record.FingerprintSHA256)
	}
	if accepted == 0 {
		return nil
	}
	return sshtrust.Save(sshtrust.DirForContext(contextDir), store)
}
