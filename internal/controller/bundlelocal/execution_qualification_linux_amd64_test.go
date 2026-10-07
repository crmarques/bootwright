//go:build linux && amd64

package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// installedBuilds is a FoundationBuildReader over a fixed RPM database.
type installedBuilds struct {
	builds []prerequisites.InstalledBuild
	reads  int
	asked  []string
}

func (b *installedBuilds) FoundationBuilds(_ context.Context, platform prerequisites.Platform, names []string) ([]prerequisites.InstalledBuild, error) {
	b.reads++
	b.asked = append(b.asked, platform.OS+" "+platform.Release+" "+strings.Join(names, ","))
	return b.builds, nil
}

var qualificationPackages = []foundationPackage{
	{Name: "glibc", Build: "2.42-16.fc43", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2", "/usr/lib64/libc.so.6"}},
	{Name: "libgcc", Build: "15.3.1-1.fc43", Files: []string{"/usr/lib64/libgcc_s-1.so.1"}},
}

const glibcRemedy = "Install exactly glibc 2.42-16.fc43 again with dnf (dnf install glibc-2.42-16.fc43, or dnf reinstall glibc-2.42-16.fc43 while that build is installed), hold it with dnf versionlock add glibc-2.42-16.fc43, then repeat this command; a host that must take the update needs a Bootwright build whose execution foundation pins it."

// errataHost is the synthetic foundation after a z-stream update: glibc's
// libc.so.6 and libgcc's versioned library, renamed with its link, hold new
// bytes, and the RPM database records both builds, vendor-signed, of the
// compiled upstream versions, with an i686 glibc beside them.
type errataHost struct {
	executionFixture
	builds *installedBuilds
	libc   string
	libgcc string
}

func sha256Of(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func replaceFoundationFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.Chmod(name, 0o644); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o555); err != nil {
		t.Fatal(err)
	}
}

func newErrataHost(t *testing.T) errataHost {
	t.Helper()
	f := newExecutionFixture(t)
	libc, libgcc := []byte("glibc 2.42-17.fc43 libc.so.6"), []byte("libgcc 15.3.1-2.fc43 libgcc_s")
	replaceFoundationFile(t, filepath.Join(f.root, "usr/lib64/libc.so.6"), libc)
	if err := os.Remove(filepath.Join(f.root, "usr/lib64/libgcc_s-1.so.1")); err != nil {
		t.Fatal(err)
	}
	replaceFoundationFile(t, filepath.Join(f.root, "usr/lib64/libgcc_s-2.so.1"), libgcc)
	link := filepath.Join(f.root, "usr/lib64/libgcc_s.so.1")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("libgcc_s-2.so.1", link); err != nil {
		t.Fatal(err)
	}
	builds := &installedBuilds{builds: []prerequisites.InstalledBuild{
		{Name: "glibc", Version: "2.42", Release: "17.fc43", Architecture: "x86_64", Signed: true, DigestAlgorithm: 8, Files: map[string]prerequisites.InstalledBuildFile{
			"/usr/lib64/ld-linux-x86-64.so.2": {SHA256: f.requirement.Files[0].SHA256},
			"/usr/lib64/libc.so.6":            {SHA256: sha256Of(libc)},
			"/usr/lib64":                      {},
		}},
		{Name: "glibc", Version: "2.42", Release: "17.fc43", Architecture: "i686", Signed: false, DigestAlgorithm: 8, Files: map[string]prerequisites.InstalledBuildFile{
			"/usr/lib/libc.so.6": {SHA256: strings.Repeat("0", 64)},
		}},
		{Name: "libgcc", Version: "15.3.1", Release: "2.fc43", Architecture: "x86_64", Signed: true, DigestAlgorithm: 8, Files: map[string]prerequisites.InstalledBuildFile{
			"/lib64/libgcc_s-2.so.1": {SHA256: sha256Of(libgcc)},
			"/lib64/libgcc_s.so.1":   {LinkTo: "libgcc_s-2.so.1"},
		}},
	}}
	f.guard.Builds = builds
	f.guard.view.packages = qualificationPackages
	f.guard.view.platform = prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	return errataHost{executionFixture: f, builds: builds, libc: sha256Of(libc), libgcc: sha256Of(libgcc)}
}

func (h errataHost) inspect(t *testing.T) prerequisites.FoundationInspection {
	t.Helper()
	inspection, err := h.guard.inspectRecord(context.Background(), nativeRecord{OS: "fedora", Release: "43", Execution: h.requirement, Packages: qualificationPackages})
	if err != nil {
		t.Fatal(err)
	}
	return inspection
}

func (h errataHost) refused(t *testing.T, message, remedy string) {
	t.Helper()
	inspection := h.inspect(t)
	found := diagnostics.Of(inspection.Refusal)
	if inspection.Qualified != nil || len(found) != 1 || found[0].Code != "controller.unsupported" || found[0].Message != message || found[0].Remediation != remedy {
		t.Fatalf("the inspection reported %+v with %#v", inspection, found)
	}
}

// A z-stream errata qualifies from the RPM database: each pinned file takes
// the vendor-signed build's digest, the renamed libgcc library, its link and
// its preload entry follow the build's file list, the i686 instance is
// ignored, and the qualified requirement then launches byte for byte while
// the compiled one is refused.
func TestAZStreamFoundationQualifiesFromTheRPMDatabase(t *testing.T) {
	h := newErrataHost(t)
	inspection := h.inspect(t)
	want := &prerequisites.QualifiedFoundation{
		Execution: prerequisites.ExecutionRequirement{
			Loader: h.requirement.Loader, LockPath: h.requirement.LockPath,
			Files:   []prerequisites.InstalledFile{h.requirement.Files[0], {Path: "/usr/lib64/libc.so.6", SHA256: h.libc}, {Path: "/usr/lib64/libgcc_s-2.so.1", SHA256: h.libgcc}},
			Links:   []prerequisites.InstalledLink{{Path: "/lib64", Target: "usr/lib64"}, {Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-2.so.1"}},
			Preload: []string{"/usr/lib64/libc.so.6", "/usr/lib64/libgcc_s-2.so.1"},
		},
		Packages: []prerequisites.FoundationBuild{
			{Name: "glibc", Build: "2.42-17.fc43", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2", "/usr/lib64/libc.so.6"}},
			{Name: "libgcc", Build: "15.3.1-2.fc43", Files: []string{"/usr/lib64/libgcc_s-2.so.1"}},
		},
	}
	if inspection.Refusal != nil || inspection.Drift != "" || !reflect.DeepEqual(inspection.Qualified, want) || inspection.Required != "glibc 2.42-16.fc43, libgcc 15.3.1-1.fc43" {
		t.Fatalf("the errata host inspected as %+v, want qualified %+v", inspection, want)
	}
	if len(h.builds.asked) != 1 || h.builds.asked[0] != "fedora 43 glibc,libgcc" {
		t.Fatalf("the RPM database was asked %q", h.builds.asked)
	}
	if got := prerequisites.FoundationSummary(*inspection.Qualified, h.guard.view.platform); got != "glibc 2.42-17.fc43, libgcc 15.3.1-2.fc43 (vendor-signed, qualified within fedora 43)" {
		t.Fatalf("the qualified builds read %q", got)
	}
	reads := h.builds.reads
	launched := false
	if err := h.guard.WithPython(context.Background(), &h.area, inspection.Qualified.Execution, func(prerequisites.PythonLaunch, func() error) error {
		launched = true
		return nil
	}); err != nil || !launched {
		t.Fatalf("the qualified foundation did not launch: %v", err)
	}
	if err := h.guard.WithPython(context.Background(), &h.area, h.requirement, func(prerequisites.PythonLaunch, func() error) error {
		t.Fatal("the compiled foundation launched over an errata host")
		return nil
	}); err == nil {
		t.Fatal("the compiled foundation was accepted over an errata host")
	}
	if h.builds.reads != reads {
		t.Fatal("a launch read the RPM database")
	}
}

// A build no vendor signature covers, or that another key signed, never
// qualifies: the refusal names the build and the key it lacks, and reinstalls
// the compiled build.
func TestAForeignSignedFoundationBuildRefuses(t *testing.T) {
	h := newErrataHost(t)
	h.builds.builds[0].Signed = false
	h.refused(t, "the provided execution foundation differs at /usr/lib64/libc.so.6, from glibc 2.42-16.fc43, which holds other content than this build pins, and glibc 2.42-17.fc43 is not signed by the fedora vendor key", glibcRemedy)
}

// Another upstream version is not a z-stream errata of the compiled build.
func TestAnotherUpstreamFoundationVersionRefuses(t *testing.T) {
	h := newErrataHost(t)
	h.builds.builds[0].Version = "2.43"
	h.refused(t, "the provided execution foundation differs at /usr/lib64/libc.so.6, from glibc 2.42-16.fc43, which holds other content than this build pins, and glibc 2.43-17.fc43 is not upstream version 2.42, which this build pins", glibcRemedy)
}

// A file whose bytes differ from the digest its build records is refused,
// naming the build and reinstalling it.
func TestAFoundationFileThatDiffersFromItsRPMDigestRefuses(t *testing.T) {
	h := newErrataHost(t)
	h.builds.builds[2].Files["/lib64/libgcc_s-2.so.1"] = prerequisites.InstalledBuildFile{SHA256: strings.Repeat("a", 64)}
	h.refused(t, "the provided execution foundation differs at /usr/lib64/libgcc_s-2.so.1, from libgcc 15.3.1-2.fc43, which holds other content than the RPM database records for that build",
		"Install exactly libgcc 15.3.1-2.fc43 again with dnf (dnf install libgcc-15.3.1-2.fc43, or dnf reinstall libgcc-15.3.1-2.fc43 while that build is installed), hold it with dnf versionlock add libgcc-15.3.1-2.fc43, then repeat this command; a host that must take the update needs a Bootwright build whose execution foundation pins it.")
}

// Qualification reads exactly one x86_64 instance of each package: none, or
// more than one, refuses.
func TestAFoundationPackageNeedsOneX86_64Instance(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		h := newErrataHost(t)
		h.builds.builds = h.builds.builds[1:]
		h.refused(t, "the provided execution foundation differs at /usr/lib64/libc.so.6, from glibc 2.42-16.fc43, which holds other content than this build pins, and glibc has no x86_64 instance installed", glibcRemedy)
	})
	t.Run("two", func(t *testing.T) {
		h := newErrataHost(t)
		h.builds.builds = append(h.builds.builds, h.builds.builds[0])
		h.refused(t, "the provided execution foundation differs at /usr/lib64/libc.so.6, from glibc 2.42-16.fc43, which holds other content than this build pins, and more than one x86_64 instance of glibc is installed", glibcRemedy)
	})
}

// Another minor is still refused before any read of the host or its RPM
// database, exactly as it was before qualification existed.
func TestAnotherMinorStillRefuses(t *testing.T) {
	builds := &installedBuilds{}
	for _, platform := range []prerequisites.Platform{{OS: "rhel", Release: "9.9", Architecture: "amd64"}, {OS: "fedora", Release: "44", Architecture: "amd64"}} {
		_, err := ExecutionGuard{Builds: builds}.Inspect(context.Background(), platform)
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unsupported" || found[0].Message != "controller setup requires qualified RHEL 9.8 or Fedora 43 on Linux amd64" {
			t.Fatalf("%s %s inspected as %v", platform.OS, platform.Release, err)
		}
	}
	if builds.reads != 0 {
		t.Fatal("another minor read the RPM database")
	}
}

// A host holding the compiled builds qualifies nothing and never reads the
// RPM database.
func TestAnUnchangedFoundationQualifiesNothing(t *testing.T) {
	f := newExecutionFixture(t)
	builds := &installedBuilds{}
	f.guard.Builds = builds
	f.guard.view.packages = qualificationPackages
	inspection, err := f.guard.inspectRecord(context.Background(), nativeRecord{OS: "fedora", Release: "43", Execution: f.requirement, Packages: qualificationPackages})
	if err != nil || inspection.Qualified != nil || inspection.Refusal != nil || inspection.Drift != "" || builds.reads != 0 {
		t.Fatalf("an unchanged host inspected as %+v after %d reads (%v)", inspection, builds.reads, err)
	}
}

// A launch that meets an update setup could qualify names setup first, then
// the reinstall and hold that keeps the pinned build; over a requirement setup
// qualified, whose builds only the receipt names, it names the package as
// setup qualified it.
func TestALaunchRefusalNamesSetupForAQualifiableUpdate(t *testing.T) {
	f := newExecutionFixture(t)
	f.guard.view.packages = qualificationPackages
	f.guard.view.platform = prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	replaceFoundationFile(t, filepath.Join(f.root, "usr/lib64/libc.so.6"), []byte("a later glibc build"))
	err := f.guard.WithPython(context.Background(), &f.area, f.requirement, func(prerequisites.PythonLaunch, func() error) error {
		t.Fatal("a drifted foundation launched")
		return nil
	})
	found := diagnostics.Of(err)
	want := "If dnf updated glibc or libgcc within fedora 43, run bootwright setup to qualify the new builds; otherwise install exactly glibc 2.42-16.fc43 again with dnf (dnf install glibc-2.42-16.fc43, or dnf reinstall glibc-2.42-16.fc43 while that build is installed), hold it with dnf versionlock add glibc-2.42-16.fc43, then repeat this command; a host that must take the update needs a Bootwright build whose execution foundation pins it."
	if len(found) != 1 || found[0].Remediation != want || found[0].Message != "the provided execution foundation differs at /usr/lib64/libc.so.6, from glibc 2.42-16.fc43, which holds other content than this build pins" {
		t.Fatalf("the launch refused with %#v", found)
	}

	record, err := compiledCatalog()
	if err != nil {
		t.Fatal(err)
	}
	native, _ := selectNative(record, prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "amd64"})
	qualified := cloneExecution(native.Execution)
	qualified.PythonExecutable = "python/bin/python3.14"
	for index, file := range qualified.Files {
		if prerequisites.VersionedLibgcc(file.Path) {
			qualified.Files[index].Path = "/usr/lib64/libgcc_s-11-20250101.so.1"
		}
	}
	refusal := foundationRefusal(executionView{}, qualified, &foundationDrift{path: "/usr/lib64/libgcc_s-11-20250101.so.1", condition: driftMissing}, true)
	found = diagnostics.Of(refusal)
	if len(found) != 1 || found[0].Message != "the provided execution foundation differs at /usr/lib64/libgcc_s-11-20250101.so.1, from libgcc as setup qualified it, which is missing" ||
		found[0].Remediation != "If dnf updated glibc or libgcc within rhel 9.8, run bootwright setup to qualify the new builds; otherwise install the libgcc build setup qualified again with dnf (dnf reinstall libgcc), hold it with dnf versionlock add libgcc, then repeat this command; a host that must take another build needs a Bootwright build whose execution foundation pins it." {
		t.Fatalf("a qualified requirement's launch refused with %#v", found)
	}
}
