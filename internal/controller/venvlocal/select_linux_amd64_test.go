//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestSelectionPrefersTheReceiptsInterpreterThenTheNewestABI(t *testing.T) {
	host := newFakeHost(t)
	older := host.interpreter("3.12", "3.12.11")
	newest := host.interpreter("3.14", "3.14.8")
	adapter := host.adapter(testLock(t, "3.14", "3.13", "3.12"))
	for _, test := range []struct {
		name, preferred, want string
	}{
		{"no receipt takes the newest locked ABI", "", newest},
		{"the receipt's interpreter comes first", older, older},
		{"an unlocked receipt interpreter is no candidate", "/usr/bin/python3", newest},
	} {
		t.Run(test.name, func(t *testing.T) {
			interpreter, err := adapter.Select(context.Background(), test.preferred)
			if err != nil {
				t.Fatal(err)
			}
			if interpreter.Path != test.want {
				t.Fatalf("selected %s, want %s", interpreter.Path, test.want)
			}
		})
	}
	interpreter, _ := adapter.Select(context.Background(), "")
	stdlib := filepath.Join(host.root, "usr", "lib64", "python3.14")
	want := prerequisites.PythonInterpreter{Path: newest, ABI: "3.14", Version: "3.14.8", Glibc: "2.43", Stdlib: []string{stdlib, filepath.Join(stdlib, "lib-dynload")}}
	if interpreter.Path != want.Path || interpreter.ABI != want.ABI || interpreter.Version != want.Version || interpreter.Glibc != want.Glibc ||
		len(interpreter.Stdlib) != 2 || interpreter.Stdlib[0] != want.Stdlib[0] || interpreter.Stdlib[1] != want.Stdlib[1] {
		t.Fatalf("interpreter = %+v, want %+v", interpreter, want)
	}
}

func TestAnInterpreterBelowAWritableDirectoryIsSkipped(t *testing.T) {
	host := newFakeHost(t)
	newest := host.interpreter("3.14", "3.14.8")
	older := host.interpreter("3.13", "3.13.9")
	adapter := host.adapter(testLock(t, "3.14", "3.13"))
	writable := filepath.Join(host.root, "usr", "bin")
	if err := os.Chmod(writable, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.Select(context.Background(), "")
	if !errors.Is(err, prerequisites.ErrNoPythonInterpreter) {
		t.Fatalf("both interpreters share a writable directory, yet Select returned %v", err)
	}
	if err := os.Chmod(writable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(newest, 0o777); err != nil {
		t.Fatal(err)
	}
	interpreter, err := adapter.Select(context.Background(), newest)
	if err != nil {
		t.Fatal(err)
	}
	if interpreter.Path != older {
		t.Fatalf("selected %s, want %s past the group-writable %s", interpreter.Path, older, newest)
	}
}

func TestTheStdlibDirectoriesAreWalked(t *testing.T) {
	for _, directory := range []string{"python3.14", filepath.Join("python3.14", "lib-dynload")} {
		t.Run(directory, func(t *testing.T) {
			host := newFakeHost(t)
			newest := host.interpreter("3.14", "3.14.8")
			older := host.interpreter("3.13", "3.13.9")
			adapter := host.adapter(testLock(t, "3.14", "3.13"))
			if err := os.Chmod(filepath.Join(host.root, "usr", "lib64", directory), 0o775); err != nil {
				t.Fatal(err)
			}
			interpreter, err := adapter.Select(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if interpreter.Path != older {
				t.Fatalf("selected %s although its %s is group-writable", interpreter.Path, directory)
			}
			for _, call := range host.commands() {
				if call.Path == newest {
					t.Fatalf("ran %s %v before refusing its group-writable %s", newest, call.Arguments, directory)
				}
			}
		})
	}
}

func TestAnInterpreterBelowTheGlibcFloorOrOfAnotherABIIsSkipped(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*interpreterReport)
	}{
		{"glibc below the floor", func(report *interpreterReport) { report.Libc = "glibc 2.16" }},
		{"no glibc", func(report *interpreterReport) { report.Libc = "" }},
		{"another ABI", func(report *interpreterReport) { report.ABI, report.Version = "3.13", "3.13.9" }},
		{"another implementation", func(report *interpreterReport) { report.CacheTag = "pypy314" }},
		{"another executable", func(report *interpreterReport) { report.Executable = "/usr/bin/python3" }},
		{"a stdlib other than the walked one", func(report *interpreterReport) { report.Stdlib = "/opt/python3.14" }},
		{"a lib-dynload other than the walked one", func(report *interpreterReport) { report.Dynload = []string{"/opt/lib-dynload"} }},
		{"a second lib-dynload", func(report *interpreterReport) { report.Dynload = append(report.Dynload, "/opt/lib-dynload") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := newFakeHost(t)
			path := host.interpreter("3.14", "3.14.8")
			report := host.reports[path]
			test.change(&report)
			host.reports[path] = report
			_, err := host.adapter(testLock(t, "3.14")).Select(context.Background(), "")
			if !errors.Is(err, prerequisites.ErrNoPythonInterpreter) {
				t.Fatalf("Select = %v, want no qualified interpreter", err)
			}
		})
	}
}
