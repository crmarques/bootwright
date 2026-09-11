//go:build linux && amd64

package hostlinux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"golang.org/x/sys/unix"
)

func TestRuntimeVerifiesExactLibraryAndConfigurationAliasesReadOnly(t *testing.T) {
	i := fixture(t, "btrfs")
	requirement := linkedRuntimeFixture(t, i)
	before := snapshot(t, i.view.root)
	observed, err := i.Runtime(context.Background(), requirement)
	if err != nil || !observed.Present || !observed.Ready {
		t.Fatal(observed, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, i.view.root)) {
		t.Fatal("runtime alias inspection changed metadata")
	}
	// A different target spelling reaches the same bytes but is not the
	// qualified loader alias; content equality does not waive target identity.
	name := filepath.Join(i.view.root, "usr/lib64/libnative.so")
	must(t, os.Remove(name))
	must(t, os.Symlink("./libnative.so.1", name))
	observed, err = i.Runtime(context.Background(), requirement)
	if err != nil || !observed.Present || observed.Ready {
		t.Fatal("changed alias accepted", observed, err)
	}
}

func TestRuntimeDistinguishesMissingDependencyFromConflictingContent(t *testing.T) {
	for _, missing := range []bool{false, true} {
		i := fixture(t, "btrfs")
		requirement := runtimeFixture(t, i)
		name := filepath.Join(i.view.root, "usr/lib/podman/support")
		if missing {
			must(t, os.Remove(name))
		} else {
			must(t, os.WriteFile(name, []byte("different installed content"), 0644))
		}
		observed, err := i.Runtime(context.Background(), requirement)
		if err != nil || !observed.Present || observed.Ready || observed.Conflict == missing {
			t.Fatalf("missing=%t: %+v %v", missing, observed, err)
		}
	}
}

func TestRuntimeRequiresSelectedNativeCLIToRemainExecutable(t *testing.T) {
	i := fixture(t, "btrfs")
	requirement := runtimeFixture(t, i)
	content := "qualified native CLI"
	writeFixture(t, i, "/usr/bin/nmstatectl", content, 0755)
	digest := sha256.Sum256([]byte(content))
	requirement.Files = append(requirement.Files, prerequisites.InstalledFile{Path: "/usr/bin/nmstatectl", SHA256: hex.EncodeToString(digest[:])})
	observed, err := i.Runtime(context.Background(), requirement)
	if err != nil || !observed.Ready {
		t.Fatal(observed, err)
	}
	must(t, os.Chmod(filepath.Join(i.view.root, "usr/bin/nmstatectl"), 0644))
	observed, err = i.Runtime(context.Background(), requirement)
	if err != nil || observed.Ready || !observed.Conflict {
		t.Fatal("nonexecutable native CLI accepted", observed, err)
	}
}

func TestRuntimeAliasManifestRejectsOutsideRootsAndDuplicateEvidence(t *testing.T) {
	for _, variant := range []string{"source-outside", "target-outside", "duplicate-link", "file-link-collision", "unbounded", "file-outside"} {
		t.Run(variant, func(t *testing.T) {
			i := fixture(t, "btrfs")
			r := linkedRuntimeFixture(t, i)
			switch variant {
			case "source-outside":
				r.Links[0].Path = "/tmp/alias"
			case "target-outside":
				r.Links[0].Target = "../../../../tmp/alias"
			case "duplicate-link":
				r.Links = append(r.Links, r.Links[0])
			case "file-link-collision":
				r.Links[0].Path = r.Files[0].Path
			case "unbounded":
				r.Links = make([]prerequisites.InstalledLink, maxRuntimeLinks+1)
			case "file-outside":
				r.Files[1].Path = "/etc/shadow"
			}
			_, err := i.Runtime(context.Background(), r)
			assertDiagnostic(t, err, "controller.unsupported")
		})
	}
}

func TestRuntimeAliasesCannotHideUnqualifiedOrMissingTargets(t *testing.T) {
	for _, variant := range []string{"missing", "regular-alias", "unhashed-target", "undeclared-chain", "directory-mutable", "cycle", "file-replaced-with-link", "undeclared-ancestor"} {
		t.Run(variant, func(t *testing.T) {
			i := fixture(t, "btrfs")
			r := linkedRuntimeFixture(t, i)
			alias := filepath.Join(i.view.root, "usr/lib64/libnative.so")
			switch variant {
			case "missing":
				must(t, os.Remove(alias))
			case "regular-alias":
				must(t, os.Remove(alias))
				must(t, os.WriteFile(alias, []byte("library"), 0644))
			case "unhashed-target":
				r.Files = r.Files[:len(r.Files)-1]
			case "undeclared-chain":
				must(t, os.Remove(alias))
				must(t, os.Symlink("second.so", alias))
				must(t, os.Symlink("libnative.so.1", filepath.Join(i.view.root, "usr/lib64/second.so")))
				r.Links[0].Target = "second.so"
			case "directory-mutable":
				must(t, os.Chmod(filepath.Join(i.view.root, "usr/lib64"), 0777))
			case "cycle":
				must(t, os.Remove(alias))
				must(t, os.Symlink("libnative.so", alias))
				r.Links[0].Target = "libnative.so"
			case "file-replaced-with-link":
				name := filepath.Join(i.view.root, "usr/lib/podman/support")
				must(t, os.Rename(name, name+".real"))
				must(t, os.Symlink("support.real", name))
				r.Links = append(r.Links, prerequisites.InstalledLink{Path: "/usr/lib/podman/support", Target: "support.real"})
				// A final file alias without independent declaration is refused.
				r.Links = r.Links[:len(r.Links)-1]
			case "undeclared-ancestor":
				name := filepath.Join(i.view.root, "usr/lib/podman")
				must(t, os.Rename(name, name+"-real"))
				must(t, os.Symlink("podman-real", name))
			}
			before := snapshot(t, i.view.root)
			observed, err := i.Runtime(context.Background(), r)
			if err != nil || !observed.Present || observed.Ready {
				t.Fatal("unsafe alias accepted", observed, err)
			}
			if !reflect.DeepEqual(before, snapshot(t, i.view.root)) {
				t.Fatal("alias refusal changed metadata")
			}
		})
	}
}

func TestRuntimeOwnsManifestSlicesDuringInspection(t *testing.T) {
	i := fixture(t, "btrfs")
	r := linkedRuntimeFixture(t, i)
	i.view.stat = func(name string, _ *unix.Stat_t) {
		if name == "/usr/bin/podman" {
			r.Files[0].SHA256 = "changed"
			r.Links[0].Target = "changed"
		}
	}
	observed, err := i.Runtime(context.Background(), r)
	if err != nil || !observed.Ready {
		t.Fatal("caller mutation changed admitted file proof", observed, err)
	}
}

func linkedRuntimeFixture(t *testing.T, i Inspector) prerequisites.RuntimeRequirement {
	t.Helper()
	r := runtimeFixture(t, i)
	for _, name := range []string{"/etc/containers/registries.conf", "/etc/selinux/targeted/policy/policy.33", "/usr/lib64/libnative.so.1"} {
		data := "qualified fixture bytes"
		writeFixture(t, i, name, data, 0644)
		digest := sha256.Sum256([]byte(data))
		r.Files = append(r.Files, prerequisites.InstalledFile{Path: name, SHA256: hex.EncodeToString(digest[:])})
	}
	r.Links = []prerequisites.InstalledLink{{Path: "/usr/lib64/libnative.so", Target: "libnative.so.1"}, {Path: "/lib64", Target: "usr/lib64"}}
	for _, link := range r.Links {
		must(t, os.Symlink(link.Target, filepath.Join(i.view.root, link.Path)))
	}
	return r
}
