package bundlelocal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func pythonMetadataFixture(t *testing.T, versions ...string) []byte {
	t.Helper()
	records := map[string]any{}
	for _, version := range versions {
		var major, minor, patch int
		parts := strings.Split(version, ".")
		for index, target := range []*int{&major, &minor, &patch} {
			for _, c := range parts[index] {
				*target = *target*10 + int(c-'0')
			}
		}
		records[version] = map[string]any{"name": "cpython", "os": "linux", "libc": "gnu", "arch": map[string]any{"family": "x86_64", "variant": nil}, "major": major, "minor": minor, "patch": patch, "prerelease": "", "variant": nil, "build": "20260901", "sha256": strings.Repeat("a", 64), "url": "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-" + version + "%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"}
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPythonLatestIsTheNewestQualifiedMinor(t *testing.T) {
	data := pythonMetadataFixture(t, "3.12.14", "3.13.15", "3.14.7")
	for request, want := range map[string]string{"latest": "3.14.7", "3.13.15": "3.13.15"} {
		got, source, err := selectPythonArtifact(data, request)
		if err != nil || got != want || source.SHA256 != strings.Repeat("a", 64) {
			t.Fatalf("%s: %q %#v %v", request, got, source, err)
		}
	}
	if _, _, err := selectPythonArtifact(data, "3.13.14"); err == nil {
		t.Fatal("unpublished exact version accepted")
	}
	newer := pythonMetadataFixture(t, "3.14.7", "3.14.8")
	if got, _, err := selectPythonArtifact(newer, "latest"); err != nil || got != "3.14.8" {
		t.Fatalf("new publisher release not selected: %s %v", got, err)
	}
	// A newer minor than the qualified ansible-core supports as a controller
	// is neither selected nor a reason to refuse.
	unqualified := pythonMetadataFixture(t, "3.14.7", "3.15.0", "3.16.1")
	if got, _, err := selectPythonArtifact(unqualified, "latest"); err != nil || got != "3.14.7" {
		t.Fatalf("latest left the qualified controller minors: %s %v", got, err)
	}
	if got, _, err := selectPythonArtifact(unqualified, "3.15.0"); err == nil {
		t.Fatalf("an exact unqualified controller Python was selected: %s", got)
	}
}

// ansibleFile is one file of a PyPI release listing, shaped like the JSON
// API's per-file record; the selector reads only its name, type and yank.
func ansibleFile(version, packageType, requiresPython string, yanked bool) map[string]any {
	filename := "ansible_core-" + version + ".tar.gz"
	if packageType == "bdist_wheel" {
		filename = "ansible_core-" + version + "-py3-none-any.whl"
	}
	return map[string]any{"filename": filename, "packagetype": packageType, "python_version": "py3", "requires_python": requiresPython, "yanked": yanked, "yanked_reason": nil, "url": "https://files.pythonhosted.org/packages/" + filename, "digests": map[string]string{"sha256": strings.Repeat("a", 64)}}
}

// ansibleMetadataFixture shapes PyPI's project JSON when releases is non-nil
// and its version-specific JSON, which carries no releases key, otherwise.
func ansibleMetadataFixture(t *testing.T, version string, releases map[string][]map[string]any) []byte {
	t.Helper()
	record := map[string]any{"info": map[string]any{"name": "ansible-core", "version": version, "requires_python": ">=3.12"}, "urls": []map[string]any{ansibleFile(version, "bdist_wheel", ">=3.12", false)}}
	if releases != nil {
		record["releases"] = releases
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAnsibleLatestIsTheNewestQualifiedPatch(t *testing.T) {
	wheel := func(version string) []map[string]any {
		return []map[string]any{ansibleFile(version, "bdist_wheel", ">=3.12", false), ansibleFile(version, "sdist", ">=3.12", false)}
	}
	releases := map[string][]map[string]any{
		"2.20.9": wheel("2.20.9"), "2.21.4": wheel("2.21.4"), "2.21.9": wheel("2.21.9"), "2.21.10": wheel("2.21.10"),
		"2.21.11":    {ansibleFile("2.21.11", "bdist_wheel", ">=3.12", true), ansibleFile("2.21.11", "sdist", ">=3.12", true)},
		"2.21.12rc1": wheel("2.21.12rc1"),
		"2.21.13":    {ansibleFile("2.21.13", "sdist", ">=3.12", false)},
		"2.22.0b1":   wheel("2.22.0b1"), "2.22.0": wheel("2.22.0"),
	}
	if got, err := selectAnsibleRelease(ansibleMetadataFixture(t, "2.22.0", releases), "latest", "3.14.7"); err != nil || got != "2.21.10" {
		t.Fatalf("latest is not the newest qualified patch with a live wheel: %s %+v", got, diagnostics.Of(err))
	}
	for name, listing := range map[string]map[string][]map[string]any{
		"releases absent":        nil,
		"no qualified candidate": {"2.20.9": wheel("2.20.9"), "2.21.13": releases["2.21.13"], "2.22.0": wheel("2.22.0")},
	} {
		got, err := selectAnsibleRelease(ansibleMetadataFixture(t, "2.22.0", listing), "latest", "3.14.7")
		found := diagnostics.Of(err)
		if len(found) != 1 || !strings.Contains(found[0].Message, "ansible-core "+prerequisites.QualifiedAnsibleMinor+" ") {
			t.Fatalf("%s: latest did not fail closed on the qualified minor: %s %+v", name, got, found)
		}
	}
}

func TestAnsibleDoesNotDowngradeForIncompatiblePython(t *testing.T) {
	// pip's exact-root resolve enforces Requires-Python; the selector never
	// yields to an older release that the selected interpreter could run.
	project := ansibleMetadataFixture(t, "2.21.5", map[string][]map[string]any{
		"2.21.4": {ansibleFile("2.21.4", "bdist_wheel", ">=3.12", false)},
		"2.21.5": {ansibleFile("2.21.5", "bdist_wheel", ">=3.15", false)},
	})
	if version, err := selectAnsibleRelease(project, "latest", "3.14.7"); err != nil || version != "2.21.5" {
		t.Fatalf("latest yielded to an older release for the selected Python: %s %v", version, err)
	}
	if version, err := selectAnsibleRelease(project, "latest", "3.15.0"); err == nil {
		t.Fatalf("an unqualified controller Python was accepted: %s", version)
	}
	_, err := selectAnsibleRelease(ansibleMetadataFixture(t, "2.20.1", nil), "2.20.1", "3.14.7")
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, "ansible-core "+prerequisites.QualifiedAnsibleMinor+" ") {
		t.Fatalf("an exact release outside the qualified minor was accepted: %+v", found)
	}
	exact := ansibleMetadataFixture(t, "2.21.4", nil)
	if version, err := selectAnsibleRelease(exact, "2.21.4", "3.14.7"); err != nil || version != "2.21.4" {
		t.Fatalf("an exact qualified release was refused: %s %v", version, err)
	}
	if version, err := selectAnsibleRelease(exact, "2.21.3", "3.14.7"); err == nil {
		t.Fatalf("an exact release was taken from another release's metadata: %s", version)
	}
}

func TestPythonPublisherRejectsForeignArtifact(t *testing.T) {
	data := pythonMetadataFixture(t, "3.14.7")
	data = []byte(strings.ReplaceAll(string(data), "github.com/astral-sh", "example.test/astral-sh"))
	if _, _, err := selectPythonArtifact(data, "latest"); err == nil {
		t.Fatal("foreign Python artifact accepted")
	}
}

func TestExactReleasesOutsideTheQualifiedSetRefuseBeforeResolverEffects(t *testing.T) {
	for _, exact := range []struct{ python, ansible, message string }{
		{"latest", "2.18.99", "ansible-core " + prerequisites.QualifiedAnsibleMinor + " "},
		{"latest", "2.20.9", "ansible-core " + prerequisites.QualifiedAnsibleMinor + " "},
		{"latest", "2.22.0", "ansible-core " + prerequisites.QualifiedAnsibleMinor + " "},
		{"3.15.0", "latest", "CPython " + strings.Join(prerequisites.QualifiedControllerPythons(), ", ") + " "},
	} {
		resolver := NewBootstrapResolver()
		resolver.metadata = func(context.Context, string, string, prerequisites.SetupEgress) (toolMetadata, error) {
			t.Fatalf("exact Python %s, Ansible %s reached publisher metadata", exact.python, exact.ansible)
			return toolMetadata{}, nil
		}
		versions := controller.DefaultDependencyVersions()
		versions.Python, versions.Ansible = exact.python, exact.ansible
		_, err := resolver.Resolve(context.Background(), prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, versions, prerequisites.SetupEgress{})
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "controller.unsupported" || !strings.Contains(found[0].Message, exact.message) {
			t.Fatalf("exact Python %s, Ansible %s lacks the qualified-set diagnostic: %+v", exact.python, exact.ansible, found)
		}
	}
}

func TestPipReportRejectsUnapprovedResolutionEffects(t *testing.T) {
	item := func(name, version string) map[string]any {
		return map[string]any{"metadata": map[string]string{"name": name, "version": version}, "download_info": map[string]any{"url": "https://files.pythonhosted.org/packages/" + name + ".whl", "archive_info": map[string]any{"hashes": map[string]string{"sha256": strings.Repeat("a", 64)}}}}
	}
	for _, mutation := range []string{"valid", "direct", "yanked", "foreign", "sdist", "missing-hash", "wrong-root", "unknown-report", "duplicate", "foreign-python"} {
		t.Run(mutation, func(t *testing.T) {
			items := []map[string]any{item("ansible-core", "2.21.4"), item("urllib3", "2.7.0")}
			report := map[string]any{"version": "1", "install": items, "environment": map[string]string{"python_full_version": "3.14.7", "implementation_name": "cpython", "platform_machine": "x86_64", "sys_platform": "linux"}}
			switch mutation {
			case "direct":
				items[0]["is_direct"] = true
			case "yanked":
				items[0]["is_yanked"] = true
			case "foreign":
				items[0]["download_info"].(map[string]any)["url"] = "https://example.test/source.whl"
			case "sdist":
				items[0]["download_info"].(map[string]any)["url"] = "https://files.pythonhosted.org/packages/source.tar.gz"
			case "missing-hash":
				delete(items[0]["download_info"].(map[string]any)["archive_info"].(map[string]any)["hashes"].(map[string]string), "sha256")
			case "wrong-root":
				items[0]["metadata"].(map[string]string)["version"] = "2.20.1"
			case "unknown-report":
				report["version"] = "2"
			case "foreign-python":
				report["environment"].(map[string]string)["python_full_version"] = "3.13.15"
			case "duplicate":
				report["install"] = append(items, item("Ansible_Core", "2.21.4"))
			}
			data, _ := json.Marshal(report)
			wheels, sources, err := parsePipReport(data, prerequisites.BootstrapDefinition{AnsibleVersion: "2.21.4", PythonVersion: "3.14.7"})
			if mutation == "valid" {
				if err != nil || len(wheels) != 2 || len(sources) != 2 {
					t.Fatalf("valid report rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("unapproved resolver result accepted")
			}
		})
	}
}

func TestProjectionIdentityIncludesFileModeAndSelectedPythonLayout(t *testing.T) {
	p := newProjection()
	p.site = "python/lib/python3.14/site-packages/"
	if err := p.add("python/bin/python3.14", []byte("executable"), true); err != nil {
		t.Fatal(err)
	}
	before := p.identity()
	p.files["python/bin/python3.14"] = projectedFile{data: []byte("executable")}
	if p.identity() == before {
		t.Fatal("projection digest omitted executable mode")
	}
}
