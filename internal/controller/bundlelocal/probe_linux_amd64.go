//go:build linux && amd64

package bundlelocal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func probeBundle(ctx context.Context, guard prerequisites.PythonExecutionGuard, area prerequisites.BundleArea, definition prerequisites.Definition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return guard.WithPython(ctx, area, definition.Execution, func(launch prerequisites.PythonLaunch, _ func() error) error {
		return runImportProbe(ctx, area, launch, definition)
	})
}

// This import-only check has no Ansible configuration discovery, inventory,
// connection, subprocess or cache creation. -S suppresses site hooks; the four
// private import roots are fixed explicitly before loading the resolved wheel
// closure, whose exact releases the resolution names.
const resolvedImportProbe = `import sys, os, json
assert sys.flags.isolated and sys.flags.dont_write_bytecode and sys.flags.no_site
expected = json.loads(sys.argv[1])
assert '.'.join(map(str, sys.version_info[:3])) == expected['pythonVersion']
root = os.path.dirname(os.path.dirname(sys.executable))
assert sys.prefix == root and sys.base_prefix == root
minor = str(sys.version_info.major) + '.' + str(sys.version_info.minor)
stdlib = root + '/lib/python' + minor
sys.path[:] = [root + '/lib/python' + minor.replace('.', '') + '.zip', stdlib, stdlib + '/lib-dynload', stdlib + '/site-packages']
import ssl, zlib, bz2, lzma, sqlite3, ctypes
import ansible.release, urllib3
from importlib.metadata import version
for wheel in expected['wheels']:
    assert version(wheel['name']) == wheel['version']
assert ansible.release.__version__ == expected['ansibleVersion']
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
cipher = AESGCM(bytes(32))
sealed = cipher.encrypt(bytes(12), b'bootwright-controller', None)
assert cipher.decrypt(bytes(12), sealed, None) == b'bootwright-controller'
print('bootwright-controller-ready-v1')
`

func runImportProbe(ctx context.Context, area prerequisites.BundleArea, launch prerequisites.PythonLaunch, definition prerequisites.Definition) error {
	if definition.Bootstrap == nil {
		return bundleFailure("the import check requires a resolved Python and Ansible closure")
	}
	data, err := json.Marshal(definition.Bootstrap)
	if err != nil {
		return bundleFailure("bootstrap import expectation cannot be encoded")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	arguments := append(slices.Clone(launch.Arguments), "-I", "-B", "-S", "-c", resolvedImportProbe, string(data))
	command := exec.CommandContext(bounded, launch.Loader, arguments...)
	command.Dir = launch.Directory
	command.Env = slices.Clone(launch.Environment)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 2 * time.Second
	var output, diagnostic limitedCapture
	command.Stdout, command.Stderr = &output, &diagnostic
	err = command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || output.exceeded || diagnostic.exceeded || diagnostic.buffer.Len() != 0 || !bytes.Equal(output.buffer.Bytes(), []byte("bootwright-controller-ready-v1\n")) {
		return bundleFailure("qualified isolated Python and Ansible import checks did not pass")
	}
	return area.Verify(ctx)
}

type limitedCapture struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (w *limitedCapture) Write(data []byte) (int, error) {
	if len(data) > 4096-w.buffer.Len() {
		w.exceeded = true
		return 0, errors.New("dependency probe output limit exceeded")
	}
	return w.buffer.Write(data)
}
