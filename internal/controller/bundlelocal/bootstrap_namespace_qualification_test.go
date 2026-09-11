//go:build linux && amd64 && controllerqualification

package bundlelocal

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

// The trusted Go probe establishes the kernel's actual UID mapping and chroot
// before the root-invoked resolver starts any downloaded Python process.
func TestQualifiedRootInvocationResolver(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_QUALIFY_ROOT_RESOLVER") != "1" {
		t.Skip("explicit root-invocation isolation qualification")
	}
	if os.Getuid() != 0 {
		t.Fatal("root-invocation qualification requires the trusted Go test process to be elevated")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	platform, err := hostlinux.New().Platform(ctx)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	native, ok := selectNative(catalog, platform)
	if !ok {
		t.Fatal("unsupported current platform")
	}
	// This first stage contains only the repository's trusted Go test binary.
	// No downloaded interpreter executes before the external mapping proof.
	projected := newProjection()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := projected.add("python/bin/python3.14", program, true); err != nil {
		t.Fatal(err)
	}
	requirement := cloneExecution(native.Execution)
	requirement.PythonExecutable = "python/bin/python3.14"
	root, cleanup, err := stageBootstrap(ctx, projected, prerequisites.BootstrapDefinition{PythonExecutable: requirement.PythonExecutable, Execution: requirement}, []byte("unused probe trust"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	command := exec.CommandContext(ctx, "/"+requirement.PythonExecutable, "-test.run", "^TestBootstrapNamespaceProbeChild$")
	command.Dir = "/"
	command.Env = []string{"BOOTWRIGHT_NAMESPACE_PROBE=1", "GOMAXPROCS=1", "HOME=/home", "TMPDIR=/tmp"}
	command.SysProcAttr = bootstrapProcessAttributes(root)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	if err := command.Start(); err != nil {
		t.Fatal("trusted Go namespace probe could not start", err)
	}
	defer func() {
		if command.Process != nil {
			command.Process.Kill()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "namespace-ready\n" {
		t.Fatalf("trusted namespace probe failed: %v %q", err, diagnostic.String())
	}
	mapBytes, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(command.Process.Pid), "uid_map"))
	if err != nil {
		t.Fatal(err)
	}
	if fields := strings.Fields(string(mapBytes)); len(fields) != 3 || fields[0] != "0" || fields[1] != "65534" || fields[2] != "1" {
		t.Fatal("resolver namespace does not map exclusively to the non-root host identity")
	}
	status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(command.Process.Pid), "status"))
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			verified = len(fields) == 5
			for _, id := range fields[1:] {
				verified = verified && id == "65534"
			}
		}
	}
	if !verified {
		t.Fatal("namespace probe retains a host-root user identity")
	}
	if _, err := io.WriteString(input, "verified\n"); err != nil {
		t.Fatal(err)
	}
	input.Close()
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	resolved, err := NewBootstrapResolver().Resolve(ctx, platform, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
	if err != nil {
		t.Fatalf("%v: %+v", err, desiredstate.DiagnosticsOf(err))
	}
	t.Logf("trusted root Go invocation verified non-root child mapping before resolving Python %s / Ansible %s; %d exact sources; no host package effects", resolved.PythonVersion, resolved.AnsibleVersion, len(resolved.Sources))
}

func TestBootstrapNamespaceProbeChild(t *testing.T) {
	if os.Getenv("BOOTWRIGHT_NAMESPACE_PROBE") != "1" {
		t.Skip("trusted namespace child only")
	}
	if os.Getuid() != 0 || os.Getgid() != 0 {
		os.Exit(21)
	}
	if _, err := os.Stat("/etc/os-release"); !os.IsNotExist(err) {
		os.Exit(22)
	}
	if err := os.WriteFile("/tmp/namespace-probe", []byte("private"), 0600); err != nil {
		os.Exit(23)
	}
	if _, err := io.WriteString(os.Stdout, "namespace-ready\n"); err != nil {
		os.Exit(24)
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || line != "verified\n" {
		os.Exit(25)
	}
	os.Exit(0)
}
