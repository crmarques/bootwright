// Package venvlocal builds the controller's Python runtime: a venv on the OS
// interpreter holding exactly the embedded lock's wheels, and the guard every
// launch passes through.
package venvlocal

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

type Config struct {
	Lock     runtimelock.Lock
	Runtimes string
	Wheels   string
}

type command struct {
	Path        string
	Arguments   []string
	Environment []string
	Directory   string
}

type Adapter struct {
	lock     runtimelock.Lock
	runtimes string
	wheels   string
	bin      string
	top      string
	trusted  func(uint32) bool
	run      func(context.Context, command) ([]byte, error)
	client   func(context.Context, prerequisites.SetupEgress) (*http.Client, error)
}

var _ prerequisites.PythonRuntime = (*Adapter)(nil)

func New(config Config) *Adapter {
	return &Adapter{
		lock: config.Lock, runtimes: config.Runtimes, wheels: config.Wheels,
		bin: "/usr/bin", top: "/", trusted: func(uid uint32) bool { return uid == 0 },
		run: runCommand, client: wheelClient,
	}
}

const (
	shimName    = "bootwright-python"
	recordName  = "runtime.json"
	lockName    = ".lock"
	maxRecord   = 64 << 10
	maxOutput   = 1 << 20
	maxRedirect = 5
	fetchers    = 4
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type runtimeRecord struct {
	Format  string                `json:"format"`
	Runtime prerequisites.Runtime `json:"runtime"`
}

func (a *Adapter) runtimeDirectory(digest string) (string, error) {
	if !digestPattern.MatchString(digest) || a.runtimes == "" {
		return "", failure("the controller runtime digest is not 64 lowercase hex digits")
	}
	return filepath.Join(a.runtimes, digest), nil
}

func (a *Adapter) runtimeFor(interpreter prerequisites.PythonInterpreter) prerequisites.Runtime {
	return prerequisites.Runtime{
		Digest:     prerequisites.RuntimeDigest(a.lock.Digest(), interpreter.ABI, interpreter.Path),
		LockDigest: a.lock.Digest(), Interpreter: interpreter, AnsibleCore: a.lock.AnsibleCore(),
	}
}

func (a *Adapter) lockedFiles(abi string) ([]runtimelock.File, error) {
	files, found := a.lock.Files(abi)
	if !found {
		return nil, failure("the runtime lock carries no wheels for Python " + abi)
	}
	return files, nil
}

func failure(message string) error {
	return &prerequisites.ScopedFailure{Message: message, Correction: "Restore the host's Python interpreter and the locked controller wheels"}
}

// cleanEnvironment is what env -i leaves plus a fixed locale and search path.
func cleanEnvironment(home string) []string {
	environment := []string{"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "PATH=/usr/sbin:/usr/bin"}
	if home != "" {
		environment = append(environment, "HOME="+home)
	}
	return environment
}
