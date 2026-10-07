//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// snapshotConfirmer records what both streams held and what the store was
// when it was asked, and declines.
type snapshotConfirmer struct {
	t           *testing.T
	root        string
	out, errOut *bytes.Buffer
	stdout      string
	stderr      string
	fingerprint map[string]string
	asked       int
}

func (c *snapshotConfirmer) Confirm(_ context.Context, action, name string) error {
	c.asked++
	c.stdout, c.stderr, c.fingerprint = c.out.String(), c.errOut.String(), stateFingerprint(c.t, c.root)
	return diagnostics.NewFailure("context.state", "context confirmation was declined; nothing changed", "")
}

// An update's and a deletion's prompt follow the plan of what they change: an
// update's input directory and counts on standard output and its warnings on
// standard error, and a deletion's revisions, keyring and reservations, all
// before anything is written. Declined, they change nothing.
func TestUpdateAndDeletePresentWhatTheyChangeBeforeThePrompt(t *testing.T) {
	_, repository, input, root := contextFixture(t)
	addSecretInput(t, input, "environment.yaml", syntheticEnvironment+"  resources:\n    - controller.yaml\n")
	addSecretInput(t, input, "excluded.yaml", strings.Replace(serviceHost, "name: service-host", "name: excluded-host", 1))
	var out, errOut bytes.Buffer
	confirmer := &snapshotConfirmer{t: t, root: root, out: &out, errOut: &errOut}
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	deps.Streams.Out, deps.Streams.Err = &out, &errOut
	deps.Confirmer = confirmer
	services := assembleServices(deps)
	if code := runServices(t.Context(), []string{"context", "init", "--name", "alpha", "--input-dir", input}, &out, &errOut, services); code != 0 || confirmer.asked != 0 {
		t.Fatalf("init = %d, asked %d times\n%s%s", code, confirmer.asked, out.String(), errOut.String())
	}
	addSecretInput(t, input, "environment.yaml", strings.ReplaceAll(syntheticEnvironment, "example.test", "changed.test")+"  resources:\n    - controller.yaml\n")
	out.Reset()
	errOut.Reset()
	before := stateFingerprint(t, root)
	if code := runServices(t.Context(), []string{"context", "update", "--name", "alpha", "--input-dir", input}, &out, &errOut, services); code != 1 || confirmer.asked != 1 {
		t.Fatalf("the declined update = %d, asked %d times\n%s%s", code, confirmer.asked, out.String(), errOut.String())
	}
	for _, line := range []string{"Context update plan\n", "  Name             alpha\n", "  Input directory  " + input + "\n", "  Files to copy    3\n", "  Files seen       3\n", "  Objects decoded  "} {
		if !strings.Contains(confirmer.stdout, line) {
			t.Fatalf("before the update prompt standard output lacks %q:\n%s", line, confirmer.stdout)
		}
	}
	if !strings.Contains(confirmer.stderr, "[WARN] api.selection "+filepath.Join(input, "excluded.yaml")) || !sameFingerprints(before, confirmer.fingerprint) || !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatalf("before the update prompt standard error held %q, or the store changed", confirmer.stderr)
	}
	out.Reset()
	errOut.Reset()
	if code := runServices(t.Context(), []string{"context", "delete", "--name", "alpha", "--purge"}, &out, &errOut, services); code != 1 || confirmer.asked != 2 {
		t.Fatalf("the declined deletion = %d, asked %d times\n%s%s", code, confirmer.asked, out.String(), errOut.String())
	}
	for _, line := range []string{"Context deletion plan\n", "  Input revisions  selected rev-", "  Keyring          removed with every secret version it holds\n", "  Reservations     none\n", "  Owned objects    none\n"} {
		if !strings.Contains(confirmer.stdout, line) {
			t.Fatalf("before the deletion prompt standard output lacks %q:\n%s", line, confirmer.stdout)
		}
	}
	if !sameFingerprints(before, confirmer.fingerprint) || !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("the declined deletion changed the store")
	}
	if _, err := os.Stat(filepath.Join(root, "contexts", "alpha")); err != nil {
		t.Fatalf("the declined deletion removed the context (%v)", err)
	}
}
