//go:build linux && amd64

package venvlocal

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

const commandTimeout = 10 * time.Minute

const venvProbe = `import importlib.metadata, json, sys
import ansible.release, ansible.cli.playbook, jinja2, yaml, urllib3, ssl, ctypes, zlib, bz2
names = json.loads(sys.argv[1])
json.dump({"python": "%d.%d.%d" % sys.version_info[:3], "venv": sys.prefix != sys.base_prefix,
 "ansible": ansible.release.__version__,
 "versions": dict((name, importlib.metadata.version(name)) for name in names)}, sys.stdout)
`

type venvReport struct {
	Python   string            `json:"python"`
	Venv     bool              `json:"venv"`
	Ansible  string            `json:"ansible"`
	Versions map[string]string `json:"versions"`
}

// Build stages, installs, compiles and verifies a venv, writes runtime.json and
// publishes it. A runtime already published is adopted only if it verifies and
// passes the import probe; otherwise it is retired and replaced, once.
func (a *Adapter) Build(ctx context.Context, publisher prerequisites.RuntimePublisher, interpreter prerequisites.PythonInterpreter) (prerequisites.Runtime, error) {
	qualified, err := a.qualify(ctx, interpreter.Path, interpreter.ABI)
	if err != nil {
		return prerequisites.Runtime{}, err
	}
	files, err := a.lockedFiles(qualified.ABI)
	if err != nil {
		return prerequisites.Runtime{}, err
	}
	for _, file := range append(files, a.lock.Installer()) {
		if err := a.cached(file); err != nil {
			return prerequisites.Runtime{}, err
		}
	}
	runtime := a.runtimeFor(qualified)
	final, err := a.runtimeDirectory(runtime.Digest)
	if err != nil {
		return prerequisites.Runtime{}, err
	}
	for replaced := false; ; replaced = true {
		published, err := a.publish(ctx, publisher, final, runtime, files)
		if err != nil {
			return prerequisites.Runtime{}, err
		}
		if published {
			return runtime, nil
		}
		broken := a.verifyRuntime(runtime)
		if broken == nil {
			broken = a.probeVenv(ctx, final, runtime, files)
		}
		if broken == nil {
			return runtime, nil
		}
		if replaced {
			return prerequisites.Runtime{}, broken
		}
		retired, err := publisher.RetireRuntime(ctx, runtime.Digest)
		if err != nil {
			return prerequisites.Runtime{}, err
		}
		if !retired {
			return prerequisites.Runtime{}, failure("the published controller runtime " + runtime.Digest[:12] + " is broken and cannot be replaced while a launch holds it")
		}
	}
}

func (a *Adapter) publish(ctx context.Context, publisher prerequisites.RuntimePublisher, final string, runtime prerequisites.Runtime, files []runtimelock.File) (bool, error) {
	stage, err := publisher.StageRuntime(ctx)
	if err != nil {
		return false, err
	}
	if err := a.populate(ctx, stage.Path(), final, runtime, files); err != nil {
		return false, errors.Join(err, publisher.DiscardRuntime(context.WithoutCancel(ctx), stage))
	}
	return publisher.PublishRuntime(ctx, stage, runtime.Digest)
}

func (a *Adapter) populate(ctx context.Context, stage, final string, runtime prerequisites.Runtime, files []runtimelock.File) error {
	interpreter := runtime.Interpreter
	if _, err := a.run(ctx, command{
		Path: interpreter.Path, Arguments: []string{"-I", "-m", "venv", "--without-pip", stage},
		Environment: cleanEnvironment(stage), Directory: stage,
	}); err != nil {
		return failure("python" + interpreter.ABI + " -m venv failed")
	}
	if err := a.install(ctx, stage, files); err != nil {
		return err
	}
	sitePackages := filepath.Join(stage, "lib", "python"+interpreter.ABI, "site-packages")
	if _, err := a.run(ctx, command{
		Path:        filepath.Join(stage, "bin", "python"),
		Arguments:   []string{"-I", "-m", "compileall", "-q", "-j0", "--invalidation-mode", "unchecked-hash", sitePackages},
		Environment: cleanEnvironment(stage), Directory: stage,
	}); err != nil {
		return failure("compiling the runtime's bytecode failed")
	}
	if err := verifyVenv(stage, sitePackages); err != nil {
		return err
	}
	if err := a.probeVenv(ctx, stage, runtime, files); err != nil {
		return err
	}
	return writeRuntimeFiles(stage, final, runtime)
}

// install runs pip from the locked installer wheel by zipimport, offline,
// against a link farm holding exactly the ABI's locked files.
func (a *Adapter) install(ctx context.Context, stage string, files []runtimelock.File) error {
	links := filepath.Join(stage, ".wheels")
	if err := os.Mkdir(links, 0o700); err != nil {
		return failure("the runtime stage cannot be written")
	}
	defer os.RemoveAll(links)
	var requirements strings.Builder
	for _, file := range files {
		if err := os.Symlink(a.cachePath(file), filepath.Join(links, file.Filename)); err != nil {
			return failure("the runtime stage cannot be written")
		}
		requirements.WriteString(file.Name + "==" + file.Version + " --hash=sha256:" + file.SHA256 + "\n")
	}
	lockFile := filepath.Join(stage, "requirements.lock")
	if err := os.WriteFile(lockFile, []byte(requirements.String()), 0o644); err != nil {
		return failure("the runtime stage cannot be written")
	}
	home := filepath.Join(stage, ".home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return failure("the runtime stage cannot be written")
	}
	defer os.RemoveAll(home)
	_, err := a.run(ctx, command{
		Path: filepath.Join(stage, "bin", "python"),
		Arguments: []string{
			"-I", filepath.Join(a.cachePath(a.lock.Installer()), "pip"), "install", "--isolated", "--no-index",
			"--find-links", links, "--no-deps", "--require-hashes", "--only-binary=:all:", "--no-compile",
			"--no-cache-dir", "--no-input", "--disable-pip-version-check", "--root-user-action=ignore", "-r", lockFile,
		},
		Environment: append(cleanEnvironment(home), "PIP_CONFIG_FILE=/dev/null"), Directory: stage,
	})
	if err != nil {
		return failure("pip could not install the locked wheels from the cache")
	}
	return nil
}

// verifyVenv refuses a startup hook anywhere in the venv, a venv that sees
// the system site-packages, and bytecode pip or compileall left checked.
func verifyVenv(stage, sitePackages string) error {
	config, err := os.ReadFile(filepath.Join(stage, "pyvenv.cfg"))
	if err != nil {
		return failure("the venv has no pyvenv.cfg")
	}
	isolated := 0
	for _, line := range strings.Split(string(config), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "include-system-site-packages" {
			if strings.TrimSpace(value) != "false" {
				return failure("the venv includes the system site-packages")
			}
			isolated++
		}
	}
	if isolated != 1 {
		return failure("the venv's pyvenv.cfg does not exclude the system site-packages once")
	}
	compiled := 0
	err = filepath.WalkDir(stage, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch name := entry.Name(); {
		case strings.HasSuffix(name, ".pth") || name == "sitecustomize.py" || name == "usercustomize.py":
			return failure("the venv holds the startup hook " + strings.TrimPrefix(path, stage))
		case strings.HasSuffix(name, ".pyc") && strings.HasPrefix(path, sitePackages+"/"):
			if err := uncheckedHash(path); err != nil {
				return err
			}
			compiled++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if compiled == 0 {
		return failure("the venv holds no compiled bytecode")
	}
	return nil
}

// uncheckedHash reads a pyc header: flags 0b01 is hash based and unchecked,
// so a launch with -B reads it without consulting a timestamp.
func uncheckedHash(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return failure("the venv's bytecode cannot be read")
	}
	defer file.Close()
	header := make([]byte, 8)
	if _, err := file.ReadAt(header, 0); err != nil || binary.LittleEndian.Uint32(header[4:]) != 1 {
		return failure("the venv's bytecode is not unchecked-hash: " + filepath.Base(path))
	}
	return nil
}

func (a *Adapter) probeVenv(ctx context.Context, directory string, runtime prerequisites.Runtime, files []runtimelock.File) error {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return failure("the import probe cannot be built")
	}
	output, err := a.run(ctx, command{
		Path: filepath.Join(directory, "bin", "python"), Arguments: []string{"-I", "-B", "-c", venvProbe, string(encoded)},
		Environment: cleanEnvironment(directory), Directory: directory,
	})
	if err != nil {
		return failure("the controller runtime failed its import probe")
	}
	var report venvReport
	if canonicaljson.DecodeClosed(output, &report) != nil || !report.Venv || !strings.HasPrefix(report.Python, runtime.Interpreter.ABI+".") || report.Ansible != runtime.AnsibleCore {
		return failure("the controller runtime's import probe did not report the locked interpreter and ansible-core")
	}
	for _, file := range files {
		if report.Versions[file.Name] != file.Version {
			return failure("the controller runtime reports " + file.Name + " " + report.Versions[file.Name] + ", not the locked " + file.Version)
		}
	}
	return nil
}

// writeRuntimeFiles writes the shim, the launch lock and, last, runtime.json,
// so a directory without runtime.json is never complete.
func writeRuntimeFiles(stage, final string, runtime prerequisites.Runtime) error {
	shim := "#!/bin/sh\nexec '" + strings.ReplaceAll(filepath.Join(final, "bin", "python"), "'", `'"'"'`) + "' -I -B \"$@\"\n"
	if os.WriteFile(filepath.Join(stage, shimName), []byte(shim), 0o755) != nil ||
		os.WriteFile(filepath.Join(stage, lockName), nil, 0o600) != nil {
		return failure("the runtime stage cannot be written")
	}
	record, err := canonicaljson.Encode(runtimeRecord{Format: prerequisites.RuntimeFormat, Runtime: runtime}, canonicaljson.Line)
	if err != nil {
		return failure("the runtime record cannot be encoded")
	}
	if os.WriteFile(filepath.Join(stage, recordName), record, 0o644) != nil {
		return failure("the runtime stage cannot be written")
	}
	return nil
}

func runCommand(ctx context.Context, request command) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	process := exec.CommandContext(ctx, request.Path, request.Arguments...)
	process.Env, process.Dir = request.Environment, request.Directory
	var stdout, stderr bounded
	process.Stdout, process.Stderr = &stdout, &stderr
	if err := process.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if stdout.overflow {
		return nil, errors.New("the command printed more than its output bound")
	}
	return stdout.Bytes(), nil
}

type bounded struct {
	bytes.Buffer
	overflow bool
}

func (b *bounded) Write(data []byte) (int, error) {
	written := len(data)
	if room := maxOutput - b.Len(); written > room {
		b.overflow = true
		data = data[:max(room, 0)]
	}
	b.Buffer.Write(data)
	return written, nil
}
