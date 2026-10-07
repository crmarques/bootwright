//go:build linux && amd64

package bundlelocal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Every file a compiled foundation pins is provided by exactly one package
// build, glibc or libgcc, of its own release, each package lists only pinned
// files, and every pinned link resolves to an attributed file or to the
// library directory itself, so a refusal can always name what provides the
// path it found drifted.
func TestEveryCatalogFoundationFileHasOnePackage(t *testing.T) {
	record, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{"fedora": ".fc43", "rhel": ".el9"}
	for _, native := range record.Native {
		owners := map[string]string{}
		var names []string
		for _, pkg := range native.Packages {
			names = append(names, pkg.Name)
			if !strings.Contains(pkg.Build, tags[native.OS]) || len(pkg.Files) == 0 {
				t.Errorf("%s %s: %s %q is not a build of this release providing files", native.OS, native.Release, pkg.Name, pkg.Build)
			}
			for _, file := range pkg.Files {
				if owner, found := owners[file]; found {
					t.Errorf("%s %s: %s is provided by both %s and %s", native.OS, native.Release, file, owner, pkg.Name)
				}
				owners[file] = pkg.Name
			}
		}
		if !slices.Equal(names, executionPackageOwners()) {
			t.Errorf("%s %s attributes its foundation to %v", native.OS, native.Release, names)
		}
		for _, file := range native.Execution.Files {
			if owners[file.Path] == "" {
				t.Errorf("%s %s: no package provides %s", native.OS, native.Release, file.Path)
			}
		}
		if len(owners) != len(native.Execution.Files) {
			t.Errorf("%s %s attributes %d files to its %d pinned files", native.OS, native.Release, len(owners), len(native.Execution.Files))
		}
		for _, link := range native.Execution.Links {
			if _, attributed := foundationOwner(executionView{packages: native.Packages}, native.Execution, link.Path); !attributed && link.Target != "usr/lib64" {
				t.Errorf("%s %s: the link %s reaches no attributed file", native.OS, native.Release, link.Path)
			}
		}
	}
}

// syntheticFoundation writes a Fedora 43 host's foundation beneath a new root,
// owned by this test's account: its release, loader and libraries, the links
// that reach them and the package lock. It returns the root and a stub rpm
// that attributes the libgcc file to libgcc and every other to glibc.
func syntheticFoundation(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("etc/os-release", "NAME=\"Fedora Linux\"\nID=fedora\nVERSION_ID=43\n", 0o644)
	write("usr/lib/sysimage/rpm/.rpm.lock", "", 0o644)
	for _, name := range []string{"ld-linux-x86-64.so.2", "libc.so.6", "libdl.so.2", "libgcc_s-15-20260722.so.1", "libm.so.6", "libpthread.so.0", "librt.so.1", "libutil.so.1"} {
		write("usr/lib64/"+name, "synthetic qualified ELF "+name, 0o755)
	}
	for link, target := range map[string]string{"lib64": "usr/lib64", "usr/lib64/libgcc_s.so.1": "libgcc_s-15-20260722.so.1"} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	stub := filepath.Join(t.TempDir(), "rpm")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor last; do :; done\ncase $last in\n*/libgcc_s-*) echo 'libgcc 15.3.1-1.fc43' ;;\n/usr/lib64/*) echo 'glibc 2.42-16.fc43' ;;\n*) exit 1 ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, stub
}

// foundationCatalog runs the regeneration tool over root, attributing with
// stub, and returns what it printed.
func foundationCatalog(t *testing.T, root, stub string) []byte {
	t.Helper()
	command := exec.Command("sh", filepath.Join("..", "..", "..", "scripts", "foundation-catalog"))
	command.Env = append(os.Environ(), "FOUNDATION_ROOT="+root, "FOUNDATION_RPM="+stub)
	var output, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err != nil {
		t.Fatalf("the foundation catalog tool failed: %v: %s", err, diagnostic.String())
	}
	return output.Bytes()
}

// The regeneration tool prints, from a qualified host, the native record the
// compiled catalog holds for its release: a record that decodes strictly and
// passes the execution requirement's own validation, that the guard verifies
// against the very host it was read from, naming the package builds that
// provide it, and that a rerun prints byte for byte. Once a pinned file
// changes, the guard refuses that host, naming the file and its build.
func TestTheFoundationCatalogToolRoundTrips(t *testing.T) {
	root, stub := syntheticFoundation(t)
	printed := foundationCatalog(t, root, stub)
	var record nativeRecord
	decoder := json.NewDecoder(bytes.NewReader(printed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		t.Fatalf("the printed record does not decode strictly: %v\n%s", err, printed)
	}
	if record.OS != "fedora" || record.Release != "43" || !validExecutionRequirement(record.Execution) || len(record.Execution.Files) != 8 {
		t.Fatalf("the printed record is not a qualified foundation: %+v", record)
	}
	guard := ExecutionGuard{view: executionView{root: root, owner: uint32(os.Geteuid())}}
	inspection, err := guard.inspectRecord(context.Background(), record)
	if err != nil || inspection.Refusal != nil || inspection.Required != "glibc 2.42-16.fc43, libgcc 15.3.1-1.fc43" {
		t.Fatalf("the guard did not verify the host the record was read from: %+v %v", inspection, err)
	}
	if again := foundationCatalog(t, root, stub); !bytes.Equal(again, printed) {
		t.Fatalf("a rerun printed another record:\n%s\nwant\n%s", again, printed)
	}
	if err := os.WriteFile(filepath.Join(root, "usr/lib64/libm.so.6"), []byte("a later glibc build"), 0o755); err != nil {
		t.Fatal(err)
	}
	inspection, err = guard.inspectRecord(context.Background(), record)
	found := diagnostics.Of(inspection.Refusal)
	if err != nil || inspection.Drift != "/usr/lib64/libm.so.6" || len(found) != 1 ||
		found[0].Message != "the provided execution foundation differs at /usr/lib64/libm.so.6, from glibc 2.42-16.fc43, which holds other content than this build pins" {
		t.Fatalf("the edited host was not refused by name: %+v %#v %v", inspection, found, err)
	}
}
