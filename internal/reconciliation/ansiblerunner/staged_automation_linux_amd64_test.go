//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// testLaunch is the controller runtime's launch as the guard hands it over.
var testLaunch = prerequisites.PythonLaunch{Program: "/runtime/bin/python", Shim: "/runtime/bootwright-python", Arguments: []string{"-I", "-B"}}

// dirStager stages an automation tree as the stager does: a private
// directory and an exclusive lock on it, released once. It writes no files,
// because the test adapters read none, and reports this executable's
// automation digest unless a test names another.
type dirStager struct {
	parent string
	digest string
	plain  bool

	mutex    sync.Mutex
	staged   []string
	released []string
}

func newDirStager(t *testing.T) *dirStager { return &dirStager{parent: t.TempDir()} }

func (s *dirStager) Stage(context.Context) (prerequisites.StagedAutomation, error) {
	tree, err := os.MkdirTemp(s.parent, "automation-")
	if err != nil {
		return prerequisites.StagedAutomation{}, err
	}
	lock, err := os.Open(tree)
	if err != nil {
		return prerequisites.StagedAutomation{}, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return prerequisites.StagedAutomation{}, err
	}
	digest := s.digest
	if digest == "" {
		digest = ansible.Digest()
	}
	s.mutex.Lock()
	s.staged = append(s.staged, tree)
	s.mutex.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			lock.Close()
			s.mutex.Lock()
			s.released = append(s.released, tree)
			s.mutex.Unlock()
		})
	}
	var held interface{ Fd() uintptr } = lock
	if s.plain {
		held = plainLock{lock.Fd()}
	}
	return prerequisites.StagedAutomation{Path: tree, Digest: digest, Lock: held, Release: release}, nil
}

func (s *dirStager) counts() (int, int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.staged), len(s.released)
}

// plainLock is a lock descriptor that is not an open file the runner can pass
// to its adapter.
type plainLock struct{ fd uintptr }

func (l plainLock) Fd() uintptr { return l.fd }

// completingAdapter speaks the protocol to completion and records the command
// the runner composed for it.
type completingAdapter struct {
	mutex    sync.Mutex
	commands []*exec.Cmd
}

func (a *completingAdapter) command(string, ...string) *exec.Cmd {
	command := exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
		`printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`)
	a.mutex.Lock()
	a.commands = append(a.commands, command)
	a.mutex.Unlock()
	return command
}

func stagedRunner(t *testing.T, stager prerequisites.AutomationStager, adapter *completingAdapter) Runner {
	return Runner{
		jobParent: t.TempDir(), scratchParent: privateTempDir(t), drain: 200 * time.Millisecond,
		playbooks: map[string]string{"artifact-server-nginx-v1/apply": "apply.yml"},
		command:   adapter.command, stager: stager,
	}
}

func stagedRequest() lifecycle.RunRequest {
	return lifecycle.RunRequest{
		Context: "lab", Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
		Canonical: []byte(`{}`), Placement: machineref.Placement{Connection: "local", Machine: "controller"},
		Launch: testLaunch,
	}
}

// A run executes the automation its stager extracted: the adapter starts in
// that tree, reads its configuration and collections there, and inherits the
// tree's lock, so no sweep removes the tree while it runs. A run outside an
// invocation releases its extraction when it ends.
func TestARunExecutesTheStagedAutomationAndHoldsItsLock(t *testing.T) {
	stager, adapter := newDirStager(t), &completingAdapter{}
	result, err := stagedRunner(t, stager, adapter).Run(context.Background(), stagedRequest())
	if err != nil || result.Outcome != "changed" {
		t.Fatalf("run = %+v (%v)", result, err)
	}
	staged, released := stager.counts()
	if staged != 1 || released != 1 || len(adapter.commands) != 1 {
		t.Fatalf("staged %d, released %d, started %d adapters", staged, released, len(adapter.commands))
	}
	tree, command := stager.staged[0], adapter.commands[0]
	if command.Dir != tree || !slices.Contains(command.Env, "ANSIBLE_CONFIG="+filepath.Join(tree, "ansible.cfg")) ||
		!slices.Contains(command.Env, "ANSIBLE_COLLECTIONS_PATH="+filepath.Join(tree, "collections")) {
		t.Fatalf("the adapter ran in %s with %q, want the staged tree %s", command.Dir, command.Env, tree)
	}
	if !slices.ContainsFunc(command.ExtraFiles, func(file *os.File) bool { return file != nil && file.Name() == tree }) {
		t.Fatal("the adapter did not inherit the staged tree's lock")
	}
}

// Every run of one invocation shares one extraction, released once the
// invocation ends rather than by each run.
func TestAnInvocationStagesItsAutomationOnce(t *testing.T) {
	stager, adapter := newDirStager(t), &completingAdapter{}
	runner := stagedRunner(t, stager, adapter)
	ctx, end := runner.BeginInvocation(context.Background())
	for range 2 {
		if _, err := runner.Run(ctx, stagedRequest()); err != nil {
			t.Fatal(err)
		}
	}
	if staged, released := stager.counts(); staged != 1 || released != 0 {
		t.Fatalf("two runs of one invocation staged %d trees and released %d", staged, released)
	}
	if adapter.commands[0].Dir != adapter.commands[1].Dir {
		t.Fatalf("the runs of one invocation ran from %s and %s", adapter.commands[0].Dir, adapter.commands[1].Dir)
	}
	end()
	if staged, released := stager.counts(); staged != 1 || released != 1 {
		t.Fatalf("the ended invocation staged %d trees and released %d", staged, released)
	}
}

// An extraction whose reported digest is not this executable's automation
// digest, or whose lock cannot be handed to an adapter, is refused before any
// job is claimed and is released at once.
func TestAStagedTreeThisExecutableCannotRunIsRefused(t *testing.T) {
	for name, stager := range map[string]*dirStager{
		"another digest":     {digest: strings.Repeat("0", 64)},
		"an unpassable lock": {plain: true},
	} {
		t.Run(name, func(t *testing.T) {
			stager.parent = t.TempDir()
			adapter := &completingAdapter{}
			runner := stagedRunner(t, stager, adapter)
			_, err := runner.Run(context.Background(), stagedRequest())
			if code, _ := codeOf(err); code != "controller.identity" {
				t.Fatalf("run = %q (%v)", code, err)
			}
			entries, _ := os.ReadDir(runner.jobParent)
			if staged, released := stager.counts(); staged != 1 || released != 1 || len(adapter.commands) != 0 || len(entries) != 0 {
				t.Fatalf("staged %d, released %d, started %d adapters, left %d jobs", staged, released, len(adapter.commands), len(entries))
			}
		})
	}
}

// A runner without a stager has no automation to run and refuses before it
// writes anything.
func TestARunnerWithoutAStagerRefuses(t *testing.T) {
	adapter := &completingAdapter{}
	runner := stagedRunner(t, nil, adapter)
	_, err := runner.Run(context.Background(), stagedRequest())
	if code, _ := codeOf(err); code != "lifecycle.state" || len(adapter.commands) != 0 {
		t.Fatalf("run = %q (%v), started %d adapters", code, err, len(adapter.commands))
	}
}
