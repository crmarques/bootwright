package bundlelocal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
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

func TestPythonLatestAndExplicitReleaseSelection(t *testing.T) {
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
}

func TestPythonPublisherRejectsForeignArtifact(t *testing.T) {
	data := pythonMetadataFixture(t, "3.14.7")
	data = []byte(strings.ReplaceAll(string(data), "github.com/astral-sh", "example.test/astral-sh"))
	if _, _, err := selectPythonArtifact(data, "latest"); err == nil {
		t.Fatal("foreign Python artifact accepted")
	}
}

func TestAnsibleDoesNotDowngradeForIncompatiblePython(t *testing.T) {
	data := []byte(`{"info":{"version":"2.21.4","classifiers":["Programming Language :: Python :: 3.12","Programming Language :: Python :: 3.13","Programming Language :: Python :: 3.14"]}}`)
	if version, err := selectAnsibleRelease(data, "latest", "3.14.7"); err != nil || version != "2.21.4" {
		t.Fatalf("compatible latest rejected: %s %v", version, err)
	}
	for _, pair := range [][2]string{{"latest", "3.15.0"}, {"2.20.1", "3.14.7"}} {
		if _, err := selectAnsibleRelease(data, pair[0], pair[1]); err == nil {
			t.Fatalf("incompatible root accepted: %v", pair)
		}
	}
}

func TestAnsibleMinimumRefusesBeforeResolverEffects(t *testing.T) {
	resolver := NewBootstrapResolver()
	resolver.metadata = func(context.Context, string, string, prerequisites.SetupEgress) (toolMetadata, error) {
		t.Fatal("unsupported exact Ansible request reached publisher metadata")
		return toolMetadata{}, nil
	}
	versions := controller.DefaultDependencyVersions()
	versions.Ansible = "2.18.99"
	_, err := resolver.Resolve(context.Background(), prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, versions, prerequisites.SetupEgress{})
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, prerequisites.MinimumBootstrapAnsibleVersion) {
		t.Fatalf("unsupported override lacks minimum diagnostic: %+v", diagnostics)
	}
	for _, version := range []string{"2.18.99", "2.19.0"} {
		data := []byte(`{"info":{"version":"` + version + `","classifiers":["Programming Language :: Python :: 3.13"]}}`)
		for _, requested := range []string{"latest", version} {
			got, err := selectAnsibleRelease(data, requested, "3.13.15")
			if version == "2.19.0" {
				if err != nil || got != version {
					t.Fatalf("minimum compatible release rejected: %s %v", got, err)
				}
			} else {
				diagnostics := desiredstate.DiagnosticsOf(err)
				if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, prerequisites.MinimumBootstrapAnsibleVersion) {
					t.Fatalf("unsupported publisher release lacks minimum diagnostic: %+v", diagnostics)
				}
			}
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
