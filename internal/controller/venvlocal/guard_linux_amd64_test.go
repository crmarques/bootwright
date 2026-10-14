//go:build linux && amd64

package venvlocal

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func built(t *testing.T) (*fakeHost, *Adapter, prerequisites.Runtime) {
	t.Helper()
	host, adapter, interpreter := buildable(t)
	runtime, err := adapter.Build(context.Background(), interpreter)
	if err != nil {
		t.Fatal(err)
	}
	return host, adapter, runtime
}

func exclusive(t *testing.T, path string) (*os.File, error) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return file, syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func TestTheGuardHoldsTheRuntimeSharedForTheBlock(t *testing.T) {
	_, adapter, runtime := built(t)
	directory := filepath.Join(adapter.runtimes, runtime.Digest)
	var launch prerequisites.PythonLaunch
	err := adapter.WithPython(context.Background(), runtime, func(given prerequisites.PythonLaunch) error {
		launch = given
		retire, err := exclusive(t, filepath.Join(directory, lockName))
		defer retire.Close()
		if err == nil {
			t.Error("retirement took the launch lock while a block held it")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Program != filepath.Join(directory, "bin", "python") || launch.Shim != filepath.Join(directory, shimName) ||
		!slices.Equal(launch.Arguments, []string{"-I", "-B"}) || launch.Loader != "" ||
		!slices.Equal(launch.Environment, []string{"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "PATH=/usr/sbin:/usr/bin"}) {
		t.Fatalf("launch = %+v", launch)
	}
	retire, err := exclusive(t, filepath.Join(directory, lockName))
	defer retire.Close()
	if err != nil {
		t.Fatalf("the launch lock outlived the block: %v", err)
	}
	called := false
	if err := adapter.WithPython(context.Background(), runtime, func(prerequisites.PythonLaunch) error { called = true; return nil }); err == nil || called {
		t.Fatal("a launch ran while retirement held the runtime")
	}
}

func TestTheGuardRefusesARuntimeThatNoLongerVerifies(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, *Adapter, prerequisites.Runtime) prerequisites.Runtime
	}{
		{"another digest", func(t *testing.T, adapter *Adapter, runtime prerequisites.Runtime) prerequisites.Runtime {
			other := runtime
			other.LockDigest = "0000000000000000000000000000000000000000000000000000000000000000"
			return other
		}},
		{"a runtime.json naming another runtime", func(t *testing.T, adapter *Adapter, runtime prerequisites.Runtime) prerequisites.Runtime {
			record := filepath.Join(adapter.runtimes, runtime.Digest, recordName)
			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)-3] = ' '
			if err := os.WriteFile(record, data, 0o644); err != nil {
				t.Fatal(err)
			}
			return runtime
		}},
		{"a group-writable runtime directory", func(t *testing.T, adapter *Adapter, runtime prerequisites.Runtime) prerequisites.Runtime {
			if err := os.Chmod(filepath.Join(adapter.runtimes, runtime.Digest), 0o770); err != nil {
				t.Fatal(err)
			}
			return runtime
		}},
		{"a group-writable stdlib", func(t *testing.T, adapter *Adapter, runtime prerequisites.Runtime) prerequisites.Runtime {
			if err := os.Chmod(runtime.Interpreter.Stdlib[1], 0o775); err != nil {
				t.Fatal(err)
			}
			return runtime
		}},
		{"a bin/python leading elsewhere", func(t *testing.T, adapter *Adapter, runtime prerequisites.Runtime) prerequisites.Runtime {
			link := filepath.Join(adapter.runtimes, runtime.Digest, "bin", "python")
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/usr/bin/true", link); err != nil {
				t.Fatal(err)
			}
			return runtime
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, adapter, runtime := built(t)
			runtime = test.change(t, adapter, runtime)
			called := false
			err := adapter.WithPython(context.Background(), runtime, func(prerequisites.PythonLaunch) error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("WithPython = %v, launched %t", err, called)
			}
		})
	}
}

func TestInspectReportsAnAbsentRuntimeAsNotReady(t *testing.T) {
	_, adapter, interpreter := buildable(t)
	runtime, ready, err := adapter.Inspect(context.Background(), interpreter)
	if err != nil || ready {
		t.Fatalf("Inspect = %t, %v before any build", ready, err)
	}
	if runtime.Digest != prerequisites.RuntimeDigest(adapter.lock.Digest(), interpreter.ABI, interpreter.Path) {
		t.Fatalf("runtime = %+v", runtime)
	}
}
