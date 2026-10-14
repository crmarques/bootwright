//go:build linux && amd64

package venvlocal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func buildable(t *testing.T) (*fakeHost, *Adapter, prerequisites.PythonInterpreter) {
	t.Helper()
	host := newFakeHost(t)
	host.interpreter("3.14", "3.14.8")
	adapter := host.adapter(testLock(t, "3.14"))
	host.fillCache(adapter, "3.14")
	interpreter, err := adapter.Select(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	return host, adapter, interpreter
}

func runtimeEntries(t *testing.T, adapter *Adapter) []string {
	t.Helper()
	entries, err := os.ReadDir(adapter.runtimes)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestABuildPublishesAVerifiedRuntime(t *testing.T) {
	host, adapter, interpreter := buildable(t)
	runtime, err := adapter.Build(context.Background(), interpreter)
	if err != nil {
		t.Fatal(err)
	}
	if want := prerequisites.RuntimeDigest(adapter.lock.Digest(), "3.14", interpreter.Path); runtime.Digest != want {
		t.Fatalf("digest = %s, want %s", runtime.Digest, want)
	}
	if got := runtimeEntries(t, adapter); !slices.Equal(got, []string{runtime.Digest}) {
		t.Fatalf("runtimes = %v, want only %s", got, runtime.Digest)
	}
	directory := filepath.Join(adapter.runtimes, runtime.Digest)
	shim, err := os.ReadFile(filepath.Join(directory, shimName))
	if err != nil || string(shim) != "#!/bin/sh\nexec '"+filepath.Join(directory, "bin", "python")+"' -I -B \"$@\"\n" {
		t.Fatalf("shim = %q, %v", shim, err)
	}
	requirements, err := os.ReadFile(filepath.Join(directory, "requirements.lock"))
	files, _ := adapter.lock.Files("3.14")
	if err != nil || strings.Count(string(requirements), " --hash=sha256:") != len(files) {
		t.Fatalf("requirements.lock = %q, %v", requirements, err)
	}
	for _, leftover := range []string{".wheels", ".home"} {
		if _, err := os.Lstat(filepath.Join(directory, leftover)); !os.IsNotExist(err) {
			t.Fatalf("the published runtime keeps %s", leftover)
		}
	}
	for _, call := range host.commands() {
		for _, entry := range call.Environment {
			if !strings.HasPrefix(entry, "LC_ALL=") && !strings.HasPrefix(entry, "LANG=") && !strings.HasPrefix(entry, "PATH=/usr/sbin:/usr/bin") &&
				!strings.HasPrefix(entry, "HOME=") && entry != "PIP_CONFIG_FILE=/dev/null" {
				t.Fatalf("%v ran with the ambient %s", call.Arguments, entry)
			}
		}
	}
	_, ready, err := adapter.Inspect(context.Background(), interpreter)
	if err != nil || !ready {
		t.Fatalf("Inspect = %t, %v after a build", ready, err)
	}
}

func TestTheVenvHoldsNoPthOrStartupHook(t *testing.T) {
	for _, hook := range []string{"distutils-precedence.pth", "sitecustomize.py", "usercustomize.py"} {
		t.Run(hook, func(t *testing.T) {
			host, adapter, interpreter := buildable(t)
			host.startupHook = hook
			if _, err := adapter.Build(context.Background(), interpreter); err == nil {
				t.Fatalf("a venv holding %s was published", hook)
			}
			if got := runtimeEntries(t, adapter); len(got) != 0 {
				t.Fatalf("a refused build left %v", got)
			}
		})
	}
	t.Run("system site-packages", func(t *testing.T) {
		host, adapter, interpreter := buildable(t)
		host.systemSite = true
		if _, err := adapter.Build(context.Background(), interpreter); err == nil {
			t.Fatal("a venv that includes the system site-packages was published")
		}
	})
}

func TestBytecodeIsUncheckedHash(t *testing.T) {
	host, adapter, interpreter := buildable(t)
	if _, err := adapter.Build(context.Background(), interpreter); err != nil {
		t.Fatal(err)
	}
	var compile []string
	for _, call := range host.commands() {
		if slices.Contains(call.Arguments, "compileall") {
			compile = call.Arguments
		}
	}
	if !slices.Contains(compile, "unchecked-hash") || compile[slices.Index(compile, "unchecked-hash")-1] != "--invalidation-mode" {
		t.Fatalf("compileall ran as %v", compile)
	}
	for _, flags := range []uint32{0, 3} {
		host, adapter, interpreter := buildable(t)
		host.pycFlags = flags
		if _, err := adapter.Build(context.Background(), interpreter); err == nil {
			t.Fatalf("bytecode with flags %d was published", flags)
		}
	}
}

func TestRuntimeJsonIsWrittenBeforePublication(t *testing.T) {
	host, adapter, interpreter := buildable(t)
	runtime, err := adapter.Build(context.Background(), interpreter)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(host.publications, []string{"runtime.json present: true"}) {
		t.Fatalf("publications = %v", host.publications)
	}
	record, err := readRecord(filepath.Join(adapter.runtimes, runtime.Digest, recordName))
	if err != nil {
		t.Fatal(err)
	}
	if record.Format != prerequisites.RuntimeFormat || record.Runtime.Digest != runtime.Digest || record.Runtime.AnsibleCore != testAnsibleCore ||
		record.Runtime.Interpreter.Version != "3.14.8" || record.Runtime.LockDigest != adapter.lock.Digest() {
		t.Fatalf("runtime.json = %+v", record)
	}
}

func TestConcurrentPublishersLeaveOneRuntime(t *testing.T) {
	_, adapter, interpreter := buildable(t)
	barrier := &sync.WaitGroup{}
	barrier.Add(2)
	adapter.store.(*renameStore).barrier = barrier
	results := make([]prerequisites.Runtime, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	for index := range 2 {
		group.Go(func() { results[index], errs[index] = adapter.Build(context.Background(), interpreter) })
	}
	group.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("publisher %d: %v", index, err)
		}
	}
	if results[0].Digest != results[1].Digest {
		t.Fatalf("the publishers returned %s and %s", results[0].Digest, results[1].Digest)
	}
	if got := runtimeEntries(t, adapter); !slices.Equal(got, []string{results[0].Digest}) {
		t.Fatalf("runtimes = %v, want one runtime and no stage", got)
	}
}

func TestABuildReplacesAPublishedRuntimeThatFailsItsProbe(t *testing.T) {
	_, adapter, interpreter := buildable(t)
	runtime, err := adapter.Build(context.Background(), interpreter)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(adapter.runtimes, runtime.Digest)
	if err := os.WriteFile(filepath.Join(directory, brokenMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := adapter.Inspect(context.Background(), interpreter); ready || err == nil {
		t.Fatalf("Inspect = %t, %v on a runtime an update broke", ready, err)
	}
	rebuilt, err := adapter.Build(context.Background(), interpreter)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Digest != runtime.Digest {
		t.Fatalf("the rebuild published %s, want %s", rebuilt.Digest, runtime.Digest)
	}
	if _, err := os.Lstat(filepath.Join(directory, brokenMarker)); !os.IsNotExist(err) {
		t.Fatal("the build adopted the broken runtime instead of replacing it")
	}
	if got := runtimeEntries(t, adapter); !slices.Equal(got, []string{runtime.Digest}) {
		t.Fatalf("runtimes = %v, want one runtime and no stage", got)
	}
	if _, ready, err := adapter.Inspect(context.Background(), interpreter); !ready || err != nil {
		t.Fatalf("Inspect = %t, %v after the replacement", ready, err)
	}
}

func TestABrokenRuntimeALaunchHoldsIsNotAdopted(t *testing.T) {
	_, adapter, interpreter := buildable(t)
	runtime, err := adapter.Build(context.Background(), interpreter)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(adapter.runtimes, runtime.Digest)
	if err := os.WriteFile(filepath.Join(directory, brokenMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err = adapter.WithPython(context.Background(), runtime, func(prerequisites.PythonLaunch) error {
		if _, err := adapter.Build(context.Background(), interpreter); err == nil {
			t.Error("a build adopted a broken runtime a launch holds")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(directory, brokenMarker)); err != nil {
		t.Fatal("a build removed a runtime a launch holds")
	}
	if got := runtimeEntries(t, adapter); !slices.Equal(got, []string{runtime.Digest}) {
		t.Fatalf("runtimes = %v, want the held runtime and no stage", got)
	}
}

func TestABuildNeedsEveryLockedWheelCached(t *testing.T) {
	_, adapter, interpreter := buildable(t)
	if err := os.Remove(adapter.cachePath(adapter.lock.Installer())); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Build(context.Background(), interpreter); err == nil {
		t.Fatal("a build ran without the installer wheel")
	}
	if got := runtimeEntries(t, adapter); len(got) != 0 {
		t.Fatalf("a refused build left %v", got)
	}
}
