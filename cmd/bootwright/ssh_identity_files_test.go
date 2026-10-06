//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// sessionNode is an OS-ready Machine reached over SSH under the operator's
// own identity, which is the arm --ssh-id-file most often serves.
const sessionNode = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata: {name: node}
spec:
  os: {provided: true}
  network:
    addresses: [{name: ssh, address: 192.0.2.20}]
`

// sessionFileServices binds the complete graph with files as the invoking
// account's opener and the test's own account as the one an offered key must
// belong to.
func sessionFileServices(t *testing.T, files *virtualOperatorFiles) cli.Services {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Files = files
	deps.Owner = func() (int, error) { return os.Getuid(), nil }
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository,
		Inputs:    contexts.Inputs{Repository: repository, Selection: deps.Selection},
		Host:      testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer,
		Capabilities: testLifecycleCapabilities{},
	}
	return assembleServices(deps)
}

// The session the shipped graph builds resolves an offered key only through
// the invoking account's opener: the path exists nowhere on disk, so a session
// that opened it itself would refuse it as absent.
func TestAnOfferedKeyResolvesOnlyThroughTheBoundOpener(t *testing.T) {
	files := newVirtualOperatorFiles(t)
	environment := files.write(t, "env/environment.yaml", syntheticEnvironment)
	files.write(t, "env/controller.yaml", serviceHost)
	files.write(t, "env/node.yaml", sessionNode)
	key := files.write(t, "keys/id_ed25519", "OFFERED KEY\n")
	if _, err := os.Lstat(key); !os.IsNotExist(err) {
		t.Fatalf("the offered key exists on this host, so the test proves nothing: %v", err)
	}
	services := sessionFileServices(t, files)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", filepath.Dir(environment))
	files.take()
	_, err := services.MachineAccess.Exec(context.Background(), machineaccess.ExecRequest{
		ContextName: "alpha", Name: "node", SSH: machine.SSHOptions{IdentityFile: key}, Command: []string{"true"},
	})
	if asked := files.take(); !slices.Equal(asked, []string{key}) {
		t.Fatalf("the session asked the opener for %v, want %s (%#v)", asked, key, diagnostics.Of(err))
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "trust.identity" ||
		reported[0].Remediation != "record it with bootwright machine trust --context alpha --machines node" {
		t.Fatalf("after resolving the key the session reported %#v (%v), want the unproved host key", reported, err)
	}
}

// Production hands sessions the invoking account's opener, and no opener at
// all when none is bound, so an offered key refuses rather than being opened
// with this process's own credentials.
func TestProductionBindsTheOpenerForSessions(t *testing.T) {
	deps, release := localServiceDependencies(processDependencies{})
	defer release()
	if files := sessionFiles(deps.Files); files == nil {
		t.Fatal("production hands sessions no opener")
	}
	if files := sessionFiles(nil); files != nil {
		t.Fatalf("sessions received %#v for no opener", files)
	}
}
